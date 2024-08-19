package kyc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

func (p *DOJAHProvider) VerifyBVN(bvn string) (*dojahmodels.BVNEntity, error) {
	// Implementation for BVN verification
	// This would use the BaseProvider's fields to make the actual HTTP request
	// ...

	var requiredHeaders = make(map[string]string)
	requiredHeaders["AppId"] = p.config.KYCProviderID
	requiredHeaders["Authorization"] = p.config.KYCProviderKey

	resp, err := p.MakeRequest("GET", fmt.Sprintf("/api/v1/kyc/bvn/full?bvn=%v", bvn), nil, requiredHeaders)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Decode the response body
	var newModel dojahmodels.Response
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&newModel)
	if err != nil {
		return nil, fmt.Errorf("error decoding response body: %w", err)
	}

	return &newModel.Entity, nil
}
