package giftcards

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
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

	baseURL, err := BuildProductsURL(r.config.GiftCardProdUrl)
	if err != nil {
		return nil, err
	}

	logger := logging.NewLogger()
	var allContent reloadlymodels.GiftCardCollection
	pageNumber := 0
	lastPage := false

	for !lastPage {
		// Create URL with page parameter
		queryParams := baseURL.Query()
		queryParams.Set("page", strconv.Itoa(pageNumber))
		baseURL.RawQuery = queryParams.Encode()

		resp, err := r.MakeRequest("GET", baseURL.String(), nil, requiredHeaders)
		if err != nil {
			return nil, err
		}

		// Read the response body
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close() // Close the body after reading

		if err != nil {
			return nil, fmt.Errorf("error reading response body: %w", err)
		}

		// Create truncated body for logging
		bodyStr := string(respBody)
		maxLen := 2000000000000000000 // Maximum length for logging
		truncatedBody := bodyStr
		if len(bodyStr) > maxLen {
			truncatedBody = bodyStr[:maxLen] + "... [truncated]"
		}

		logData := map[string]interface{}{
			"status_code":   resp.StatusCode,
			"url":           resp.Request.URL.String(),
			"headers":       resp.Header,
			"method":        resp.Request.Method,
			"response_body": truncatedBody,
			"page_number":   pageNumber,
		}

		// Check if status code indicates an error
		if resp.StatusCode != http.StatusOK {
			logger.Error("Reloadly API Error", logData)
			return nil, fmt.Errorf("unexpected status code: %d, URL: %s, Response: %s",
				resp.StatusCode, resp.Request.URL, truncatedBody)
		}

		// Log success response
		logger.Info("Reloadly API Response", logData)

		// Decode the response body
		var products reloadlymodels.PageResponse[reloadlymodels.GiftCardCollectionElement]
		if err := json.Unmarshal(respBody, &products); err != nil {
			return nil, fmt.Errorf("error parsing products: %w", err)
		}

		// Append content from this page to our collection
		allContent = append(allContent, products.Content...)

		// Check if this is the last page
		lastPage = products.Last
		pageNumber++
	}

	return allContent, nil
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

	logging.NewLogger().Info("Reloadly env vars", map[string]interface{}{
		"prod client ID":        r.config.GiftCardProdID,
		"prod client secret":    r.config.GiftCardProdKey,
		"prod url":              r.config.GiftCardProdUrl,
		"sandbox client ID":     r.config.GiftCardID,
		"sandbox client secret": r.config.GiftCardKey,
		"sandbox url":           r.config.GiftCardBaseUrl,
	})

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

	logging.NewLogger().Info("Reloadly Token Request", map[string]interface{}{
		"url":     url,
		"payload": request,
		"headers": requiredHeaders,
	})

	resp, err := r.MakeRequest("POST", url, request, requiredHeaders)
	if err != nil {
		logging.NewLogger().Error("Reloadly Token Request Error", err)
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)
	logging.NewLogger().Info("Reloadly Token Response", map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
	})

	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("Reloadly Token Error", map[string]interface{}{
			"status_code": resp.StatusCode,
			"body":        string(respBody),
		})
		return "", fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	var apiResponse reloadlymodels.TokenApiResponse
	err = json.Unmarshal(respBody, &apiResponse)
	if err != nil {
		logging.NewLogger().Error("Reloadly Token Unmarshal Error", err)
		return "", fmt.Errorf("error parsing products: %w", err)
	}

	logging.NewLogger().Info("Reloadly Token Value", map[string]interface{}{
		"access_token": maskToken(apiResponse.AccessToken),
		"expires_in":   apiResponse.ExpiresIn,
	})

	/// Set AccessToken
	r.token = reloadlymodels.TokenApiStore{
		Token:    apiResponse,
		Audience: audience,
	}
	return apiResponse.AccessToken, nil
}

func maskToken(token string) string {
	if len(token) <= 10 {
		return "***"
	}
	return token[:5] + "..." + token[len(token)-5:]
}

