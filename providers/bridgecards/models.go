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
	Metadata  map[string]any `json:"meta_data"`
}

type CreateCardHolderResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardHolderID string `json:"cardholder_id"`
	} `json:"data"`
}

type Identity struct {
	IDType   string `json:"id_type" binding:"required"`
	IDNumber string `json:"id_number" binding:"omitempty"`
	// IDImage     *string `json:"id_image" binding:"omitempty"`
	SelfieImage string `json:"selfie_image" binding:"omitempty"`
	BVN         string `json:"bvn" binding:"required"`
}

type CreateCardRequest struct {
	CardHolderID         string         `json:"cardholder_id" binding:"required"` // Your internal user ID
	CardType             string         `json:"card_type" binding:"required"`     // virtual
	CardLimit            string         `json:"card_limit"`
	Brand                string         `json:"card_brand"`     // "visa" or "mastercard"
	Currency             string         `json:"card_currency"`  // "USD"
	FundingAmount        string         `json:"funding_amount"` // Initial funding amount
	TransactionReference string         `json:"transaction_reference"`
	Pin                  string         `json:"pin"`
	MetaData             map[string]any `json:"metadata"`
	UserID               uuid.UUID          `json:"-"`
	CardPlanID           int64          `json:"-"`
	CardName             string         `json:"-"`
	CardColor            string         `json:"-"`
	SourceWalletID       uuid.UUID      `json:"-"`
	IdempotencyKey       string         `json:"idempotency_key" binding:"required"`
	IdempotencyKey2      string         `json:"idempotency_key_2" binding:"required"`
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
	CardID               string `json:"card_id"`               // Amount in cents
	Amount               string `json:"amount"`                // Amount in cents
	TransactionReference string `json:"transaction_reference"` // Amount in cents
	Currency             string `json:"currency"`              // Amount in cents
}

type WithdrawCardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID               string `json:"card_id"`
		TransactionReference string `json:"transaction_reference"`
	} `json:"data"`
}

