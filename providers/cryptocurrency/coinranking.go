package cryptocurrency

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type CoinRankingProvider struct {
	providers.BaseProvider
	config *CoinRankingProviderConfig
}

type CoinRankingProviderConfig struct {
	RatesProviderName    string `mapstructure:"RATES_PROVIDER_NAME"`
	CoinRankingBaseUrl   string `mapstructure:"COINRANKING_BASE_URL"`
	CoinRankingAccessKey string `mapstructure:"COINRANKING_ACCESS_KEY"`
}

func NewCoinRankingProvider() *CoinRankingProvider {
	var c CoinRankingProviderConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &CoinRankingProvider{
		BaseProvider: providers.BaseProvider{
			Name:    c.RatesProviderName,
			BaseURL: c.CoinRankingBaseUrl,
			APIKey:  c.CoinRankingAccessKey,
			Client:  &http.Client{Timeout: 10 * time.Second},
		},
		config: &c,
	}
}

// GetCoinUUIDBySymbol fetches the UUID for a given coin symbol from the CoinRanking API
func (p *CoinRankingProvider) GetCoinUUIDBySymbol(symbol string) (string, error) {
	url := fmt.Sprintf("%s/coin?symbols=%s", p.BaseURL, strings.ToUpper(symbol))

	headers := map[string]string{
		"x-access-token": p.APIKey,
	}

	response, err := p.MakeRequest("GET", url, nil, headers)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get coin UUID: %s", response.Status)
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Coins []struct {
				UUID   string `json:"uuid"`
				Symbol string `json:"symbol"`
			} `json:"coins"`
		} `json:"data"`
	}

	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	if result.Status != "success" {
		return "", fmt.Errorf("API request is unsuccessful: %s", response.Status)
	}

	for _, coin := range result.Data.Coins {
		if strings.EqualFold(coin.Symbol, symbol) {
			return coin.UUID, nil
		}
	}

	return "", fmt.Errorf("coin with symbol %s not found", symbol)
}

// GetCoinDetailsBySymbol fetches coin details by symbol, resolving the UUID internally
func (p *CoinRankingProvider) GetCoinDetailsBySymbol(symbol string) (*CoinRankingResponse, error) {
	// First, check if the symbol exists in supportedCoins
	uuid, exists := supportedCoins[strings.ToLower(symbol)]
	if !exists {
		// If not in supportedCoins, fetch UUID from API
		var err error
		uuid, err = p.GetCoinUUIDBySymbol(symbol)
		if err != nil {
			return nil, err
		}
	}

	// Use the UUID to fetch coin details
	url := fmt.Sprintf("%s/coin/%s", p.BaseURL, uuid)

	headers := map[string]string{
		"x-access-token": p.APIKey,
	}

	response, err := p.MakeRequest("GET", url, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get coin details: %s", response.Status)
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Coin CoinRankingResponse `json:"coin"`
		} `json:"data"`
	}

	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if result.Status != "success" {
		return nil, fmt.Errorf("API request unsuccessful: %s", result.Status)
	}

	return &result.Data.Coin, nil
}
