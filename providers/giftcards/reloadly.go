package giftcards

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards/reloadly_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type ReloadlyProvider struct {
	providers.BaseProvider
	config *GiftCardConfig
	token  reloadlymodels.TokenApiStore
}

type GiftCardConfig struct {
	GiftCardName    string `mapstructure:"GIFTCARD_PROVIDER_NAME"`
	GiftCardID      string `mapstructure:"GIFTCARD_APP_ID"`
	GiftCardKey     string `mapstructure:"GIFTCARD_KEY"`
	GiftCardBaseUrl string `mapstructure:"GIFTCARD_BASE_URL"`
	GiftCardAuthUrl string `mapstructure:"GIFTCARD_AUTH_URL"`
	GiftCardProdKey string `mapstructure:"GIFTCARD_PROD_KEY"`
	GiftCardProdID  string `mapstructure:"GIFTCARD_PROD_ID"`
	GiftCardProdUrl string `mapstructure:"GIFTCARD_PROD_URL"`
}

func NewGiftCardProvider() *ReloadlyProvider {

	var c GiftCardConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &ReloadlyProvider{
		BaseProvider: providers.BaseProvider{
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

	token, err := r.GetToken(reloadlymodels.PROD)
	if err != nil {
		return nil, err
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token

	url, err := BuildProductsURL(r.config.GiftCardProdUrl)
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

// audience: The target audience for the token, specifying the environment (PROD or SANDBOX)
func (r *ReloadlyProvider) GetToken(audience reloadlymodels.Audience) (string, error) {
	if r.token.Token.AccessToken != "" && r.token.Audience == audience {
		tokenExpiry := time.Now().Add(time.Duration(r.token.Token.ExpiresIn) * time.Second)
		if time.Now().Before(tokenExpiry) {
			return r.token.Token.AccessToken, nil
		}
	}

	var clientID string
	var clientSecret string
	var audienceType string

	if audience == reloadlymodels.PROD {
		clientID = r.config.GiftCardProdID
		clientSecret = r.config.GiftCardProdKey
		audienceType = r.config.GiftCardProdUrl
	} else {
		clientID = r.config.GiftCardID
		clientSecret = r.config.GiftCardKey
		audienceType = r.config.GiftCardBaseUrl
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/json"
	requiredHeaders["Content-Type"] = "application/json"

	url := r.config.GiftCardAuthUrl
	request := reloadlymodels.AuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		GrantType:    "client_credentials",
		Audience:     audienceType,
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
	r.token = reloadlymodels.TokenApiStore{
		Token:    apiResponse,
		Audience: audience,
	}
	return apiResponse.AccessToken, nil
}

func (r *ReloadlyProvider) BuyGiftCard(request *reloadlymodels.GiftCardPurchaseRequest) (*reloadlymodels.GiftCardPurchaseResponse, error) {
	token, err := r.GetToken(reloadlymodels.SANDBOX)
	if err != nil {
		return nil, err
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token

	base, err := url.Parse(r.config.GiftCardBaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	base.Path += "/orders"

	resp, err := r.MakeRequest("POST", base.String(), *request, requiredHeaders)
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

	logging.NewLogger().Info(fmt.Sprintf("response status - %v", resp.Status))
	logging.NewLogger().Info(fmt.Sprintf("response statusCode - %v", resp.StatusCode))

	// Decode the response body
	var response reloadlymodels.GiftCardPurchaseResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("error parsing products: %w", err)
	}

	return &response, nil
}

func (r *ReloadlyProvider) GetCardInfo(request string) (interface{}, error) {
	token, err := r.GetToken(reloadlymodels.SANDBOX)
	if err != nil {
		return nil, err
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token

	base, err := url.Parse(r.config.GiftCardBaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	base.Path += fmt.Sprintf("/orders/transactions/%s/cards", request)

	resp, err := r.MakeRequest("GET", base.String(), nil, requiredHeaders)
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

	logging.NewLogger().Info(fmt.Sprintf("response status - %v", resp.Status))
	logging.NewLogger().Info(fmt.Sprintf("response statusCode - %v", resp.StatusCode))

	// Decode the response body
	var response interface{}
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("error parsing products: %w", err)
	}

	return &response, nil
}