func (r *ReloadlyProvider) BuyGiftCard(request *reloadlymodels.GiftCardPurchaseRequest) (*reloadlymodels.GiftCardPurchaseResponse, error) {
	token, err := r.GetToken(reloadlymodels.SANDBOX) // Change to prod
	if err != nil {
		return nil, err
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token

	base, err := url.Parse(r.config.GiftCardBaseUrl) // Change to prod
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	base.Path += "/orders"

	logging.NewLogger().Info("Reloadly BuyGiftCard Request", map[string]any{
		"url":     base.String(),
		"headers": requiredHeaders,
		"payload": request,
	})

	resp, err := r.MakeRequest("POST", base.String(), *request, requiredHeaders)
	if err != nil {
		logging.NewLogger().Error("Reloadly BuyGiftCard Request Error", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	logging.NewLogger().Info("Reloadly BuyGiftCard Response", map[string]any{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
	})

	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Error("Reloadly BuyGiftCard Error", map[string]any{
			"status_code": resp.StatusCode,
			"body":        string(respBody),
		})
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	var response reloadlymodels.GiftCardPurchaseResponse
	err = json.Unmarshal(respBody, &response)
	if err != nil {
		logging.NewLogger().Error("Reloadly BuyGiftCard Unmarshal Error", err)
		return nil, fmt.Errorf("error parsing products: %w", err)
	}

	return &response, nil
}

func (r *ReloadlyProvider) GetReedemInsrtructionByProductID(productID string) (*reloadlymodels.RedeemInstruction, error) {
	token, err := r.GetToken(reloadlymodels.SANDBOX) // Change to prod
	if err != nil {
		return nil, err
	}
	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token
	base, err := url.Parse(r.config.GiftCardBaseUrl) // Change to prod
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	base.Path += fmt.Sprintf("/products/%s/redeem-instructions", productID)
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

	var response reloadlymodels.RedeemInstruction
	// Decode the response body
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		logging.NewLogger().Error("Reloadly GetRedeemInstruction Unmarshal Error", err)
		return nil, fmt.Errorf("error parsing redeem instructions: %w", err)
	}

	return &response, nil
}

func (r *ReloadlyProvider) GetCardInfo(request string) (*reloadlymodels.ReedemGiftCardResponse, error) {
	token, err := r.GetToken(reloadlymodels.SANDBOX) // Change to prod
	if err != nil {
		return nil, err
	}

	var requiredHeaders = make(map[string]string)
	requiredHeaders["Accept"] = "application/com.reloadly.giftcards-v1+json"
	requiredHeaders["Authorization"] = "Bearer " + token

	base, err := url.Parse(r.config.GiftCardBaseUrl) // Change to prod
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
	var response reloadlymodels.ReedemGiftCardResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&response)
	if err != nil {
		return nil, fmt.Errorf("error parsing products: %w", err)
	}

	return &response, nil
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (r *ReloadlyProvider) GetReloadlyToken() (string, error) {
	reqUrl := "https://auth.reloadly.com/oauth/token"

	body := map[string]string{
		"client_id":     r.config.GiftCardID,
		"client_secret": r.config.GiftCardKey,
		"grant_type":    "client_credentials",
		"audience":      r.config.GiftCardBaseUrl,
	}

	jsonData, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", reqUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get token: %s", string(respBody))
	}

	var tokenResp TokenResponse
	err = json.Unmarshal(respBody, &tokenResp)
	if err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func (r *ReloadlyProvider) BuyReloadlyGiftCard(token string, request *reloadlymodels.GiftCardPurchaseRequest) (*reloadlymodels.GiftCardPurchaseResponse, error) {
	reqUrl := "https://giftcards-sandbox.reloadly.com/orders"
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", reqUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/com.reloadly.giftcards-v1+json")
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gift card purchase failed [%d]: %s", resp.StatusCode, string(body))
	}

	var result reloadlymodels.GiftCardPurchaseResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse gift card response: %w", err)
	}

	return &result, nil
}
