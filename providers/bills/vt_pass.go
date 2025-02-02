package bills

import (
	"bytes"
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

type VTPassProvider struct {
	providers.BaseProvider
	config *BillConfig
}

type BillConfig struct {
	BillProviderName string `mapstructure:"BILL_PROVIDER_NAME"`
	VTPassBaseUrl    string `mapstructure:"VT_BASE_URL"`
	VTPassKey        string `mapstructure:"VT_PASS_KEY"`
	VTPassPK         string `mapstructure:"VT_PASS_PK"`
	VTPassSK         string `mapstructure:"VT_PASS_SK"`
}

func NewBillProvider() *VTPassProvider {

	var c BillConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &VTPassProvider{
		BaseProvider: providers.BaseProvider{
			Name:    c.BillProviderName,
			BaseURL: c.VTPassBaseUrl,
			APIKey:  c.VTPassKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

func (p *VTPassProvider) GetServiceCategories() (interface{}, error) {

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "service-categories"

	resp, err := p.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
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

	// Decode the response body
	var newModel VTPassResponse[[]ServiceCategory]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Content, nil
}

func (p *VTPassProvider) GetServiceIdentifiers(identifier string) (interface{}, error) {

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	base.Path += "services"

	// Add query
	q := base.Query()
	q.Set("identifier", identifier)
	base.RawQuery = q.Encode()

	resp, err := p.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
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

	// Decode the response body
	var newModel VTPassResponse[[]ServiceIdentifier]
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Content, nil
}

func (p *VTPassProvider) GetServiceVariation(serviceID string) ([]Variation, error) {

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	base.Path += "service-variations"

	// Add query
	q := base.Query()
	q.Set("serviceID", serviceID)
	base.RawQuery = q.Encode()

	resp, err := p.MakeRequest("GET", base.String(), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read response body", err)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Log the body
	logging.NewLogger().Error(fmt.Sprintf("response body: %v\nresponse statusCode: %v", string(bodyBytes), resp.StatusCode))

	// Reset the response body for further processing (if needed)
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel VTPassResponse[ServiceContentWithVariation]
	/// TODO: We may need to parse into VTPassResponse before unmarshalling the rest
	/// This is because VTPass is
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	if newModel.Content.Variations == nil {
		newModel.Content.Variations = []Variation{} // Initialize empty slice
	}
	return newModel.Content.Variations, nil
}

func (p *VTPassProvider) BuyAirtime(request PurchaseAirtimeRequest) (*Transaction, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	base.Path += "pay"
	headers := map[string]string{
		"public-key": p.config.VTPassPK,
		"secret-key": p.config.VTPassSK,
		"api-key":    p.config.VTPassKey,
	}

	resp, err := p.MakeRequest("POST", base.String(), request, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read response body", err)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Log the body
	logging.NewLogger().Error(fmt.Sprintf("response body: %v\nresponse statusCode: %v", string(bodyBytes), resp.StatusCode))

	// Reset the response body for further processing (if needed)
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel PayResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Content.Transaction, nil
}

func (p *VTPassProvider) BuyData(request PurchaseDataRequest) (*Transaction, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	base.Path += "pay"
	headers := map[string]string{
		"public-key": p.config.VTPassPK,
		"secret-key": p.config.VTPassSK,
		"api-key":    p.config.VTPassKey,
	}

	resp, err := p.MakeRequest("POST", base.String(), request, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read response body", err)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Log the body
	logging.NewLogger().Error(fmt.Sprintf("response body: %v\nresponse statusCode: %v", string(bodyBytes), resp.StatusCode))

	// Reset the response body for further processing (if needed)
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel PayResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Content.Transaction, nil
}

func (p *VTPassProvider) GetCustomerInfo(request GetCustomerInfoRequest) (*CustomerInfo, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	base.Path += "merchant-verify"
	headers := map[string]string{
		"public-key": p.config.VTPassPK,
		"secret-key": p.config.VTPassSK,
		"api-key":    p.config.VTPassKey,
	}

	resp, err := p.MakeRequest("POST", base.String(), request, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read response body", err)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Log the body
	logging.NewLogger().Error(fmt.Sprintf("response body: %v\nresponse statusCode: %v", string(bodyBytes), resp.StatusCode))

	// Decode the response body
	// First try decoding as success response
	var successModel VTPassResponse[CustomerInfo]
	decoder := json.NewDecoder(bytes.NewBuffer(bodyBytes))
	err = decoder.Decode(&successModel)
	if err == nil {
		return &successModel.Content, nil
	}

	// If that fails, try decoding as error response
	var errorModel VTPassResponse[VTPassError[[]VTPassErrorItem]]
	decoder = json.NewDecoder(bytes.NewBuffer(bodyBytes))
	if err := decoder.Decode(&errorModel); err != nil {
		// Try simpler error format
		var simpleErrorModel VTPassError[string]
		decoder = json.NewDecoder(bytes.NewBuffer(bodyBytes))
		if err := decoder.Decode(&simpleErrorModel); err != nil {
			return nil, fmt.Errorf("error decoding response body: %w", err)
		}
	}

	return nil, fmt.Errorf("API error: %s", errorModel.ResponseDescription)
}

func (p *VTPassProvider) BuyTVSubscription(request BuyTVSubscriptionRequest) (*Transaction, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	base.Path += "pay"
	headers := map[string]string{
		"public-key": p.config.VTPassPK,
		"secret-key": p.config.VTPassSK,
		"api-key":    p.config.VTPassKey,
	}

	resp, err := p.MakeRequest("POST", base.String(), request, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read response body", err)
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Log the body
	logging.NewLogger().Error(fmt.Sprintf("response body: %v\nresponse statusCode: %v", string(bodyBytes), resp.StatusCode))

	// Reset the response body for further processing (if needed)
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel PayResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Content.Transaction, nil
}
