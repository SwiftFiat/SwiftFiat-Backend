package rapidramp

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)



type QRCode struct {
	ID                  uuid.UUID        `json:"id"`
	Token               uuid.UUID        `json:"token"`
	UserID              int64            `json:"user_id"`
	QRType              string           `json:"qr_type"`
	CurrencyPreference  string           `json:"currency_preference"`
	ConversionMode      string           `json:"conversion_mode"`
	Network             string           `json:"network"`
	CryptoCurrency      string           `json:"crypto_currency"`
	CryptomusAddressID  *uuid.UUID       `json:"cryptomus_address_id,omitempty"`
	LinkedWalletID      *uuid.UUID       `json:"linked_wallet_id,omitempty"`
	LinkedBankAccountID *uuid.UUID       `json:"linked_bank_account_id,omitempty"`
	FixedAmount         *decimal.Decimal `json:"fixed_amount,omitempty"`
	MinAmount           *decimal.Decimal `json:"min_amount,omitempty"`
	MaxAmount           *decimal.Decimal `json:"max_amount,omitempty"`
	QRCodeData          string           `json:"qr_code_data"`
	QRCodeImageURL      *string          `json:"qr_code_image_url,omitempty"`
	Description         *string          `json:"description,omitempty"`
	Label               *string          `json:"label,omitempty"`
	Status              string           `json:"status"`
	UsageLimit          *int             `json:"usage_limit,omitempty"`
	UsageCount          int              `json:"usage_count"`
	ExpiresAt           *time.Time       `json:"expires_at,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	LastUsedAt          *time.Time       `json:"last_used_at,omitempty"`
	DeletedAt           *time.Time       `json:"deleted_at,omitempty"`
}

type CreateQRCodeRequest struct {
	Network            string           `json:"network" binding:"required"`
	CryptoCurrency     string           `json:"crypto_currency" binding:"required"`
	// CurrencyPreference string           `json:"currency_preference" binding:"required,oneof=USD NGN"`
	// ConversionMode     string           `json:"conversion_mode" binding:"required,oneof=auto manual"`
	BankAccountID      *uuid.UUID       `json:"bank_account_id" binding:"required"`  // Required if auto mode
	// LinkedWalletID     *uuid.UUID       `json:"linked_wallet_id"` // Required if manual mode
	Label              *string          `json:"label"`
	Description        *string          `json:"description"`
	Amount             string           `json:"amount" binding:"required"`
	UsageLimit         *int             `json:"usage_limit"`
	ExpiresAt          *time.Time       `json:"expires_at"`
}

type QRCodeResponse struct {
	ID                 uuid.UUID        `json:"id"`
	Token              uuid.UUID        `json:"token"`
	QRCodeData         string           `json:"qr_code_data"`
	QRCodeImageURL     *string          `json:"qr_code_image_url,omitempty"`
	Amount             string           `json:"amount"`
	CryptoAddress      string           `json:"crypto_address"`
	Network            string           `json:"network"`
	CryptoCurrency     string           `json:"crypto_currency"`
	CurrencyPreference string           `json:"currency_preference"`
	ConversionMode     string           `json:"conversion_mode"`
	Status             string           `json:"status"`
	UsageCount         int              `json:"usage_count"`
	UsageLimit         *int             `json:"usage_limit,omitempty"`
	Label              *string          `json:"label,omitempty"`
	BankAccount        *BankAccountInfo `json:"bank_account,omitempty"`
	ExpiresAt          *time.Time       `json:"expires_at,omitempty"`
	LastUsedAt         *time.Time       `json:"last_used_at,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
}

type BankAccountInfo struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankName      string `json:"bank_name"`
}

