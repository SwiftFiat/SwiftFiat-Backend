package bridgecards

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// REQUEST/RESPONSE TYPES
// ============================================================================
type CreateCardHolderRequest struct {
	FirstName string         `json:"first_name" binding:"required"`
	LastName  string         `json:"last_name" binding:"required"`
	Email     string         `json:"email_address" binding:"required"`
	Phone     string         `json:"phone" binding:"required"`
	Address   Address        `json:"address" binding:"required"`
	Identity  Identity       `json:"identity" binding:"required"`
	Metadata  map[string]any `json:"metadata"`
}

type CreateCardHolderResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardHolderID string `json:"cardholder_id"`
	} `json:"data"`
}

type Identity struct {
	IDType      string `json:"id_type" binding:"required"`
	IDNumber    string `json:"id_number" binding:"omitempty"`
	IDImage     string `json:"id_image" binding:"omitempty"`
	SelfieImage string `json:"selfie_image" binding:"omitempty"`
	BVN         string `json:"bvn" binding:"required"`
}

type CreateCardRequest struct {
	CardHolderID         string         `json:"cardholder_id"`                // Your internal user ID
	CardType             string         `json:"card_type" binding:"required"` // virtual
	CardLimit            string         `json:"card_limit"`
	Brand                string         `json:"card_brand"`     // "visa" or "mastercard"
	Currency             string         `json:"card_currency"`  // "USD"
	FundingAmount        string         `json:"funding_amount"` // Initial funding amount
	TransactionReference string         `json:"transaction_reference"`
	Pin                  string         `json:"pin"`
	MetaData             map[string]any `json:"metadata"`
	UserID               int64
	CardPlanID           int64
	CardName             string
	CardColor            string
	SourceWalletID       uuid.UUID
}

type CreateCardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID   string `json:"card_id"`
		Currency string `json:"currency"`
	} `json:"data"`
}

type Card struct {
	ID             string    `json:"id"`          // BridgeCard's card ID
	CustomerID     string    `json:"customer_id"` // Your internal user ID
	CardName       string    `json:"card_name"`
	Brand          string    `json:"brand"` // "visa" or "mastercard"
	Type           string    `json:"type"`  // "virtual"
	Currency       string    `json:"currency"`
	Balance        int64     `json:"balance"`    // Current balance in cents
	Status         string    `json:"status"`     // "active", "frozen", "terminated"
	MaskedPan      string    `json:"masked_pan"` // Masked card number
	ExpiryMonth    string    `json:"expiry_month"`
	ExpiryYear     string    `json:"expiry_year"`
	BillingAddress Address   `json:"billing_address"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CardHolder represents a cardholder object returned by BridgeCard
type CardHolder struct {
	ID        string         `json:"id,omitempty"`
	FirstName string         `json:"first_name,omitempty"`
	LastName  string         `json:"last_name,omitempty"`
	Email     string         `json:"email_address,omitempty"`
	Phone     string         `json:"phone,omitempty"`
	Address   Address        `json:"address,omitempty"`
	Identity  Identity       `json:"identity,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Address struct {
	Address     string `json:"address"`
	City        string `json:"city"`
	State       string `json:"state"`
	PostalCode  string `json:"postal_code"`
	Country     string `json:"country"`
	HouseNumber string `json:"house_no"`
}

type SecureCardDetails struct {
	Pan         string `json:"pan"` // Full card number (PCI compliant, handle carefully)
	CVV         string `json:"cvv"` // Card CVV
	ExpiryMonth string `json:"expiry_month"`
	ExpiryYear  string `json:"expiry_year"`
	Pin         string `json:"pin"` // Card PIN if applicable
}

type FundCardRequest struct {
	CardID               string `json:"card_id" binding:"required"`
	Amount               string `json:"amount" binding:"required"` // Amount in cents
	TransactionReference string `json:"transaction_reference" binding:"omitempty"`
	Currency             string `json:"currency" binding:"required"`
}

type FundCardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID               string `json:"card_id"`
		TransactionReference string `json:"transaction_reference"`
	} `json:"data"`
}

