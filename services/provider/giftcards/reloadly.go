package giftcards

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/services/provider/giftcards/reloadly_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type ReloadlyProvider struct {
	provider.BaseProvider
	config *GiftCardConfig
	token  reloadlymodels.TokenApiResponse
}

type GiftCardConfig struct {
	GiftCardName    string `mapstructure:"GIFTCARD_PROVIDER_NAME"`
	GiftCardID      string `mapstructure:"GIFTCARD_APP_ID"`
	GiftCardKey     string `mapstructure:"GIFTCARD_KEY"`
	GiftCardBaseUrl string `mapstructure:"GIFTCARD_BASE_URL"`
	GiftCardAuthUrl string `mapstructure:"GIFTCARD_AUTH_URL"`
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

func BuildProductsURL(baseURL string) (*url.URL, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}

	// Path params
	base.Path += "/products"

	// Build query parameters in specific order
	// queryParams := []string{
	// 	fmt.Sprintf("size=%d", params.Size),
	// 	fmt.Sprintf("page=%d", params.Page),
	// 	fmt.Sprintf("includeRange=%t", params.IncludeRange),
	// 	fmt.Sprintf("includeFixed=%t", params.IncludeFixed),
	// }

	// Join parameters with & to create the final query string
	// base.RawQuery = strings.Join(queryParams, "&")

	return base, nil
}

func (r *ReloadlyProvider) GetAllGiftCards() (reloadlymodels.GiftCardCollection, error) {

	token, err := r.GetToken()
	if err != nil {
		return nil, err
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token

	url, err := BuildProductsURL(r.BaseURL)
	if err != nil {
		return nil, err
	}

	resp, err := r.MakeRequest("GET", url.String(), nil, requiredHeaders)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		logging.NewLogger().Error("resp", string(respBody))
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var products reloadlymodels.PageResponse[reloadlymodels.GiftCardCollectionElement]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&products)
	if err != nil {
		return nil, fmt.Errorf("error parsing products: %w", err)
	}

	return products.Content, nil
}

func (r *ReloadlyProvider) GetToken() (string, error) {

	if r.token.AccessToken != "" {
		tokenExpiry := time.Now().Add(time.Duration(r.token.ExpiresIn) * time.Second)
		if time.Now().After(tokenExpiry) {
			return r.token.AccessToken, nil
		}
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/json"
	requiredHeaders["Content-Type"] = "application/json"

	url := r.config.GiftCardAuthUrl
	request := reloadlymodels.AuthConfig{
		ClientID:     r.config.GiftCardID,
		ClientSecret: r.config.GiftCardKey,
		GrantType:    "client_credentials",
		Audience:     r.config.GiftCardBaseUrl,
	}

	resp, err := r.MakeRequest("POST", url, request, requiredHeaders)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		logging.NewLogger().Error("resp", string(respBody))
		return "", fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var apiResponse reloadlymodels.TokenApiResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&apiResponse)
	if err != nil {
		return "", fmt.Errorf("error parsing products: %w", err)
	}

	/// Set AccessToken
	r.token = apiResponse
	return apiResponse.AccessToken, nil
}
