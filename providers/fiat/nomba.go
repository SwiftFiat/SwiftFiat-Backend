package fiat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

const providerName = "NOMBA"

// ── Config ────────────────────────────────────────────────────────────────────

type FiatConfig struct {
	FiatProviderName    string `mapstructure:"FIAT_PROVIDER_NAME"`
	NombaClientID       string `mapstructure:"NOMBA_CLIENT_ID"`
	NombaClientSecret   string `mapstructure:"NOMBA_CLIENT_SECRET"`
	NombaAccountID      string `mapstructure:"NOMBA_ACCOUNT_ID"`
	FiatProviderBaseUrl string `mapstructure:"NOMBA_BASE_URL"`
}

// ── Provider ──────────────────────────────────────────────────────────────────

// NombaProvider wraps the Nomba transfers API with automatic OAuth2 token
// management (obtain → cache → refresh on 401).
type NombaProvider struct {
	providers.BaseProvider
	config *FiatConfig

	// token cache – guarded by mu
	mu           sync.Mutex
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time
}

// NewFiatProvider constructs a ready-to-use NombaProvider.
// Drop-in replacement for the old NewFiatProvider() that returned *PaystackProvider.
func NewFiatProvider() *NombaProvider {
	var c FiatConfig
	if err := utils.LoadCustomConfig(utils.EnvPath, &c); err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	p := &NombaProvider{
		BaseProvider: providers.BaseProvider{
			Name:    providerName,
			BaseURL: c.FiatProviderBaseUrl,
			APIKey:  "", // Nomba uses OAuth2, not a static key
			Client: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
		config: &c,
	}

	// Eagerly obtain the first token so the first real call is fast.
	if err := p.obtainToken(); err != nil {
		logging.NewLogger().Error("nomba: initial token fetch failed", err)
	}
	return p
}

// ── OAuth2 token management ───────────────────────────────────────────────────

// bearerToken returns a valid access token, refreshing/re-issuing as needed.
func (p *NombaProvider) bearerToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.accessToken != "" && time.Now().Before(p.tokenExpiry) {
		return p.accessToken, nil
	}

	// Try refresh first; fall back to full re-issue.
	if p.refreshToken != "" {
		if err := p.refreshTokenLocked(); err == nil {
			return p.accessToken, nil
		}
	}
	if err := p.obtainTokenLocked(); err != nil {
		return "", err
	}
	return p.accessToken, nil
}

// obtainToken is the public (lock-acquiring) variant used at startup.
func (p *NombaProvider) obtainToken() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.obtainTokenLocked()
}

func (p *NombaProvider) obtainTokenLocked() error {
	endpoint := p.BaseURL + "v1/auth/token/issue"

	payload := NombaTokenRequest{
		GrantType:    "client_credentials",
		ClientID:     p.config.NombaClientID,
		ClientSecret: p.config.NombaClientSecret,
	}

	headers := map[string]string{
		"accountId": p.config.NombaAccountID,
	}

	// Don't use MakeRequest for token endpoint as it adds Bearer auth which we don't need
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nomba: marshal token request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("nomba: create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.BaseProvider.Client.Do(req)
	if err != nil {
		return fmt.Errorf("nomba: token issue request: %w", err)
	}
	defer resp.Body.Close()

	var result NombaTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("nomba: decode token response: %w", err)
	}
	if result.Code != "00" {
		return fmt.Errorf("nomba: token issue failed: %s", result.Description)
	}

	p.storeTokens(result.Data)
	return nil
}

func (p *NombaProvider) refreshTokenLocked() error {
	endpoint := p.BaseURL + "v1/auth/token/refresh"

	payload := map[string]string{
		"refresh_token": p.refreshToken,
	}

	headers := map[string]string{
		"accountId": p.config.NombaAccountID,
	}

	// Don't use MakeRequest for token endpoint as it adds Bearer auth which we don't need
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("nomba: marshal refresh request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("nomba: create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := p.BaseProvider.Client.Do(req)
	if err != nil {
		return fmt.Errorf("nomba: token refresh request: %w", err)
	}
	defer resp.Body.Close()

	var result NombaTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("nomba: decode refresh response: %w", err)
	}
	if result.Code != "00" {
		return fmt.Errorf("nomba: token refresh failed: %s", result.Description)
	}

	p.storeTokens(result.Data)
	return nil
}

func (p *NombaProvider) storeTokens(d NombaTokenData) {
	p.accessToken = d.AccessToken
	p.refreshToken = d.RefreshToken
	// Shave 60 s off expiry so we never use a token on its last breath.
	p.tokenExpiry = time.Now().Add(time.Duration(d.ExpiresIn-60) * time.Second)
}

// ── Request helper ────────────────────────────────────────────────────────────

// nombaHeaders returns the common headers required by every Nomba endpoint.
func (p *NombaProvider) nombaHeaders() (map[string]string, error) {
	token, err := p.bearerToken()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Authorization": "Bearer " + token,
		"accountId":     p.config.NombaAccountID,
	}, nil
}

