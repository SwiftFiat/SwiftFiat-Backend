package cryptocurrency

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

// CoinGecko Supported Coin Names
var supportedCoins = map[string]string{
	"btc":       "bitcoin",
	"tbtc":      "bitcoin",
	"tbtc4":     "bitcoin",
	"sol":       "solana",
	"tsol":      "solana",
	"xrp":       "ripple",
	"txrp":      "ripple",
	"usdt":      "tether",
	"sol:usdt":  "tether",
	"tusdt":     "tether",
	"usdc":      "usd-coin",
	"eth":       "ethereum",
	"teth":      "ethereum",
	"sol:usdc":  "usd-coin",
	"tusdc":     "usd-coin",
	"usdc:usdt": "tether",
	"eth:usdt":  "tether",
	"eth:usdc":  "usd-coin",
}

type CoinGeckoProvider struct {
	providers.BaseProvider
	config *RatesProviderConfig
}

type RatesProviderConfig struct {
	RatesProviderName  string `mapstructure:"RATES_PROVIDER_NAME"`
	CoinGeckoBaseUrl   string `mapstructure:"COINGECKO_BASE_URL"`
	CoinGeckoAccessKey string `mapstructure:"COINGECKO_ACCESS_KEY"`
}

func NewRatesProvider() *CoinGeckoProvider {

	var c RatesProviderConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &CoinGeckoProvider{
		BaseProvider: providers.BaseProvider{
			Name:    c.RatesProviderName,
			BaseURL: c.CoinGeckoBaseUrl,
			APIKey:  c.CoinGeckoAccessKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

func (c *CoinGeckoProvider) GetUSDRate(coin *string) (string, error) {

	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("unexpected status code: %v", err.Error())
	} 

	// Path params
	base.Path += "/price"
	// Query params
	params := url.Values{}
	if coin != nil {
		params.Add("ids", supportedCoins[*coin])
		params.Add("vs_currencies", "usd")
	}
	base.RawQuery = params.Encode()

	resp, err := c.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return "", fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	coinID := supportedCoins[*coin]
	var newModel map[string]any
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return "", fmt.Errorf("error decoding response body: %w", err)
	}

	logging.NewLogger().Info("newModel From CoinGecko", newModel)

	// Type assertion to convert interface{} to *string
	coinRate, ok := newModel[coinID]
	if !ok || coinRate == nil {
		return "", fmt.Errorf("coin rate not found in response")
	}
	coinMap, ok := coinRate.(map[string]any)
	if !ok {
		return "", fmt.Errorf("coin rate is not a map")
	}
	usdValue, ok := coinMap["usd"].(float64)
	if !ok {
		return "", fmt.Errorf("USD value for %s not found or not a float", coinID)
	}

	s := fmt.Sprintf("%f", usdValue)
	return s, nil
}

func (c *CoinGeckoProvider) GetCoinData(coin string) (map[string]any, error) {
	coinID, ok := supportedCoins[coin]
	if !ok {
		return nil, fmt.Errorf("unsupported coin: %s", coin)
	}

	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	// CoinGecko coin info endpoint
	base.Path += fmt.Sprintf("/coins/%s", coinID)

	resp, err := c.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("resp", resp)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	logging.NewLogger().Info("Coin Data", data)
	return data, nil
}
