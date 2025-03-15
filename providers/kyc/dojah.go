package kyc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	dojahmodels "github.com/SwiftFiat/SwiftFiat-Backend/providers/kyc/dojah_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/sirupsen/logrus"
)

type DOJAHProvider struct {
	providers.BaseProvider
	config *KYCConfig
}

type KYCConfig struct {
	KYCProviderName    string `mapstructure:"KYC_PROVIDER_NAME"`
	KYCProviderID      string `mapstructure:"DOJAH_APP_ID"`
	KYCProviderKey     string `mapstructure:"DOJAH_KEY"`
	KYCProviderBaseUrl string `mapstructure:"DOJAH_BASE_URL"`
}

func NewKYCProvider() *DOJAHProvider {

	var c KYCConfig

	err := utils.LoadCustomConfig(utils.EnvPath, &c)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	return &DOJAHProvider{
		BaseProvider: providers.BaseProvider{
			Name:    c.KYCProviderName,
			BaseURL: c.KYCProviderBaseUrl,
			APIKey:  c.KYCProviderKey,
			Client: &http.Client{
				Timeout: time.Second * 30,
			},
		},
		config: &c,
	}
}

func (p *DOJAHProvider) ValidateBVN(bvn string, first_name string, last_name string, dob *string) (*dojahmodels.BVNEntity, error) {
	// Implementation for BVN Validation
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = p.config.KYCProviderID
	requiredHeaders["Authorization"] = p.config.KYCProviderKey

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "api/v1/kyc/bvn"

	// Query params
	params := url.Values{}
	params.Add("bvn", bvn)
	params.Add("first_name", first_name)
	params.Add("last_name", last_name)
	if dob != nil {
		params.Add("dob", *dob)
	}
	base.RawQuery = params.Encode()

	resp, err := p.MakeRequest("GET", base.String(), nil, requiredHeaders)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read and log the full response body for tracking
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.NewLogger().Error("Failed to read response body", err)
	} else {
		logFields := logrus.Fields{
			"status_code": resp.StatusCode,
			"url":         resp.Request.URL,
			"headers":     resp.Header,
			"body":        string(bodyBytes),
		}

		if resp.StatusCode == http.StatusOK {
			logging.NewLogger().WithFields(logFields).Info("Successful response from Dojah API")
		} else {
			logging.NewLogger().WithFields(logFields).Error("Unexpected response from Dojah API")
		}
	}

	// Reset the response body for subsequent reads
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel dojahmodels.BVNResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Entity, nil
}

func (p *DOJAHProvider) VerifyBVN(request interface{}) (*dojahmodels.BVNVerificationEntity, error) {
	// Implementation for BVN verification
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = p.config.KYCProviderID
	requiredHeaders["Authorization"] = p.config.KYCProviderKey

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "api/v1/kyc/bvn/verify"

	resp, err := p.MakeRequest("POST", base.String(), request, requiredHeaders)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response body for logging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	// Log request and response details
	logFields := logrus.Fields{
		"status_code": resp.StatusCode,
		"url":         resp.Request.URL,
		"method":      resp.Request.Method,
		"response":    string(bodyBytes),
	}

	if resp.StatusCode == http.StatusOK {
		logging.NewLogger().WithFields(logFields).Info("Successful response from Dojah API")
	} else {
		logging.NewLogger().WithFields(logFields).Error("Unexpected response from Dojah API")
	}

	// Reset the response body for subsequent reads
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel dojahmodels.BVNVerificationResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Entity, nil
}

func (p *DOJAHProvider) VerifyNIN(request interface{}) (*dojahmodels.NINEntity, error) {
	// Implementation for NIN Verification
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = p.config.KYCProviderID
	requiredHeaders["Authorization"] = p.config.KYCProviderKey

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected status code: %v", err.Error())
	}

	// Path params
	base.Path += "api/v1/kyc/nin/verify"

	resp, err := p.MakeRequest("POST", base.String(), request, requiredHeaders)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response body for logging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	// Log request and response details
	logFields := logrus.Fields{
		"status_code": resp.StatusCode,
		"url":         resp.Request.URL,
		"method":      resp.Request.Method,
		"response":    string(bodyBytes),
	}

	if resp.StatusCode == http.StatusOK {
		logging.NewLogger().WithFields(logFields).Info("Successful response from Dojah API")
	} else {
		logging.NewLogger().WithFields(logFields).Error("Unexpected response from Dojah API")
	}

	// Reset the response body for subsequent reads
	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s", resp.StatusCode, resp.Request.URL)
	}

	// Decode the response body
	var newModel dojahmodels.NINResponse
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Entity, nil
}
