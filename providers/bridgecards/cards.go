package bridgecards

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	aesbridge "github.com/mervick/aes-bridge-go"
)

const (
	ProductionBaseURL = "https://issuecards.api.bridgecard.co/v1/issuing"
	SandboxBaseURL    = "https://issuecards.api.bridgecard.co/v1/issuing/sandbox"
)

// BridgeCardProvider handles all interactions with BridgeCard API
type BridgeCardProvider struct {
	authToken  string
	secreyKey  string
	webhookKey string
	baseURL    string
	config     *utils.Config
	httpClient *http.Client
	logger     *logging.Logger
}

// NewBridgeCardProvider creates a new BridgeCard provider instance
func NewBridgeCardProvider(config *utils.Config, useSandbox bool, logger *logging.Logger) *BridgeCardProvider {
	baseURL := ProductionBaseURL
	secretKey := config.BridgeCardsSecretKey
	webhookKey := config.BridgeCardsWebhookKey
	authToken := config.BridgeCardsAuthToken
	if useSandbox {
		baseURL = SandboxBaseURL
		secretKey = config.BridgeCardsTestSecretKey
		webhookKey = config.BridgeCardsTestWebhookKey
		authToken = config.BridgeCardsTestAuthToken
	}

	return &BridgeCardProvider{
		authToken:  authToken,
		secreyKey:  secretKey,
		webhookKey: webhookKey,
		baseURL:    baseURL,
		config:     config,
		httpClient: &http.Client{
			Timeout: 50 * time.Second,
		},
		logger: logger,
	}
}

// CreateCardHolder creates a new card holder
func (p *BridgeCardProvider) CreateCardHolder(ctx context.Context, req *CreateCardHolderRequest) (*CreateCardHolderResponse, error) {
	var response CreateCardHolderResponse

	err := p.makeRequest(ctx, http.MethodPost, "/cardholder/register_cardholder", req, &response)
    if err != nil {
        return nil, fmt.Errorf("failed to register cardholder: %w", err)
    }

	return &response, nil
}

// CreateCardHolderAndCard creates a new card holder and card in one request
// func (p *BridgeCardProvider) CreateCardHolderAndCard(ctx context.Context, req *CreateCardHolderRequest) (*CreateCardResponse, error) {
// 	var response CreateCardResponse
// 	err := p.makeRequest(ctx, http.MethodPost, "/cardholder/register_cardholder", req, &response)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to register cardholder: %w", err)
// 	}
// 	return &response, nil
// }

// CreateCard creates a new virtual card
func (p *BridgeCardProvider) CreateCard(ctx context.Context, req *CreateCardRequest) (*CreateCardResponse, error) {
	var response CreateCardResponse
	defaultPin, err := aesbridge.Encrypt("1234", p.secreyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt default pin: %v", err)
	}

	req.Pin = defaultPin
	err = p.makeRequest(ctx, http.MethodPost, "/issuing/cards/create_card", req, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to create card: %w", err)
	}

	return &response, nil
}

// GetCardHolder retrieves cardholder details by BridgeCard cardholder ID
func (p *BridgeCardProvider) GetCardHolder(ctx context.Context, cardholderID string) (*CardHolder, error) {
    var response struct {
        Status  string   `json:"status"`
        Message string   `json:"message"`
        Data    CardHolder `json:"data"`
    }

    endpoint := fmt.Sprintf("/cardholder/%s", cardholderID)
    if err := p.makeRequest(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
        return nil, fmt.Errorf("failed to get cardholder: %w", err)
    }

    return &response.Data, nil
}

// GetCard retrieves card details
func (p *BridgeCardProvider) GetCard(ctx context.Context, cardID string) (*Card, error) {
	var response struct {
		Card Card `json:"card"`
	}

	err := p.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/issuing/cards/%s", cardID), nil, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to get card: %w", err)
	}

	return &response.Card, nil
}

func (p *BridgeCardProvider) makeRequest(ctx context.Context, method, endpoint string, body interface{}, result interface{}) error {
	var reqBody io.Reader

	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// p.logger.Infof("auth token: %s", p.authToken)

	req.Header.Add("token", "Bearer "+p.authToken)
	req.Header.Add("Content-Type", "application/json")
	// req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiResp APIResponse
		if err := json.Unmarshal(responseBody, &apiResp); err == nil && apiResp.Error != nil {
			return fmt.Errorf("bridgecard api error [%s]: %s - %s",
				apiResp.Error.Code, apiResp.Error.Message, apiResp.Error.Details)
		}
		return fmt.Errorf("http error %d: %s", resp.StatusCode, string(responseBody))
	}

	// Parse success response
	var apiResp APIResponse
	if err := json.Unmarshal(responseBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if !apiResp.Success {
		if apiResp.Error != nil {
			return fmt.Errorf("api error [%s]: %s", apiResp.Error.Code, apiResp.Error.Message)
		}
		return fmt.Errorf("api request failed: %s", apiResp.Message)
	}

	// Unmarshal the data field into result
	if result != nil && len(apiResp.Data) > 0 {
		if err := json.Unmarshal(apiResp.Data, result); err != nil {
			return fmt.Errorf("unmarshal data: %w", err)
		}
	}

	return nil
}

// VerifyWebhookSignature verifies BridgeCard webhook signatures
// Implementation depends on BridgeCard's signature scheme
func (p *BridgeCardProvider) VerifyWebhookSignature(payload []byte, signature string) (bool, error) {
	// TODO: Implement based on BridgeCard's webhook security documentation
	// extract the x-webhook-signature header and decrypt it using your secret key
	// (live_secret_key / test_secret_key depending on the environment)
	if signature == "" {
		return false, fmt.Errorf("missing webhook signature")
	}

	// Decrypt the signature using the secret key
	decryptedSignature, err := aesbridge.Decrypt(signature, p.secreyKey)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt signature: %w", err)
	}

	// Compare decrypted signature with webhook key
	return decryptedSignature == p.webhookKey, nil
}

// ParseWebhook parses incoming webhook payload
func (p *BridgeCardProvider) ParseWebhook(payload []byte) (*WebhookEvent, error) {
	var webhook WebhookEvent
	if err := json.Unmarshal(payload, &webhook); err != nil {
		return nil, fmt.Errorf("parse webhook: %w", err)
	}

	return &webhook, nil
}

// New method to parse cardholder verification events
func (p *BridgeCardProvider) ParseCardholderVerification(payload []byte) (interface{}, error) {
	var event WebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("parse webhook: %w", err)
	}

	switch event.Event {
	case "cardholder_verification.successful":
		var success CardholderVerificationSuccess
		if err := json.Unmarshal(event.Data, &success); err != nil {
			return nil, fmt.Errorf("parse success data: %w", err)
		}
		return &success, nil

	case "cardholder_verification.failed":
		var failed CardholderVerificationFailed
		if err := json.Unmarshal(event.Data, &failed); err != nil {
			return nil, fmt.Errorf("parse failed data: %w", err)
		}
		return &failed, nil

	default:
		return nil, fmt.Errorf("unsupported webhook event: %s", event.Event)
	}
}

// New method to parse transaction webhook events
func (p *BridgeCardProvider) ParseTransactionWebhook(payload []byte) (*Transaction, error) {
	var webhook struct {
		Event string `json:"event"`
		Data  struct {
			Transaction Transaction `json:"transaction"`
		} `json:"data"`
	}

	if err := json.Unmarshal(payload, &webhook); err != nil {
		return nil, fmt.Errorf("parse transaction webhook: %w", err)
	}

	return &webhook.Data.Transaction, nil
}
