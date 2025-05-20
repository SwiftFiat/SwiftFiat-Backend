package cryptocurrency

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type CryptomusProvider struct {
	providers.BaseProvider
	config *CryptomusConfig
}

type CryptomusConfig struct {
	CryptomusProviderName string `mapstructure:"CRYPTOMUS_PROVIDER_NAME"`
	BaseURL               string `mapstructure:"CRYPTOMUS_BASE_URL"`
	APIKey                string `mapstructure:"CRYPTOMUS_API_KEY"`
	MerchantID            string `mapstructure:"CRYPTOMUS_MERCHANT_ID"`
}

func NewCryptomusProvider() *CryptomusProvider {

	var c CryptomusConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &CryptomusProvider{
		BaseProvider: providers.BaseProvider{
			Name:    c.CryptomusProviderName,
			BaseURL: c.BaseURL,
			APIKey:  c.APIKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

func (p *CryptomusProvider) CreateStaticWallet(request *StaticWalletRequest) (*StaticWalletResponse, error) {
	wallet, err := p.processRequest("POST", "/wallet", request)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	var staticWalletResponse StaticWalletRawResponse
	decoder := json.NewDecoder(wallet.Body)
	err = decoder.Decode(&staticWalletResponse)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return staticWalletResponse.Result, nil
}

func (p *CryptomusProvider) ListServices() ([]CryptomusService, error) {
    serviceResponse, err := p.processRequest("POST", "/payment/services", nil)
    if err != nil {
        return nil, fmt.Errorf("unexpected status code: %v", err.Error())
    }

    // Decode the response body
    var services ServicesRawResponse
    decoder := json.NewDecoder(serviceResponse.Body)
    err = decoder.Decode(&services)
    if err != nil {
        return nil, fmt.Errorf("error decoding response body: %w", err)
    }

    // Filter the services based on the specified criteria
    var filteredServices []CryptomusService
    for _, service := range services.Result {
        if (service.Currency == "USDT" && service.Network == "TON") ||
            (service.Currency == "ETH" && service.Network == "BSC") ||
            (service.Currency == "ETH" && service.Network == "TON") ||
            service.Currency == "BNB" ||
            service.Currency == "BTC" ||
            service.Currency == "LTC" ||
            service.Currency == "TRX" {
            filteredServices = append(filteredServices, service)
        }
    }

    // Add any random 3 services if available
    if len(filteredServices) < len(services.Result) {
        randomCount := 0
        for _, service := range services.Result {
            // Ensure the service is not already in the filtered list
            isAlreadyIncluded := false
            for _, filtered := range filteredServices {
                if filtered.Currency == service.Currency && filtered.Network == service.Network {
                    isAlreadyIncluded = true
                    break
                }
            }
            if !isAlreadyIncluded {
                filteredServices = append(filteredServices, service)
                randomCount++
            }
            if randomCount >= 3 {
                break
            }
        }
    }

    // Ensure the final list contains at least the specified currencies and random 3
    return filteredServices, nil
}

func (p *CryptomusProvider) processRequest(method string, endpoint string, payload any) (*http.Response, error) {
	if payload == nil {
		payload = map[string]string{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	sign := p.signRequest(p.config.APIKey, body)
	extraHeaders := map[string]string{
		"Content-Type": "application/json",
		"merchant":     p.config.MerchantID,
		"sign":         sign,
	}

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += endpoint

	resp, err := p.MakeRequest(method, base.String(), payload, extraHeaders)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logging.NewLogger().Error("problem closing body")
		}
	}(resp.Body)

	// Log the response
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read response body", err)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Log request details
	logging.NewLogger().Error("request details",
		"method", resp.Request.Method,
		"url", resp.Request.URL.String(),
		"headers", resp.Request.Header)

	// Log response details
	logging.NewLogger().Error("response details",
		"status_code", resp.StatusCode,
		"headers", resp.Header,
		"body", string(bodyBytes))

	// Reset the response body for further processing
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	return resp, nil
}

func (p *CryptomusProvider) GenerateQRCode(walletAddressUuid uuid.UUID) (*GenerateQRCodeResponse, error) {
    // Create the request payload
    payload := map[string]string{
        "wallet_address_uuid": walletAddressUuid.String(),
    }

    // Send the request
    qrCode, err := p.processRequest("POST", "/wallet/qr", payload)
    if err != nil {
        logging.NewLogger().Error(fmt.Sprintf("error creating qr code: %v", err.Error()))
        return nil, err
    }

    // Decode the response
    var GenerateWalletResponse GenerateQRCodeRawResponse
    decoder := json.NewDecoder(qrCode.Body)
    err = decoder.Decode(&GenerateWalletResponse)
    if err != nil {
        logging.NewLogger().Error(fmt.Sprintf("error decoding response body: %v", err))
        return nil, err
    }

    return GenerateWalletResponse.Result, nil
}
 
// VerifyWebhook verifies the webhook signature
func (p *CryptomusProvider) VerifyWebhook(payload *WebhookPayload, body []byte) error {
	if payload.Sign == "" {
		return errors.New("empty signature received")
	}

	// Parse JSON into a map to remove the 'sign' field
    var payloadData map[string]interface{}
    if err := json.Unmarshal(body, &payloadData); err != nil {
        return fmt.Errorf("failed to parse payload: %w", err)
    }

	receivedSign := payload.Sign
    delete(payloadData, "sign")

	// Re-marshal the remaining data
    cleanPayload, err := json.Marshal(payloadData)
    if err != nil {
        return fmt.Errorf("failed to re-marshal payload: %w", err)
    }

	// Compute the correct signature
    computedSign := p.signRequest(p.config.APIKey, cleanPayload) 

    if receivedSign != computedSign {
        logging.NewLogger().Error("signature mismatch",
            "received", receivedSign,
            "computed", computedSign,
            "payload", string(cleanPayload),
        )
        return errors.New("signature mismatch")
    }

    return nil
}

// ProcessWebhook handles the webhook payload
func (p *CryptomusProvider) ProcessWebhook(payload *WebhookPayload) (string, error) {
	if payload.Type != "payment" {
		logging.NewLogger().Error("received unexpected webhook type", "type", payload.Type)
		return "ignored: not a payment webhook", nil
	}

	switch payload.Status {
	case "paid": 
		if !payload.IsFinal {
			logging.NewLogger().Error("payment not final", "order_id", payload.OrderID)
			return "payment not final, awaiting confirmation", nil
		}
		// TODO: Update database, notify user, etc.
		
		// get users usd wwallet and update it

		// add the transaction to the database
		logging.NewLogger().Info("payment confirmed",
			"order_id", payload.OrderID,
			"amount", payload.Amount,
			"currency", payload.Currency,
			"txid", payload.TxID)
		return "payment processed successfully", nil
	case "pending":
		logging.NewLogger().Info("payment pending", "order_id", payload.OrderID)
		return "payment pending", nil
	case "failed":
		logging.NewLogger().Info("payment pending", "order_id", payload.OrderID)
		return "payment failed", nil
	default:
		logging.NewLogger().Info("unknown payment status", "order_id", payload.OrderID, "status", payload.Status)
		return "", errors.New("unknown payment status")
	}
}

// Add to CryptomusProvider
func (p *CryptomusProvider) TestCryptomusWebhook(uuid, orderId, currency, network, status string) error {
    payload := map[string]any{
        "uuid":         uuid,
        "order_id":     orderId,
        "currency":     currency,
        "network":      network,
        "status":       status,
        "url_callback": "https://swiftfiat-backend.onrender.com/api/v1/crypto/webhook",
    }

    payloadBytes, _ := json.Marshal(payload)
    sign := p.signRequest(p.config.APIKey, payloadBytes)

    req, err := http.NewRequest(
        "POST",
        "https://api.cryptomus.com/v1/test-webhook/payment",
        bytes.NewBuffer(payloadBytes),
    )
    if err != nil {
        return err
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("merchant", p.config.MerchantID)
    req.Header.Set("sign", sign)

    resp, err := p.Client.Do(req)
    if err != nil {
        return fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("invalid status %d: %s", resp.StatusCode, string(body))
    }

    return nil
}


func (p *CryptomusProvider) signRequest(apiKey string, reqBody []byte) string {
	data := base64.StdEncoding.EncodeToString(reqBody)
	hash := md5.Sum([]byte(data + apiKey))
	return hex.EncodeToString(hash[:])
}