// nombaCall executes a request and auto-retries once on 401 (token expired).
func (p *NombaProvider) nombaCall(method, endpoint string, body interface{}) (*http.Response, error) {
	headers, err := p.nombaHeaders()
	if err != nil {
		return nil, err
	}

	resp, err := p.MakeRequest(method, endpoint, body, headers)
	if err != nil {
		return nil, err
	}

	// On 401, invalidate cache, re-obtain, and retry once.
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()

		p.mu.Lock()
		p.accessToken = ""
		p.mu.Unlock()

		headers, err = p.nombaHeaders()
		if err != nil {
			return nil, err
		}
		resp, err = p.MakeRequest(method, endpoint, body, headers)
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// ── API methods ───────────────────────────────────────────────────────────────

// GetBanks fetches all supported banks from Nomba.
// Maps to: GET /v1/transfers/banks
func (p *NombaProvider) GetBanks() (*BankCollection, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("nomba: parse base URL: %w", err)
	}
	base.Path += "v1/transfers/banks"

	resp, err := p.nombaCall("GET", base.String(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Log response body for debugging
		bodyBytes, _ := io.ReadAll(resp.Body)
		logging.NewLogger().Error("nomba: GetBanks error response", string(bodyBytes))
		return nil, fmt.Errorf("nomba: GetBanks unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Try parsing as NombaResponse with array data
	var result NombaResponse[[]NombaBank]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("nomba: decode GetBanks: %w", err)
	}
	if result.Code != "00" {
		return nil, fmt.Errorf("nomba: GetBanks failed: %s", result.Description)
	}

	// Translate NombaBank → existing Bank type so callers need zero changes.
	collection := make(BankCollection, 0, len(result.Data))
	for _, b := range result.Data {
		collection = append(collection, Bank{
			Name: b.Name,
			Code: b.Code,
		})
	}
	return &collection, nil
}

// ResolveAccount performs an account name-enquiry against Nomba.
// Maps to: POST /v1/transfers/bank/lookup
func (p *NombaProvider) ResolveAccount(accountNumber string, bankCode string) (*AccountInfo, error) {
	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("nomba: parse base URL: %w", err)
	}
	base.Path += "v1/transfers/bank/lookup"

	body := NombaAccountLookupRequest{
		AccountNumber: accountNumber,
		BankCode:      bankCode,
	}

	resp, err := p.nombaCall("POST", base.String(), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nomba: ResolveAccount unexpected status %d", resp.StatusCode)
	}

	var result NombaResponse[NombaAccountLookupData]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("nomba: decode ResolveAccount: %w", err)
	}
	if result.Code != "00" {
		return nil, fmt.Errorf("nomba: ResolveAccount failed: %s", result.Description)
	}

	return &AccountInfo{
		AccountName:   result.Data.AccountName,
		AccountNumber: result.Data.AccountNumber,
	}, nil
}

// CreateTransferRecipient resolves the account and encodes the result as an
// opaque "recipient token" (accountNumber|bankCode|accountName) that is later
// decoded by MakeTransfer.  This preserves the existing call-site interface
// while mapping cleanly onto Nomba's single-step transfer model.
func (p *NombaProvider) CreateTransferRecipient(accountNumber string, bankCode string, name string) (*Recipient, error) {
	info, err := p.ResolveAccount(accountNumber, bankCode)
	if err != nil {
		return nil, fmt.Errorf("nomba: CreateTransferRecipient lookup: %w", err)
	}

	// Encode as a pipe-delimited token that MakeTransfer will unpack.
	token := strings.Join([]string{info.AccountNumber, bankCode, info.AccountName}, "|")

	return &Recipient{
		Active:        true,
		RecipientCode: token, // caller stores/passes this as the "recipient"
		Name:          info.AccountName,
		Details: Details{
			AccountNumber: info.AccountNumber,
			AccountName:   info.AccountName,
			BankCode:      bankCode,
		},
	}, nil
}

// MakeTransfer executes a bank transfer through Nomba.
// The `recipient` parameter must be the token produced by CreateTransferRecipient
// ("accountNumber|bankCode|accountName").
// Maps to: POST /v2/transfers/bank
func (p *NombaProvider) MakeTransfer(recipient, merchantTxRef, narration string, amount int64, senderName string) (*NombaTransferResponse, error) {
	// Parse the opaque recipient token.
	parts := strings.SplitN(recipient, "|", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("nomba: MakeTransfer invalid recipient token %q (want accountNumber|bankCode|accountName)", recipient)
	}
	accountNumber, bankCode, accountName := parts[0], parts[1], parts[2]

	base, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("nomba: parse base URL: %w", err)
	}
	base.Path += "v1/transfers/bank"

	body := NombaBankTransferRequest{
		Amount:        amount,
		AccountNumber: accountNumber,
		AccountName:   accountName,
		BankCode:      bankCode,
		MerchantTxRef: merchantTxRef,
		SenderName:    senderName,
		Narration:     narration,
	}

	resp, err := p.nombaCall("POST", base.String(), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response body for error details
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Accept both 200 (OK) and 202 (Accepted - processing) as valid responses
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		logging.NewLogger().Error("nomba: MakeTransfer non-success status", resp.StatusCode, "body", string(bodyBytes))
		// Try to parse error response for more details
		var errResult NombaResponse[interface{}]
		if err := json.Unmarshal(bodyBytes, &errResult); err == nil {
			return nil, fmt.Errorf("nomba: MakeTransfer status %d: %s", resp.StatusCode, errResult.Description)
		}
		return nil, fmt.Errorf("nomba: MakeTransfer unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result NombaResponse[NombaTransferData]
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("nomba: decode MakeTransfer: %w", err)
	}
	// Accept codes: "00" (legacy), "200" (success), "202" (processing/accepted)
	if result.Code != "00" && result.Code != "200" && result.Code != "202" {
		return nil, fmt.Errorf("nomba: MakeTransfer failed with code %s: %s", result.Code, result.Description)
	}

	d := result.Data
	// Amount is returned as string from Nomba API, convert to int64
	amountFloat, err := strconv.ParseFloat(d.Amount, 64)
	if err != nil {
		return nil, fmt.Errorf("nomba: invalid amount format: %w", err)
	}
	amountInt := int64(amountFloat)
	return &NombaTransferResponse{
		Amount:       amountInt,
		Currency:     "NGN",
		Reference:    merchantTxRef,
		Reason:       narration,
		Status:       d.Status,
		TransferCode: d.ID,
	}, nil
}