type WithdrawCardRequest struct {
	Amount int64 `json:"amount"` // Amount in cents
}

type Transaction struct {
	ID                   string    `json:"id"`
	CardID               string    `json:"card_id"`
	Type                 string    `json:"type"` // "debit", "credit", "reversal"
	Amount               int64     `json:"amount"`
	Fee                  int64     `json:"fee"`
	Currency             string    `json:"currency"`
	Status               string    `json:"status"` // "pending", "approved", "declined"
	DeclineReason        string    `json:"decline_reason"`
	MerchantName         string    `json:"merchant_name"`
	MerchantCategory     string    `json:"merchant_category"`
	MerchantCategoryCode string    `json:"merchant_category_code"`
	MerchantCountry      string    `json:"merchant_country"`
	MerchantCity         string    `json:"merchant_city"`
	BillingAmount        int64     `json:"billing_amount"` // Original amount in merchant currency
	BillingCurrency      string    `json:"billing_currency"`
	BalanceBefore        int64     `json:"balance_before"`
	BalanceAfter         int64     `json:"balance_after"`
	TransactionDate      time.Time `json:"transaction_date"`
	CreatedAt            time.Time `json:"created_at"`
}

type ListTransactionsParams struct {
	StartDate string `json:"start_date"` // YYYY-MM-DD
	EndDate   string `json:"end_date"`   // YYYY-MM-DD
	Limit     int    `json:"limit"`
	Page      int    `json:"page"`
}

type APIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Error   *APIError       `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// WebhookEvent represents the structure of webhook events from BridgeCard
type WebhookEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// CardholderVerificationSuccess represents successful cardholder verification
type CardholderVerificationSuccess struct {
	CardholderID string `json:"cardholder_id"`
	IsActive     bool   `json:"is_active"`
	Livemode     bool   `json:"livemode"`
	IssuingAppID string `json:"issuing_app_id"`
}

// CardholderVerificationFailed represents failed cardholder verification
type CardholderVerificationFailed struct {
	CardholderID     string `json:"cardholder_id"`
	IsActive         bool   `json:"is_active"`
	Livemode         bool   `json:"livemode"`
	IssuingAppID     string `json:"issuing_app_id"`
	ErrorDescription string `json:"error_description"`
}

type CardCreditSuccess struct {
	CardID                  string    `json:"card_id"`
	CardholderID            string    `json:"cardholder_id"`
	Amount                  string     `json:"amount"`
	Currency                string    `json:"currency"`
	TransactionReference    string    `json:"transaction_reference"`
	Livemode                bool      `json:"livemode"`
	IssuingAppID            string    `json:"issuing_app_id"`
	CardTransactionType     string    `json:"card_transaction_type"`
	TransactionDate         time.Time `json:"transaction_date"`
	TransactionTimestamp    int64     `json:"transaction_timestamp"`
	SettledAvailableBalance int64     `json:"settled_available_balance"`
	SettledBookBalance      int64     `json:"settled_book_balance"`
}

type CardCreditFailed struct {
	CardID                  string    `json:"card_id"`
	CardholderID            string    `json:"cardholder_id"`
	Amount                  string     `json:"amount"`
	Currency                string    `json:"currency"`
	TransactionReference    string    `json:"transaction_reference"`
	Livemode                bool      `json:"livemode"`
	IssuingAppID            string    `json:"issuing_app_id"`
	CardTransactionType     string    `json:"card_transaction_type"`
	TransactionDate         time.Time `json:"transaction_date"`
	TransactionTimestamp    int64     `json:"transaction_timestamp"`
}

type FundIssuingWalletRequest struct {
	Amount int64 `json:"amount"`
}

type GetCardBalanceResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID                  string `json:"card_id"`
		Balance                 string  `json:"balance"`
		SettledAvailableBalance string  `json:"available_balance"`
		SettledBookBalance      string  `json:"book_balance"`
	} `json:"data"`
}

type FreezeCardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID string `json:"card_id"`
	} `json:"data"`
}
	