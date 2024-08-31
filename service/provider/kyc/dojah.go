package kyc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/service/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	dojahmodels "github.com/SwiftFiat/SwiftFiat-Backend/service/provider/kyc/dojah_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type DOJAHProvider struct {
	provider.BaseProvider
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
		BaseProvider: provider.BaseProvider{
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

func (p *DOJAHProvider) ValidateBVN(bvn string, first_name string, last_name string, dob string) (*dojahmodels.BVNEntity, error) {
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
	base.Path += "api/v1/kyc/bvn"

	// Query params
	params := url.Values{}
	params.Add("bvn", bvn)
	params.Add("first name", first_name)
	params.Add("last name", last_name)
	params.Add("dob", dob)
	base.RawQuery = params.Encode()

	resp, err := p.MakeRequest("GET", base.String(), nil, requiredHeaders)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		logging.NewLogger().Debug(resp)
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
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

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
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
