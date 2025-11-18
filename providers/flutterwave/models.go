package flutterwave

import (
	"encoding/json"
	"time"
)

const (
	FlutterwaveBaseURL    = "https://api.flutterwave.com/v4"
	FlutterwaveSandboxURL = "https://api-sandbox.flutterwave.com/v4"
)

// ============================================================================
// REQUEST/RESPONSE MODELS
// ============================================================================

type APIResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// ============================================================================
// VIRTUAL CARD MODELS
// ============================================================================

type CreateCardRequest struct {
	Currency          string  `json:"currency"`
	Amount            float64 `json:"amount"`
	DebitCurrency     string  `json:"debit_currency,omitempty"`
	BillingName       string  `json:"billing_name"`
	BillingAddress    string  `json:"billing_address,omitempty"`
	BillingCity       string  `json:"billing_city,omitempty"`
	BillingState      string  `json:"billing_state,omitempty"`
	BillingPostalCode string  `json:"billing_postal_code,omitempty"`
	BillingCountry    string  `json:"billing_country,omitempty"`
	CallbackURL       string  `json:"callback_url,omitempty"`
	FirstName         string  `json:"first_name,omitempty"`
	LastName          string  `json:"last_name,omitempty"`
	Email             string  `json:"email,omitempty"`
	Phone             string  `json:"phone,omitempty"`
	Title             string  `json:"title,omitempty"`         // Mr, Mrs, Miss
	Gender            string  `json:"gender,omitempty"`        // M, F
	DateOfBirth       string  `json:"date_of_birth,omitempty"` // YYYY-MM-DD
}

type VirtualCard struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"account_id"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	CardPan      string    `json:"card_pan"`
	MaskedPan    string    `json:"masked_pan"`
	City         string    `json:"city"`
	State        string    `json:"state"`
	AddressLine1 string    `json:"address_1"`
	AddressLine2 string    `json:"address_2"`
	ZipCode      string    `json:"zip_code"`
	CVV          string    `json:"cvv"`
	Expiration   string    `json:"expiration"`
	SendTo       string    `json:"send_to,omitempty"`
	BinCheckName string    `json:"bin_check_name"`
	CardType     string    `json:"card_type"`
	NameOnCard   string    `json:"name_on_card"`
	CreatedAt    time.Time `json:"created_at"`
	IsActive     bool      `json:"is_active"`
	CallbackURL  string    `json:"callback_url,omitempty"`
}

type FundCardRequest struct {
	DebitCurrency string  `json:"debit_currency"`
	Amount        float64 `json:"amount"`
}

type WithdrawCardRequest struct {
	Amount float64 `json:"amount"`
}

type CardTransaction struct {
	ID              string    `json:"id"`
	CardID          string    `json:"card_id"`
	TransactionRef  string    `json:"transaction_ref"`
	Amount          float64   `json:"amount"`
	Currency        string    `json:"currency"`
	Product         string    `json:"product"`
	Note            string    `json:"note"`
	Status          string    `json:"status"`
	Type            string    `json:"type"` // debit, credit
	MerchantName    string    `json:"merchant_name,omitempty"`
	MerchantCity    string    `json:"merchant_city,omitempty"`
	MerchantCountry string    `json:"merchant_country,omitempty"`
	BalanceBefore   float64   `json:"balance_before"`
	BalanceAfter    float64   `json:"balance_after"`
	TransactionDate time.Time `json:"transaction_date"`
	CreatedAt       time.Time `json:"created_at"`
}

type ListCardsResponse struct {
	Cards []VirtualCard `json:"cards"`
	Meta  struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
	} `json:"meta"`
}

type ListTransactionsResponse struct {
	Transactions []CardTransaction `json:"transactions"`
	Meta         struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
	} `json:"meta"`
}

// WebhookPayload represents the structure of Flutterwave webhooks
type WebhookPayload struct {
	Event     string                 `json:"event"`
	Data      map[string]interface{} `json:"data"`
	EventType string                 `json:"event.type"`
}

type CardDesignRequest struct {
	CardID          string `json:"card_id"`
	BackgroundColor string `json:"background_color,omitempty"` // hex color
	TextColor       string `json:"text_color,omitempty"`       // hex color
	LogoURL         string `json:"logo_url,omitempty"`
}

type CardLimitsRequest struct {
	DailySpendLimit           float64  `json:"daily_spend_limit,omitempty"`
	MonthlySpendLimit         float64  `json:"monthly_spend_limit,omitempty"`
	SingleTransactionLimit    float64  `json:"single_transaction_limit,omitempty"`
	AllowedMerchantCategories []string `json:"allowed_merchant_categories,omitempty"` // MCC codes
	BlockedMerchantCategories []string `json:"blocked_merchant_categories,omitempty"`
	AllowedCountries          []string `json:"allowed_countries,omitempty"` // ISO country codes
	BlockedCountries          []string `json:"blocked_countries,omitempty"`
}

type CardAnalytics struct {
	CardID             string  `json:"card_id"`
	TotalTransactions  int     `json:"total_transactions"`
	TotalSpent         float64 `json:"total_spent"`
	AverageTransaction float64 `json:"average_transaction"`
	TopMerchants       []struct {
		Name   string  `json:"name"`
		Amount float64 `json:"amount"`
		Count  int     `json:"count"`
	} `json:"top_merchants"`
	SpendByCategory       map[string]float64 `json:"spend_by_category"`
	TransactionsByCountry map[string]int     `json:"transactions_by_country"`
	Period                string             `json:"period"` // e.g., "last_30_days"
}


