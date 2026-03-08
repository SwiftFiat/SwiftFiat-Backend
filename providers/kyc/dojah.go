package kyc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
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
				Timeout: time.Second * 60,
				Transport: &http.Transport{
					TLSHandshakeTimeout: 30 * time.Second,
				},
			},
		},
		config: &c,
	}
}

func (p *DOJAHProvider) getFullURL(relativePath string) (*url.URL, error) {
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, err
	}

	// Normalize base path: ensure it ends with api/v1 if not present
	// and handle trailing slashes
	u.Path = strings.TrimSuffix(u.Path, "/")
	if !strings.Contains(u.Path, "api/v1") {
		u.Path = path.Join(u.Path, "api/v1")
	}

	// Append relative path
	u.Path = path.Join(u.Path, relativePath)
	return u, nil
}

func (p *DOJAHProvider) ValidateBVN(bvn string, first_name string, last_name string, dob *string) (*dojahmodels.BVNEntity, error) {
	// Implementation for BVN verification
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = p.config.KYCProviderID
	requiredHeaders["Authorization"] = p.config.KYCProviderKey

	fullURL, err := p.getFullURL("kyc/bvn")
	if err != nil {
		return nil, fmt.Errorf("failed to construct URL: %w", err)
	}

	// Query params
	params := url.Values{}
	params.Add("bvn", bvn)
	params.Add("first_name", first_name)
	params.Add("last_name", last_name)
	// if dob != nil {
	// 	params.Add("dob", *dob)
	// }
	fullURL.RawQuery = params.Encode()

	resp, err := p.MakeRequest("GET", fullURL.String(), nil, requiredHeaders)
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
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s \nBody: %s", resp.StatusCode, resp.Request.URL, string(bodyBytes))
	}

	// Check if content type is JSON
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return nil, fmt.Errorf("expected application/json response but got %s. Body: %s", contentType, string(bodyBytes))
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

func (p *DOJAHProvider) ValidateNIN(request interface{}) (*dojahmodels.NINEntity, error) {
	// Implementation for BVN verification
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = p.config.KYCProviderID
	requiredHeaders["Authorization"] = p.config.KYCProviderKey

	fullURL, err := p.getFullURL("kyc/nin/verify")
	if err != nil {
		return nil, fmt.Errorf("failed to construct URL: %w", err)
	}

	resp, err := p.MakeRequest("POST", fullURL.String(), request, requiredHeaders)
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
		return nil, fmt.Errorf("unexpected status code: %d \nURL: %s \nBody: %s", resp.StatusCode, resp.Request.URL, string(bodyBytes))
	}

	// Check if content type is JSON
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		return nil, fmt.Errorf("expected application/json response but got %s. Body: %s", contentType, string(bodyBytes))
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
