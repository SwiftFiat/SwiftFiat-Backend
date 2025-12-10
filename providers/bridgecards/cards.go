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

func (p *BridgeCardProvider) GetCardBalance(ctx context.Context, cardID string) (*GetCardBalanceResponse, error) {
	var response GetCardBalanceResponse
	err := p.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/cards/get_card_balance?card_id=%s", cardID), nil, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to get card balance: %w", err)
	}
	p.logger.Infof("card balance data: %v", response.Data)
	return &response, nil
}

// CreateCard creates a new virtual card
func (p *BridgeCardProvider) CreateCard(ctx context.Context, req *CreateCardRequest) (*CreateCardResponse, error) {
	var response CreateCardResponse
	defaultPin, err := aesbridge.Encrypt("1234", p.secreyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt default pin: %v", err)
	}

	req.Pin = defaultPin
	err = p.makeRequest(ctx, http.MethodPost, "/cards/create_card", req, &response)
	if err != nil {
		p.logger.Errorf("failed to create card: %v", err)
		return nil, fmt.Errorf("failed to create card: %w", err)
	}

	p.logger.Infof("create card response in provider is ====: %v", response)

	return &response, nil
}

// GetCardHolder retrieves cardholder details by BridgeCard cardholder ID
func (p *BridgeCardProvider) GetCardHolder(ctx context.Context, cardholderID string) (*CardHolder, error) {
	var response struct {
		Status  string     `json:"status"`
		Message string     `json:"message"`
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

	err := p.makeRequest(ctx, http.MethodGet, fmt.Sprintf("/cards/%s", cardID), nil, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to get card: %w", err)
	}

	return &response.Card, nil
}

// GetCard retrieves card details
func (p *BridgeCardProvider) FundIssuingWallet(ctx context.Context, req FundIssuingWalletRequest) (*string, error) {
	var response struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	err := p.makeRequest(ctx, http.MethodPatch, "/cards/fund_issuing_wallet?currency=USD", req, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to get card: %w", err)
	}

	return &response.Message, nil
}

func (p *BridgeCardProvider) FreezeCard(ctx context.Context, cardID string) (*FreezeCardResponse, error) {
	var response FreezeCardResponse
	err := p.makeRequest(ctx, http.MethodPatch, fmt.Sprintf("/cards/freeze_card?card_id=%s", cardID), nil, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to freeze card: %w", err)
	}
	return &response, nil
}

func (p *BridgeCardProvider) UnfreezeCard(ctx context.Context, cardID string) (*FreezeCardResponse, error) {
	var response FreezeCardResponse
	err := p.makeRequest(ctx, http.MethodPatch, fmt.Sprintf("/cards/unfreeze_card?card_id=%s", cardID), nil, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to unfreeze card: %w", err)
	}
	return &response, nil
}

func (p *BridgeCardProvider) FundCard(ctx context.Context, req FundCardRequest) (*FundCardResponse, error) {
	var response FundCardResponse
	err := p.makeRequest(ctx, http.MethodPatch, "/cards/fund_card_asynchronously", req, &response)
	if err != nil {
		return nil, fmt.Errorf("failed to fund card: %w", err)
	}
	return &response, nil
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

	finalURL := p.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, finalURL, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if p.logger != nil {
		p.logger.Infof("BridgeCard request: %s %s", method, finalURL)
	}

	req.Header.Add("token", "Bearer "+p.authToken)
	req.Header.Add("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Log raw response body for diagnosis
	if p.logger != nil {
		p.logger.Infof("BridgeCard response body: %s", string(responseBody))
	}

	// Check for HTTP errors (non-2xx)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiResp APIResponse
		if err := json.Unmarshal(responseBody, &apiResp); err == nil && apiResp.Error != nil {
			return fmt.Errorf("bridgecard api error [%s]: %s - %s",
				apiResp.Error.Code, apiResp.Error.Message, apiResp.Error.Details)
		}
		return fmt.Errorf("http error %d: %s", resp.StatusCode, string(responseBody))
	}

	// Parse API envelope
	var apiResp APIResponse
	if err := json.Unmarshal(responseBody, &apiResp); err != nil {
		// As a last resort, try unmarshalling the whole response into result (if provided)
		if result != nil {
			if uerr := json.Unmarshal(responseBody, result); uerr == nil {
				if p.logger != nil {
					p.logger.Infof("BridgeCard: unmarshalled full response into result (envelope parse failed)")
				}
				return nil
			}
		}
		return fmt.Errorf("unmarshal response: %w", err)
	}

	// If API indicates failure...
	if !apiResp.Success {
		// If there's an explicit API error object, surface it
		if apiResp.Error != nil {
			return fmt.Errorf("api error [%s]: %s", apiResp.Error.Code, apiResp.Error.Message)
		}

		// If data is present and caller provided a result, try to unmarshal the data.
		if result != nil && len(apiResp.Data) > 0 {
			if err := json.Unmarshal(apiResp.Data, result); err == nil {
				// If result fields are empty (some responses put payload at top-level), fallback:
				// try unmarshalling the whole response body into result.
				// We'll detect emptiness by re-marshalling the result and checking for default values.
				// (Simple heuristic: attempt full-body unmarshal if fields appear zero.)
				// Try a full-body unmarshal as fallback.
				if uerr := json.Unmarshal(responseBody, result); uerr == nil {
					if p.logger != nil {
						p.logger.Infof("bridgecard: success flag false but parsed data (and full-body fallback succeeded); message=%s", apiResp.Message)
					}
					return nil
				}
				// Successfully parsed from apiResp.Data — treat as success
				if p.logger != nil {
					p.logger.Infof("bridgecard: success flag false but parsed data; message=%s", apiResp.Message)
				}
				return nil
			}
			// can't unmarshal data -> proceed to next fallback
		}

		// Try full-body unmarshal into result if provided
		if result != nil {
			if err := json.Unmarshal(responseBody, result); err == nil {
				if p.logger != nil {
					p.logger.Infof("bridgecard: success flag false but entire response unmarshalled into result; message=%s", apiResp.Message)
				}
				return nil
			}
		}

		// No data/unparseable -> return the API message as error
		return fmt.Errorf("api request failed: %s", apiResp.Message)
	}

	// Normal success path: unmarshal data into result if provided
	if result != nil && len(apiResp.Data) > 0 {
		if err := json.Unmarshal(apiResp.Data, result); err != nil {
			// as a fallback try unmarshalling full body into result
			if ferr := json.Unmarshal(responseBody, result); ferr == nil {
				if p.logger != nil {
					p.logger.Infof("bridgecard: unmarshal data failed but full-body unmarshal succeeded")
				}
				return nil
			}
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
func (p *BridgeCardProvider) ParseCardholderVerification(payload []byte) (any, error) {
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

	case "card_credit_event.successful":
		var success CardCreditSuccess
		if err := json.Unmarshal(event.Data, &success); err != nil {
			return nil, fmt.Errorf("parse success data: %w", err)
		}
		return &success, nil

	case "card_credit_event.failed":
		var failed CardCreditFailed
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
