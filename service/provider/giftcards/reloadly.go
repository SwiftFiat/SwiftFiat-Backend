package giftcards

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/service/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/service/provider/giftcards/reloadly_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type ReloadlyProvider struct {
	provider.BaseProvider
	config *GiftCardConfig
}

type GiftCardConfig struct {
	GiftCardName    string `mapstructure:"GIFTCARD_PROVIDER_NAME"`
	GiftCardID      string `mapstructure:"GIFTCARD_APP_ID"`
	GiftCardKey     string `mapstructure:"GIFTCARD_KEY"`
	GiftCardBaseUrl string `mapstructure:"GIFTCARD_BASE_URL"`
}

func NewGiftCardProvider() *ReloadlyProvider {

	var c GiftCardConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &ReloadlyProvider{
		BaseProvider: provider.BaseProvider{
			Name:    c.GiftCardName,
			BaseURL: c.GiftCardBaseUrl,
			APIKey:  c.GiftCardKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

// BuildProductsURL constructs the URL for the products endpoint with the given parameters
func BuildProductsURL(baseURL string, params reloadlymodels.ProductQueryParams) string {
	base := baseURL
	base += "/api/v1/products"

	// Create query parameters
	queryParams := url.Values{}

	// Add pagination parameters
	queryParams.Add("size", strconv.Itoa(params.Size))
	queryParams.Add("page", strconv.Itoa(params.Page))

	// Add filters if they are not empty
	if params.ProductName != "" {
		queryParams.Add("productName", params.ProductName)
	}

	if params.CountryCode != "" {
		queryParams.Add("countryCode", params.CountryCode)
	}

	if params.ProductCategoryID > 0 {
		queryParams.Add("productCategoryId", strconv.Itoa(params.ProductCategoryID))
	}

	// Add boolean flags
	queryParams.Add("includeRange", strconv.FormatBool(params.IncludeRange))
	queryParams.Add("includeFixed", strconv.FormatBool(params.IncludeFixed))

	// Encode and return the full URL
	return base + "?" + queryParams.Encode()
}

func (r *ReloadlyProvider) GetAllGiftCards() ([]reloadlymodels.Product, error) {
	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = r.config.GiftCardID
	requiredHeaders["Authorization"] = r.config.GiftCardKey

	params := reloadlymodels.ProductQueryParams{
		Size:              10,
		Page:              1,
		ProductName:       "Amazon",
		CountryCode:       "US",
		ProductCategoryID: 2,
		IncludeRange:      true,
		IncludeFixed:      true,
	}

	url := BuildProductsURL(r.BaseURL, params)

	resp, err := r.MakeRequest("GET", url, nil, requiredHeaders)
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
	var products []reloadlymodels.Product
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&products)
	if err != nil {
		return nil, fmt.Errorf("error parsing products: %w", err)
	}
	return products, nil
}
