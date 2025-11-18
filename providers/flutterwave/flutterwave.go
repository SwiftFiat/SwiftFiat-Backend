package flutterwave

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client handles all Flutterwave API v4 interactions
type Client struct {
	BaseURL    string
	SecretKey  string
	PublicKey  string
	HTTPClient *http.Client
}

// NewClient creates a new Flutterwave API client
func NewClient(secretKey, publicKey string, sandbox bool) *Client {
	baseURL := FlutterwaveBaseURL
	if sandbox {
		baseURL = FlutterwaveSandboxURL
	}

	return &Client{
		BaseURL:   baseURL,
		SecretKey: secretKey,
		PublicKey: publicKey,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ============================================================================
// VIRTUAL CARD OPERATIONS
// ============================================================================

// CreateVirtualCard creates a new virtual card
func (c *Client) CreateVirtualCard(ctx context.Context, req CreateCardRequest) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards", c.BaseURL)

	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "POST", endpoint, req, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to create card: %s", response.Message)
	}

	return &response.Data, nil
}

// GetCard retrieves a virtual card by ID
func (c *Client) GetCard(ctx context.Context, cardID string) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s", c.BaseURL, cardID)

	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to get card: %s", response.Message)
	}

	return &response.Data, nil
}

// ListCards retrieves all virtual cards
func (c *Client) ListCards(ctx context.Context, page, pageSize int) (*ListCardsResponse, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards?page=%d&page_size=%d", c.BaseURL, page, pageSize)

	var response struct {
		APIResponse
		Data ListCardsResponse `json:"data"`
	}

	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to list cards: %s", response.Message)
	}

	return &response.Data, nil
}

// FundCard adds funds to a virtual card
func (c *Client) FundCard(ctx context.Context, cardID string, req FundCardRequest) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/fund", c.BaseURL, cardID)

	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "POST", endpoint, req, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to fund card: %s", response.Message)
	}

	return &response.Data, nil
}

// WithdrawFromCard withdraws funds from a virtual card
func (c *Client) WithdrawFromCard(ctx context.Context, cardID string, req WithdrawCardRequest) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/withdraw", c.BaseURL, cardID)

	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "POST", endpoint, req, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to withdraw from card: %s", response.Message)
	}

	return &response.Data, nil
}

// TerminateCard terminates/deletes a virtual card
func (c *Client) TerminateCard(ctx context.Context, cardID string) error {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/terminate", c.BaseURL, cardID)

	var response APIResponse

	if err := c.doRequest(ctx, "PUT", endpoint, nil, &response); err != nil {
		return err
	}

	if response.Status != "success" {
		return fmt.Errorf("failed to terminate card: %s", response.Message)
	}

	return nil
}

// FreezeCard freezes a virtual card
func (c *Client) FreezeCard(ctx context.Context, cardID string) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/freeze", c.BaseURL, cardID)

	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "PUT", endpoint, nil, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to freeze card: %s", response.Message)
	}

	return &response.Data, nil
}

// UnfreezeCard unfreezes a virtual card
func (c *Client) UnfreezeCard(ctx context.Context, cardID string) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/unfreeze", c.BaseURL, cardID)

	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "PUT", endpoint, nil, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to unfreeze card: %s", response.Message)
	}

	return &response.Data, nil
}

// GetCardTransactions retrieves transactions for a card
func (c *Client) GetCardTransactions(ctx context.Context, cardID string, page, pageSize int, fromDate, toDate string) (*ListTransactionsResponse, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/transactions?page=%d&page_size=%d",
		c.BaseURL, cardID, page, pageSize)

	if fromDate != "" {
		endpoint += fmt.Sprintf("&from=%s", fromDate)
	}
	if toDate != "" {
		endpoint += fmt.Sprintf("&to=%s", toDate)
	}

	var response struct {
		APIResponse
		Data ListTransactionsResponse `json:"data"`
	}

	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to get transactions: %s", response.Message)
	}

	return &response.Data, nil
}

// GetCardBalance retrieves the current balance of a card
func (c *Client) GetCardBalance(ctx context.Context, cardID string) (float64, error) {
	card, err := c.GetCard(ctx, cardID)
	if err != nil {
		return 0, err
	}
	return card.Amount, nil
}

// ============================================================================
// HTTP CLIENT HELPER
// ============================================================================

func (c *Client) doRequest(ctx context.Context, method, url string, body any, result any) error {
	var reqBody io.Reader

	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.SecretKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Handle error responses
	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
		}
		return fmt.Errorf("API error: %s - %s", errResp.Code, errResp.Message)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// ============================================================================
// WEBHOOK VERIFICATION
// ============================================================================

// VerifyWebhookSignature verifies the signature of a webhook payload
func (c *Client) VerifyWebhookSignature(payload []byte, signature string) bool {
	h := hmac.New(sha256.New, []byte(c.SecretKey))
	h.Write(payload)
	expectedSignature := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}


// ============================================================================
// BALANCE & WALLET OPERATIONS
// ============================================================================

// GetBalance retrieves the Flutterwave account balance
func (c *Client) GetBalance(ctx context.Context, currency string) (float64, error) {
	endpoint := fmt.Sprintf("%s/balances/%s", c.BaseURL, currency)
	
	var response struct {
		APIResponse
		Data struct {
			Currency      string  `json:"currency"`
			Balance       float64 `json:"balance"`
			AvailableBalance float64 `json:"available_balance"`
			LedgerBalance float64 `json:"ledger_balance"`
		} `json:"data"`
	}

	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return 0, err
	}

	if response.Status != "success" {
		return 0, fmt.Errorf("failed to get balance: %s", response.Message)
	}

	return response.Data.AvailableBalance, nil
}

// UpdateCardDesign updates the visual design of a virtual card
func (c *Client) UpdateCardDesign(ctx context.Context, req CardDesignRequest) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/design", c.BaseURL, req.CardID)
	
	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "PUT", endpoint, req, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to update card design: %s", response.Message)
	}

	return &response.Data, nil
}

// ============================================================================
// CARD LIMITS & RESTRICTIONS (v4 features)
// ============================================================================

// SetCardLimits sets spending limits and restrictions on a card
func (c *Client) SetCardLimits(ctx context.Context, cardID string, req CardLimitsRequest) (*VirtualCard, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/limits", c.BaseURL, cardID)
	
	var response struct {
		APIResponse
		Data VirtualCard `json:"data"`
	}

	if err := c.doRequest(ctx, "PUT", endpoint, req, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to set card limits: %s", response.Message)
	}

	return &response.Data, nil
}

// ============================================================================
// CARD ANALYTICS & INSIGHTS (v4 features)
// ============================================================================

// GetCardAnalytics retrieves spending analytics for a card
func (c *Client) GetCardAnalytics(ctx context.Context, cardID string, period string) (*CardAnalytics, error) {
	endpoint := fmt.Sprintf("%s/virtual-cards/%s/analytics?period=%s", c.BaseURL, cardID, period)
	
	var response struct {
		APIResponse
		Data CardAnalytics `json:"data"`
	}

	if err := c.doRequest(ctx, "GET", endpoint, nil, &response); err != nil {
		return nil, err
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("failed to get analytics: %s", response.Message)
	}

	return &response.Data, nil
}