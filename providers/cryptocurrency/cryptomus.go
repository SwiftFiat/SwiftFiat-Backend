package cryptocurrency

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

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
		if service.Currency == "USDT" ||
			service.Currency == "ETH" ||
			service.Currency == "BNB" ||
			service.Currency == "BTC" ||
			service.Currency == "VERSE" ||
			service.Currency == "DAI" ||
			service.Currency == "LTC" ||
			service.Currency == "DOGE" ||
			service.Currency == "TRX" {
			filteredServices = append(filteredServices, service)
		}
	}
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
func (p *CryptomusProvider) VerifySign(apiKey string, reqBody []byte) error {
	logging.NewLogger().Info("starting VerifyWebhook....")

	var jsonBody map[string]any
	err := json.Unmarshal(reqBody, &jsonBody)
	if err != nil {
		return err
	}

	reqSign, ok := jsonBody["sign"].(string)
	if !ok {
		return errors.New("missing signature field in request body")
	}
	delete(jsonBody, "sign")

	expectedSign := p.signRequest(apiKey, reqBody)
	if reqSign != expectedSign {
		return errors.New("invalid signature")
	}
	return nil
}

// Add to CryptomusProvider
func (p *CryptomusProvider) TestCryptomusWebhook(request *TestWebhookRequest) (*TestWebhookResponse, error) {
	res, err := p.processRequest("POST", "/test-webhook/wallet", request)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	response := &TestWebhookResponse{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return nil, err
	}

	return response, nil
}

func (p *CryptomusProvider) signRequest(apiKey string, reqBody []byte) string {
	data := base64.StdEncoding.EncodeToString(reqBody)
	hash := md5.Sum([]byte(data + apiKey))
	return hex.EncodeToString(hash[:])
}

func (p *CryptomusProvider) ParseWebhook(reqBody []byte, verifySign bool) (*WebhookPayload, error) {
	var apiKey string
	response := &WebhookPayload{}

	err := json.Unmarshal(reqBody, response)
	if err != nil {
		return nil, err
	}

	logging.NewLogger().Info("Webhook type", response.Type)
	logging.NewLogger().Info("Webhook type", response)
	switch response.Type {
	case "wallet":
		apiKey = p.config.APIKey
	default:
		return nil, errors.New("unknown webhook type")
	}

	if verifySign {
		err = p.VerifySign(apiKey, reqBody)
		if err != nil {
			return nil, err
		}
	}

	return response, err
}

func (p *CryptomusProvider) ResendWebhook(req *ResendWebhookRequest) (*ResendWebhookResponse, error) {
	res, err := p.processRequest("POST", "/payment/resend", req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	response := &ResendWebhookResponse{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return nil, err
	}

	return response, nil
}

func (p *CryptomusProvider) GetPaymentInfo(req *PaymentInfoRequest) (*PaymentInfoResponse, error) {
	res, err := p.processRequest("POST", "/payment/info", req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	response := &PaymentInfoResponse{}
	if err = json.NewDecoder(res.Body).Decode(response); err != nil {
		return nil, err
	}

	return response, nil
}
