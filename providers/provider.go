package providers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
)

const (
	Dojah     = "DOJAH"
	Reloadly  = "RELOADLY"
	Bitgo     = "BITGO"
	CoinGecko = "COINGECKO"
	Paystack  = "PAYSTACK"
	VTPass    = "VTPASS"
	Cryptomus = "CRYPTOMUS"
)

// BaseProvider contains common fields and methods
type BaseProvider struct {
	Name    string
	BaseURL string
	APIKey  string
	Client  *http.Client
}

// Request Processing
func (p *BaseProvider) MakeRequest(method, url string, body interface{}, extraHeaders map[string]string) (*http.Response, error) {

	var req *http.Request
	var err error

	requestLog := struct {
		Method string
		URL    string
		Body   interface{}
	}{
		Method: method,
		URL:    url,
		Body:   body,
	}

	logging.NewLogger().Info("External Request", requestLog)

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err = http.NewRequest(method, url, bytes.NewBuffer(jsonBody))
		if err != nil {
			return nil, err
		}
	} else {
		req, err = http.NewRequest(method, url, nil)
	}

	if err != nil {
		return nil, err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	// Allows for overwriting pre-set keys
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	// Make the request
	return p.Client.Do(req)
}

// Provider is an interface that all specific providers must implement
type Provider interface {
	GetName() string
	GetBaseURL() string
	GetAPIKey() string
	GetClient() *http.Client
}

// ProviderService manages multiple providers
type ProviderService struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewProviderService initializes a new ProviderService
func NewProviderService() *ProviderService {
	return &ProviderService{
		providers: make(map[string]Provider),
	}
}

// AddProvider adds a new provider to the service
func (s *ProviderService) AddProvider(provider Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[provider.GetName()] = provider
}

// GetProvider retrieves a provider by name
func (s *ProviderService) GetProvider(name string) (Provider, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	provider, exists := s.providers[name]
	return provider, exists
}

// Implement the Provider interface methods for BaseProvider
func (bp *BaseProvider) GetName() string         { return bp.Name }
func (bp *BaseProvider) GetBaseURL() string      { return bp.BaseURL }
func (bp *BaseProvider) GetAPIKey() string       { return bp.APIKey }
func (bp *BaseProvider) GetClient() *http.Client { return bp.Client }