type QRTransaction struct {
	ID                     uuid.UUID        `json:"id"`
	QRCodeID               uuid.UUID        `json:"qr_code_id"`
	TransactionID          *uuid.UUID       `json:"transaction_id,omitempty"`
	UserID                 int64            `json:"user_id"`
	CryptomusTransactionID *string          `json:"cryptomus_transaction_id,omitempty"`
	CryptomusOrderID       *string          `json:"cryptomus_order_id,omitempty"`
	CryptomusUUID          *string          `json:"cryptomus_uuid,omitempty"`
	CryptomusAddressID     *uuid.UUID       `json:"cryptomus_address_id,omitempty"`
	WebhookData            json.RawMessage  `json:"webhook_data,omitempty"`
	SenderAddress          *string          `json:"sender_address,omitempty"`
	SenderName             *string          `json:"sender_name,omitempty"`
	CryptoCurrency         string           `json:"crypto_currency"`
	CryptoNetwork          string           `json:"crypto_network"`
	CryptoAmount           decimal.Decimal  `json:"crypto_amount"`
	CryptoAmountUSD        *decimal.Decimal `json:"crypto_amount_usd,omitempty"`
	TransactionHash        *string          `json:"transaction_hash,omitempty"`
	ConfirmationBlocks     int              `json:"confirmation_blocks"`
	RequiredConfirmations  int              `json:"required_confirmations"`
	ConversionRate         *decimal.Decimal `json:"conversion_rate,omitempty"`
	FiatCurrency           *string          `json:"fiat_currency,omitempty"`
	FiatAmount             *decimal.Decimal `json:"fiat_amount,omitempty"`
	ConversionFees         decimal.Decimal  `json:"conversion_fees"`
	PlatformFees           decimal.Decimal  `json:"platform_fees"`
	NetworkFees            decimal.Decimal  `json:"network_fees"`
	TotalFees              decimal.Decimal  `json:"total_fees"`
	NetAmount              *decimal.Decimal `json:"net_amount,omitempty"`
	BankAccountID          *uuid.UUID       `json:"bank_account_id,omitempty"`
	BankAccountName        *string          `json:"bank_account_name,omitempty"`
	BankAccountNumber      *string          `json:"bank_account_number,omitempty"`
	BankCode               *string          `json:"bank_code,omitempty"`
	PayoutReference        *string          `json:"payout_reference,omitempty"`
	PayoutProvider         *string          `json:"payout_provider,omitempty"`
	PayoutProviderResponse json.RawMessage  `json:"payout_provider_response,omitempty"`
	Status                 string           `json:"status"`
	PaymentReceivedAt      *time.Time       `json:"payment_received_at,omitempty"`
	PaymentConfirmedAt     *time.Time       `json:"payment_confirmed_at,omitempty"`
	ConversionStartedAt    *time.Time       `json:"conversion_started_at,omitempty"`
	ConversionCompletedAt  *time.Time       `json:"conversion_completed_at,omitempty"`
	PayoutInitiatedAt      *time.Time       `json:"payout_initiated_at,omitempty"`
	PayoutCompletedAt      *time.Time       `json:"payout_completed_at,omitempty"`
	FailureReason          *string          `json:"failure_reason,omitempty"`
	FailureStage           *string          `json:"failure_stage,omitempty"`
	RetryCount             int              `json:"retry_count"`
	LastRetryAt            *time.Time       `json:"last_retry_at,omitempty"`
	MaxRetries             int              `json:"max_retries"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
}

type QRTransactionResponse struct {
	ID          uuid.UUID           `json:"id"`
	QRCodeLabel *string             `json:"qr_code_label,omitempty"`
	Status      string              `json:"status"`
	Crypto      CryptoDetails       `json:"crypto"`
	Conversion  *ConversionDetails  `json:"conversion,omitempty"`
	Payout      *PayoutDetails      `json:"payout,omitempty"`
	Timeline    TransactionTimeline `json:"timeline"`
	CreatedAt   time.Time           `json:"created_at"`
}

type CryptoDetails struct {
	Currency              string           `json:"currency"`
	Network               string           `json:"network"`
	Amount                string  `json:"amount"`
	AmountUSD             *string `json:"amount_usd,omitempty"`
	TransactionHash       *string          `json:"transaction_hash,omitempty"`
}

type ConversionDetails struct {
	Rate         string `json:"rate"`
	FiatCurrency   string          `json:"fiat_currency"`
	FiatAmount     string `json:"fiat_amount"`
	ConversionFees string `json:"conversion_fees"`
	PlatformFees   string `json:"platform_fees"`
	NetworkFees    string `json:"network_fees"`
	TotalFees      string `json:"total_fees"`
	NetAmount      string `json:"net_amount"`
}

type PayoutDetails struct {
	BankAccountNumber string  `json:"bank_account_number"`
	BankAccountName   string  `json:"bank_account_name"`
	BankName          *string  `json:"bank_name"`
	Reference         *string `json:"reference,omitempty"`
	Provider          string  `json:"provider"`
}

type TransactionTimeline struct {
	CreatedAt             time.Time  `json:"created_at"`
	PaymentReceivedAt     *time.Time `json:"payment_received_at,omitempty"`
	PaymentConfirmedAt    *time.Time `json:"payment_confirmed_at,omitempty"`
	ConversionStartedAt   *time.Time `json:"conversion_started_at,omitempty"`
	ConversionCompletedAt *time.Time `json:"conversion_completed_at,omitempty"`
	PayoutInitiatedAt     *time.Time `json:"payout_initiated_at,omitempty"`
	PayoutCompletedAt     *time.Time `json:"payout_completed_at,omitempty"`
}

type QRTransactionStats struct {
	TotalTransactions     int             `json:"total_transactions"`
	CompletedTransactions int             `json:"completed_transactions"`
	FailedTransactions    int             `json:"failed_transactions"`
	TotalCryptoReceived   decimal.Decimal `json:"total_crypto_received"`
	TotalNetPayout        decimal.Decimal `json:"total_net_payout"`
	SendingToBankTransactions int `json:"sending_to_bank_transactions"`
	ConvertingTransactions int `json:"converting_transactions"`
	ReceivedTransactions int `json:"received_transactions"`
	PendingTransactions int `json:"pending_transactions"`
}

// ============================================================
// WEBHOOK MODELS
// ============================================================

type CryptomusWebhookPayload struct {
	Type              string                 `json:"type"`
	UUID              string                 `json:"uuid"`
	OrderID           string                 `json:"order_id"`
	Amount            string                 `json:"amount"`
	PaymentAmount     string                 `json:"payment_amount"`
	PaymentAmountUSD  string                 `json:"payment_amount_usd"`
	MerchantAmount    string                 `json:"merchant_amount"`
	Commission        string                 `json:"commission"`
	IsFinal           bool                   `json:"is_final"`
	Status            string                 `json:"status"`
	From              string                 `json:"from"`
	WalletAddressUUID *string                `json:"wallet_address_uuid"`
	Network           string                 `json:"network"`
	Currency          string                 `json:"currency"`
	PayerCurrency     string                 `json:"payer_currency"`
	AdditionalData    *string                `json:"additional_data"`
	Convert           map[string]interface{} `json:"convert"`
	TxID              string                 `json:"txid"`
	Sign              string                 `json:"sign"`
}

// ============================================================
// ERROR TYPES
// ============================================================

type QRCodeError struct {
	Code    string
	Message string
	Err     error
}

func (e *QRCodeError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

var (
	ErrQRCodeNotFound          = &QRCodeError{Code: "QR_CODE_NOT_FOUND", Message: "QR code not found"}
	ErrQRCodeExpired           = &QRCodeError{Code: "QR_CODE_EXPIRED", Message: "QR code has expired"}
	ErrQRCodeInactive          = &QRCodeError{Code: "QR_CODE_INACTIVE", Message: "QR code is not active"}
	ErrQRCodeUsageLimitReached = &QRCodeError{Code: "USAGE_LIMIT_REACHED", Message: "QR code usage limit reached"}
	ErrBankAccountNotFound     = &QRCodeError{Code: "BANK_ACCOUNT_NOT_FOUND", Message: "Bank account not found"}
	ErrBankAccountNotVerified  = &QRCodeError{Code: "BANK_ACCOUNT_NOT_VERIFIED", Message: "Bank account not verified"}
	ErrTransactionNotFound     = &QRCodeError{Code: "TRANSACTION_NOT_FOUND", Message: "Transaction not found"}
	ErrDuplicateWebhook        = &QRCodeError{Code: "DUPLICATE_WEBHOOK", Message: "Webhook already processed"}
	ErrPayoutFailed            = &QRCodeError{Code: "PAYOUT_FAILED", Message: "Bank payout failed"}
)