type ListCardTransactionsRequest struct {
	CardID    string    `json:"card_id" binding:"required"`
	Page      int       `json:"page"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}

type ListCardTransactionsResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Transactions []struct {
			Amount                         string `json:"amount"`
			BridgecardTransactionReference string `json:"bridgecard_transaction_reference"`
			CardID                         string `json:"card_id"`
			CardTransactionType            string `json:"card_transaction_type"`
			CardholderID                   string `json:"cardholder_id"`
			ClientTransactionReference     string `json:"client_transaction_reference"`
			Currency                       string `json:"currency"`
			Description                    string `json:"description"`
			IssuingAppID                   string `json:"issuing_app_id"`
			Livemode                       bool   `json:"livemode"`
			TransactionDate                string `json:"transaction_date"`
			TransactionTimestamp           int64  `json:"transaction_timestamp"`
			EnrichedData                   struct {
				IsRecurring         bool   `json:"is_recurring"`
				MerchantCity        string `json:"merchant_city"`
				MerchantCode        string `json:"merchant_code"`
				MerchantLogo        string `json:"merchant_logo"`
				MerchantName        string `json:"merchant_name"`
				MerchantWebsite     string `json:"merchant_website"`
				TransactionCategory string `json:"transaction_category"`
				TransactionGroup    string `json:"transaction_group"`
			} `json:"enriched_data"`
			PartnerInterchangeFee       string `json:"partner_interchange_fee"`
			InterchangeRevenue          string `json:"interchange_revenue"`
			PartnerInterchangeFeeRefund string `json:"partner_interchange_fee_refund"`
			InterchangeRevenueRefund    string `json:"interchange_revenue_refund"`
		} `json:"transactions"`
		Meta struct {
			Total    int    `json:"total"`
			Pages    int    `json:"pages"`
			Previous string `json:"previous"`
			Next     string `json:"next"`
		} `json:"meta"`
	} `json:"data"`
}

type GetCardTransactionResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Amount                         string `json:"amount"`
		BridgecardTransactionReference string `json:"bridgecard_transaction_reference"`
		CardID                         string `json:"card_id"`
		CardTransactionType            string `json:"card_transaction_type"`
		CardholderID                   string `json:"cardholder_id"`
		ClientTransactionReference     string `json:"client_transaction_reference"`
		Currency                       string `json:"currency"`
		Description                    string `json:"description"`
		IssuingAppID                   string `json:"issuing_app_id"`
		Livemode                       bool   `json:"livemode"`
		TransactionDate                string `json:"transaction_date"`
		TransactionTimestamp           int64  `json:"transaction_timestamp"`
		MerchantCategoryCode           string `json:"merchant_category_code"`
	} `json:"data"`
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
	Amount                  string    `json:"amount"`
	Currency                string    `json:"currency"`
	TransactionReference    string    `json:"transaction_reference"`
	Livemode                bool      `json:"livemode"`
	IssuingAppID            string    `json:"issuing_app_id"`
	CardTransactionType     string    `json:"card_transaction_type"`
	TransactionDate         time.Time `json:"transaction_date"`
	TransactionTimestamp    time.Time `json:"transaction_timestamp"`
	SettledAvailableBalance string    `json:"settled_available_balance"`
	SettledBookBalance      string    `json:"settled_book_balance"`
}

type CardCreditFailed struct {
	CardID               string    `json:"card_id"`
	CardholderID         string    `json:"cardholder_id"`
	Amount               string    `json:"amount"`
	Currency             string    `json:"currency"`
	TransactionReference string    `json:"transaction_reference"`
	Livemode             bool      `json:"livemode"`
	IssuingAppID         string    `json:"issuing_app_id"`
	CardTransactionType  string    `json:"card_transaction_type"`
	TransactionDate      time.Time `json:"transaction_date"`
	TransactionTimestamp time.Time `json:"transaction_timestamp"`
}

type FundIssuingWalletRequest struct {
	Amount int64 `json:"amount"`
}

type GetCardBalanceResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID                  string `json:"card_id"`
		Balance                 string `json:"balance"`
		SettledAvailableBalance string `json:"available_balance"`
		SettledBookBalance      string `json:"book_balance"`
	} `json:"data"`
}

type FreezeCardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID string `json:"card_id"`
	} `json:"data"`
}

type UpdateCardPinRequest struct {
	CardID  string `json:"card_id"`
	CardPin string `json:"card_pin"`
}

type CardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type ListCardsResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Cards []struct {
			BillingAddress struct {
				BillingAddress1 string `json:"billing_address1"`
				BillingCity     string `json:"billing_city"`
				BillingCountry  string `json:"billing_country"`
				BillingZipCode  string `json:"billing_zip_code"`
				CountryCode     string `json:"country_code"`
			} `json:"billing_address"`
			Brand        string `json:"brand"`
			CardCurrency string `json:"card_currency"`
			CardID       string `json:"card_id"`
			CardName     string `json:"card_name"`
			CardNumber   string `json:"card_number"`
			CardType     string `json:"card_type"`
			CardholderID string `json:"cardholder_id"`
			CreatedAt    int64  `json:"created_at"`
			CVV          string `json:"cvv"`
			ExpiryMonth  string `json:"expiry_month"`
			ExpiryYear   string `json:"expiry_year"`
			IsActive     bool   `json:"is_active"`
			IssuingAppID string `json:"issuing_app_id"`
			Last4        string `json:"last_4"`
			Livemode     bool   `json:"livemode"`
		} `json:"cards"`
		Total int `json:"total"`
	} `json:"data"`
}

type CardDebitEventSuccessful struct {
	Event string `json:"event"`
	Data  struct {
		CardID                  string `json:"card_id"`
		CardholderID            string `json:"cardholder_id"`
		Amount                  string `json:"amount"`
		Currency                string `json:"currency"`
		Description             string `json:"description"`
		TransactionReference    string `json:"transaction_reference"`
		Livemode                bool   `json:"livemode"`
		IssuingAppID            string `json:"issuing_app_id"`
		CardTransactionType     string `json:"card_transaction_type"`
		MerchantCategoryCode    string `json:"merchant_category_code"`
		TransactionDate         string `json:"transaction_date"`
		TransactionTimestamp    string `json:"transaction_timestamp"`
		SettledAvailableBalance string `json:"settled_available_balance"`
		SettledBookBalance      string `json:"settled_book_balance"`
	} `json:"data"`
}

type CardDebitEventDeclined struct {
	Event string `json:"event"`
	Data  struct {
		CardID                  string `json:"card_id"`
		CardholderID            string `json:"cardholder_id"`
		Amount                  string `json:"amount"`
		Currency                string `json:"currency"`
		Description             string `json:"description"`
		TransactionReference    string `json:"transaction_reference"`
		Livemode                bool   `json:"livemode"`
		IssuingAppID            string `json:"issuing_app_id"`
		CardTransactionType     string `json:"card_transaction_type"`
		MerchantCategoryCode    string `json:"merchant_category_code"`
		TransactionDate         string `json:"transaction_date"`
		TransactionTimestamp    string `json:"transaction_timestamp"`
		SettledAvailableBalance string `json:"settled_available_balance"`
		SettledBookBalance      string `json:"settled_book_balance"`
	} `json:"data"`
}

type GetCardDetailsResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		BillingAddress struct {
			BillingAddress1 string `json:"billing_address1"`
			BillingCity     string `json:"billing_city"`
			BillingCountry  string `json:"billing_country"`
			BillingZipCode  string `json:"billing_zip_code"`
			CountryCode     string `json:"country_code"`
			State           string `json:"state"`
			StateCode       string `json:"state_code"`
		} `json:"billing_address"`
		Brand        string `json:"brand"`
		CardCurrency string `json:"card_currency"`
		CardID       string `json:"card_id"`
		CardName     string `json:"card_name"`
		CardNumber   string `json:"card_number"`
		CardType     string `json:"card_type"`
		CardholderID string `json:"cardholder_id"`
		CreatedAt    int64  `json:"created_at"`
		CVV          string `json:"cvv"`
		ExpiryMonth  string `json:"expiry_month"`
		ExpiryYear   string `json:"expiry_year"`
		IsActive     bool   `json:"is_active"`
		IsDeleted    bool   `json:"is_deleted"`
		IssuingAppID string `json:"issuing_app_id"`
		Last4        string `json:"last_4"`
		Livemode     bool   `json:"livemode"`
		MetaData     struct {
			UserID string `json:"user_id"`
		} `json:"meta_data"`
		Balance           string `json:"balance"`
		AvailableBalance  string `json:"available_balance"`
		BookBalance       string `json:"book_balance"`
		BlockedDueToFraud bool   `json:"blocked_due_to_fraud"`
		Pin3DSActivated   bool   `json:"pin_3ds_activated"`
	} `json:"data"`
}

type DebitCardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CardID               string `json:"card_id"`
		TransactionReference string `json:"transaction_reference"`
	} `json:"data"`
}

type DebitCardRequest struct {
	CardID string `json:"card_id"`
}

type GetCardTransactionStatusResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		TransactionStatus string `json:"transaction_status"`
	} `json:"data"`
}

type CardWithDrawEventSuccessful struct {
	Event string `json:"event"`
	Data  struct {
		CardID               string    `json:"card_id"`
		CardholderID         string    `json:"cardholder_id"`
		Amount               string    `json:"amount"`
		Description          string    `json:"description"`
		Currency             string    `json:"currency"`
		TransactionReference string    `json:"transaction_reference"`
		Livemode             bool      `json:"livemode"`
		IssuingAppID         string    `json:"issuing_app_id"`
		CardTransactionType  string    `json:"card_transaction_type"`
		TransactionDate      string    `json:"transaction_date"`
		TransactionTimestamp time.Time `json:"transaction_timestamp"`
	} `json:"data"`
}

type CardWithDrawEventFailed struct {
	Event string `json:"event"`
	Data  struct {
		CardID               string    `json:"card_id"`
		CardholderID         string    `json:"cardholder_id"`
		Amount               string    `json:"amount"`
		Description          string    `json:"description"`
		Currency             string    `json:"currency"`
		TransactionReference string    `json:"transaction_reference"`
		Livemode             bool      `json:"livemode"`
		IssuingAppID         string    `json:"issuing_app_id"`
		CardTransactionType  string    `json:"card_transaction_type"`
		TransactionDate      string    `json:"transaction_date"`
		TransactionTimestamp time.Time `json:"transaction_timestamp"`
	} `json:"data"`
}

type CardCreationEventSuccessful struct {
	Event string `json:"event"`
	Data  struct {
		CardID       string            `json:"card_id"`
		CardholderID string            `json:"cardholder_id"`
		Currency     string            `json:"currency"`
		IssuingAppID string            `json:"issuing_app_id"`
		Livemode     bool              `json:"livemode"`
		MetaData     map[string]string `json:"meta_data"`
	} `json:"data"`
}

type CardCreationEventFailed struct {
	Event string `json:"event"`
	Data  struct {
		CardID       string `json:"card_id"`
		CardholderID string `json:"cardholder_id"`
		Currency     string `json:"currency"`
		IssuingAppID string `json:"issuing_app_id"`
		Livemode     bool   `json:"livemode"`
		Reason       string `json:"reason"`
	} `json:"data"`
}

type IssuingWalletBalanceResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		IssuingBalanceUSD string `json:"issuing_balance_USD"`
	} `json:"data"`
}

type GetAllIssuedcardResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Cards []struct {
			BillingAddress struct {
				BillingAddress1 string `json:"billing_address1"`
				BillingCity     string `json:"billing_city"`
				BillingCountry  string `json:"billing_country"`
				BillingZipCode  string `json:"billing_zip_code"`
				CountryCode     string `json:"country_code"`
				State           string `json:"state"`
				StateCode       string `json:"state_code"`
			} `json:"billing_address"`
			Brand        string `json:"brand"`
			CardCurrency string `json:"card_currency"`
			CardID       string `json:"card_id"`
			CardName     string `json:"card_name"`
			CardNumber   string `json:"card_number"`
			CardType     string `json:"card_type"`
			CardholderID string `json:"cardholder_id"`
			CreatedAt    int64  `json:"created_at"`
			CVV          string `json:"cvv"`
			ExpiryMonth  string `json:"expiry_month"`
			ExpiryYear   string `json:"expiry_year"`
			IsActive     bool   `json:"is_active"`
			IssuingAppID string `json:"issuing_app_id"`
			Last4        string `json:"last_4"`
			Livemode     bool   `json:"livemode"`
			MetaData     struct {
				UserID string `json:"user_id"`
			} `json:"meta_data"`
			Balance           string `json:"balance"`
			AvailableBalance  string `json:"available_balance"`
			BookBalance       string `json:"book_balance"`
			BlockedDueToFraud bool   `json:"blocked_due_to_fraud"`
			Pin3DSActivated   bool   `json:"pin_3ds_activated"`
		} `json:"cards"`
		Meta struct {
			Total int    `json:"total"`
			Pages int    `json:"pages"`
			Prev  string `json:"prev"`
			Next  string `json:"next"`
		} `json:"meta"`
	} `json:"data"`
}
