package cryptocurrency

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type BitgoProvider struct {
	provider.BaseProvider
	config *CryptoConfig
}

type CryptoConfig struct {
	CryptoProviderName string `mapstructure:"CRYPTO_PROVIDER_NAME"`
	BitgoPort          string `mapstructure:"BITGO_PORT"`
	BitgoHost          string `mapstructure:"BITGO_HOST"`
	BitgoBaseUrl       string `mapstructure:"BITGO_BASE_URL"`
	BitgoAccessKey     string `mapstructure:"BITGO_ACCESS_KEY"`
	BitgoEnterpriseID  string `mapstructure:"BITGO_ENTERPRISE"`
	BitgoWalletPasskey string `mapstructure:"BITGO_WALLET_PASSKEY"`
}

func NewCryptoProvider() *BitgoProvider {

	var c CryptoConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &BitgoProvider{
		BaseProvider: provider.BaseProvider{
			Name:    c.CryptoProviderName,
			BaseURL: c.BitgoBaseUrl,
			APIKey:  c.BitgoAccessKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

func (p *BitgoProvider) CreateWallet(coin SupportedCoin) (interface{}, error) {

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += fmt.Sprintf("/api/v2/%s/wallet/generate", coin)

	request := WalletCreationModel{
		Label:                           fmt.Sprintf("SwiftFiat %s Wallet", coin),
		Passphrase:                      p.config.BitgoWalletPasskey,
		Enterprise:                      p.config.BitgoEnterpriseID,
		DisableTransactionNotifications: false,
		DisableKRSEmail:                 true,
	}

	resp, err := p.MakeRequest("POST", base.String(), request, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		// Example error handling with body logging
		if resp.StatusCode != http.StatusOK {
			// Read the response body
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				logging.NewLogger().Error("failed to read response body", err)
				return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
			}

			// Log the body
			logging.NewLogger().Error("response body", string(bodyBytes))

			// Reset the response body for further processing (if needed)
			resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
		}
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel interface{}
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel, nil
}

func (p *BitgoProvider) FetchWallets() (interface{}, error) {

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "/api/v2/wallets"

	resp, err := p.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel interface{}
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel, nil
}

func (p *BitgoProvider) CreateWalletAddress(walletId string, coin SupportedCoin) (*WalletAddress, error) {

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += fmt.Sprintf("/api/v2/%s/wallet/%s/address", coin, walletId)

	resp, err := p.MakeRequest("POST", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel WalletAddress
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel, nil
}
