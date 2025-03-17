package cryptocurrency

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

	return services.Result, nil
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
	defer resp.Body.Close()

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

func (p *CryptomusProvider) signRequest(apiKey string, reqBody []byte) string {
	data := base64.StdEncoding.EncodeToString(reqBody)
	hash := md5.Sum([]byte(data + apiKey))
	return hex.EncodeToString(hash[:])
}
