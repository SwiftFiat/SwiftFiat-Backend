package transaction

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionStatus string

const (
	Success             TransactionStatus = "successful"
	Pending             TransactionStatus = "pending"
	Failed              TransactionStatus = "failed"
	Unknown             TransactionStatus = "unknown"
	MinAirtimeAmount                      = 50
	InAppTransferLimitu                   = 50000
	InAppTransferLimitv                   = 10000000
	BankTransferLimitu                    = 50000
	BankTransferLimitv                    = 10000000
	AirtimeDailyLimitu                    = 50000
	AirtimeDailyLimitv                    = 200000
	OtherBillsLimitu                      = 500000
	OtherBillsLimitv                      = 1000000
)

type TransactionType string // type of transaction

const (
	Transfer       TransactionType = "transfer"
	Withdrawal     TransactionType = "withdrawal"
	Deposit        TransactionType = "deposit"
	Swap           TransactionType = "swap"
	Vault          TransactionType = "vault"
	GiftCard       TransactionType = "giftcard"
	Airtime        TransactionType = "airtime"
	Data           TransactionType = "data"
	TV             TransactionType = "tv_subscription"
	Electricity    TransactionType = "electricity"
	Card           TransactionType = "card"
	QrCode         TransactionType = "qr_code"
	UtilityPayment TransactionType = "utility_payment"
	Rewards        TransactionType = "rewards"
	Referral       TransactionType = "referral"
	Wallet         TransactionType = "wallet"
	Other          TransactionType = "other"
)

type TransactionFlow string

const (
	Inflow     TransactionFlow = "inflow"
	Outflow    TransactionFlow = "outflow"
	InPlatform TransactionFlow = "inplatform"
)

type TransactionDirection string

const (
	Credit TransactionDirection = "credit"
	Debit  TransactionDirection = "debit"
)

type Currency string

const (
	NGN Currency = "NGN"
	USD Currency = "USD"
)

var SupportedTransactions = []TransactionType{Transfer, Withdrawal, Deposit, Swap, Vault, GiftCard, Airtime, Data, TV, Electricity}

func IsTransactionTypeValid(request TransactionType) bool {
	for _, c := range SupportedTransactions {
		if request == c {
			return true
		}
	}
	return false
}

type TransactionPlatform string // platform that the transaction was initiated for

const (
	WalletTransaction                  TransactionPlatform = "wallet"
	CryptoInflowTransaction            TransactionPlatform = "crypto"
	GiftCardOutflowTransaction         TransactionPlatform = "giftcard"
	FiatOutflowTransaction             TransactionPlatform = "fiat"
	BillOutflowTransaction             TransactionPlatform = "bill"
	StablecoinWalletFundingTransaction TransactionPlatform = "stablecoin_funding"
)

var SupportedBillTransactions = []TransactionType{Airtime, Data, TV, Electricity}

type IntraTransaction struct {
	ID             string
	FromAccountID  uuid.UUID
	ToAccountID    uuid.UUID
	SentAmount     decimal.Decimal
	Rate           decimal.Decimal
	ReceivedAmount decimal.Decimal
	Fees           decimal.Decimal
	UserTag        string
	Currency       string
	Description    string
	Type           TransactionType
}

type CryptoTransaction struct {
	ID                 string
	SourceHash         string
	DestinationAddress string
	TransactionID      uuid.UUID
	Rate               decimal.Decimal
	SentAmount         decimal.Decimal
	ReceivedAmount     decimal.Decimal
	DestinationAccount uuid.UUID
	AmountInSatoshis   decimal.Decimal
	Coin               string
	Fees               decimal.Decimal
	Description        string
	Type               TransactionType
}

type GiftCardTransaction struct {
	ID               string
	TransactionID    uuid.UUID
	SourceWalletID   uuid.UUID
	Rate             decimal.Decimal
	ReceivedAmount   decimal.Decimal
	SentAmount       decimal.Decimal
	Fees             decimal.Decimal
	WalletCurrency   string
	WalletBalance    decimal.Decimal
	GiftCardCurrency string
	Description      string
	Type             TransactionType
}

type FiatTransaction struct {
	ID                         string
	SourceWalletID             uuid.UUID
	SentAmount                 decimal.Decimal
	Rate                       decimal.Decimal
	ReceivedAmount             decimal.Decimal
	Fees                       decimal.Decimal
	WalletCurrency             string
	WalletBalance              decimal.Decimal
	DestinationAccountCurrency string
	DestinationAccountNumber   string
	DestinationAccountName     string
	DestinationAccountBankCode string
	Description                string
	Type                       TransactionType
}

type BuyAirtimeRequest struct {
	ServiceID       string  `json:"service_id" binding:"required"`
	Phone           string  `json:"phone" binding:"required"`
	Amount          int64   `json:"amount" binding:"required"`
	Pin             string  `json:"pin" binding:"required"`
	UseRewardPoints bool    `json:"use_reward_points"`
	IdempotencyKey  string  `json:"idempotency_key" binding:"required"`
	PointsToUse     float32 `json:"points_to_use"`
}

func (req *BuyAirtimeRequest) GetRewardFields() (bool, float32) {
	return req.UseRewardPoints, req.PointsToUse
}

type BuyAirtimeResponse struct {
	Amount               decimal.Decimal `json:"amount"`
	AmountPaid           float64         `json:"amount_paid"`
	BonusEarned          float64         `json:"bonus_earned"`
	Phone                string          `json:"phone"`
	TransactionType      string          `json:"transaction_type"`
	Date                 time.Time       `json:"transaction_date"`
	TransactionReference string          `json:"transaction_reference"`
	Status               string          `json:"status"`
	PointsUsed           float64         `json:"cashback_used"`
}

type BuyDataRequest struct {
	ServiceID       string  `json:"service_id" binding:"required"`
	Phone           string  `json:"phone" binding:"required"`
	VariationCode   string  `json:"variation_code" binding:"required"`
	Pin             string  `json:"pin" binding:"required"`
	UseRewardPoints bool    `json:"use_reward_points"`
	PointsToUse     float32 `json:"points_to_use"`
	IdempotencyKey  string  `json:"idempotency_key" binding:"required"`
}

func (req *BuyDataRequest) GetRewardFields() (bool, float32) {
	return req.UseRewardPoints, req.PointsToUse
}

type BuyDataResponse struct {
	Amount               string    `json:"amount"`
	AmountPaid           float64   `json:"amount_paid"`
	BonusEarned          float64   `json:"bonus_earned"`
	Phone                string    `json:"phone"`
	TransactionType      string    `json:"transaction_type"`
	Date                 time.Time `json:"transaction_date"`
	TransactionReference string    `json:"transaction_reference"`
	Status               string    `json:"status"`
	Plan                 string    `json:"plan"`
	PointsUsed           float64   `json:"cashback_used"`
}

type TVSubRequest struct {
	ServiceID        string  `json:"service_id" binding:"required"`
	BillersCode      string  `json:"billers_code" binding:"required"`
	SubscriptionType string  `json:"subscription_type" binding:"required"`
	VariationCode    string  `json:"variation_code" binding:"required"`
	Pin              string  `json:"pin" binding:"required"`
	UseRewardPoints  bool    `json:"use_reward_points"`
	PointsToUse      float32 `json:"points_to_use"`
	IdempotencyKey   string  `json:"idempotency_key" binding:"required"`
}

func (req *TVSubRequest) GetRewardFields() (bool, float32) {
	return req.UseRewardPoints, req.PointsToUse
}

type TVSubResponse struct {
	Amount               decimal.Decimal `json:"amount"`
	AmountPaid           float64         `json:"amount_paid"`
	BonusEarned          float64         `json:"bonus_earned"`
	TransactionType      string          `json:"transaction_type"`
	Date                 time.Time       `json:"transaction_date"`
	TransactionReference string          `json:"transaction_reference"`
	Status               string          `json:"status"`
	Plan                 string          `json:"plan"`
	PointsUsed           float64         `json:"cashback_used"`
}

type ElectricityRequest struct {
	ServiceID       string  `json:"service_id" binding:"required"`
	BillersCode     string  `json:"billers_code" binding:"required"`
	VariationCode   string  `json:"variation_code" binding:"required"`
	Amount          float64 `json:"amount" binding:"required"`
	Pin             string  `json:"pin" binding:"required"`
	UseRewardPoints bool    `json:"use_reward_points"`
	PointsToUse     float32 `json:"points_to_use"`
	IdempotencyKey  string  `json:"idempotency_key" binding:"required"`
}

func (req *ElectricityRequest) GetRewardFields() (bool, float32) {
	return req.UseRewardPoints, req.PointsToUse
}

type ElectricityResponse struct {
	Amount               string    `json:"amount"`
	AmountPaid           float64   `json:"amount_paid"`
	BonusEarned          float64   `json:"bonus_earned"`
	TransactionType      string    `json:"transaction_type"`
	Date                 time.Time `json:"transaction_date"`
	TransactionReference string    `json:"transaction_reference"`
	Status               string    `json:"status"`
	CustomerName         string    `json:"customer_name"`
	CustomerAddress      string    `json:"customer_address"`
	Token                string    `json:"token"`
	Units                string    `json:"units"`
	ProviderRequestID    string    `json:"provider_request_id"`
	TokenAmount          any       `json:"token_amount"`
	MeterNumber          string    `json:"meter_number"`
	TaxAmount            any       `json:"tax"`
	Debt                 any       `json:"debt"`
	FixChargeAmount      any       `json:"fixChargeAmount"`
	PointsUsed           float64   `json:"cashback_used"`
}

type BillTransaction struct {
	ID              string
	SourceWalletID  uuid.UUID
	SentAmount      decimal.Decimal
	Rate            decimal.Decimal
	ReceivedAmount  decimal.Decimal
	Fees            decimal.Decimal
	WalletCurrency  string
	WalletBalance   decimal.Decimal
	ServiceCurrency string
	ServiceID       string
	Description     string
	Type            TransactionType
}

type LedgerEntries struct {
	TransactionID   uuid.UUID
	Debit           Entry
	Credit          Entry
	idempotency_key string
	Platform        TransactionPlatform
	SourceType      LedgerSourceDestination
	DestinationType LedgerSourceDestination
}

type LedgerSourceDestination string

const (
	OnPlatform  LedgerSourceDestination = "on-platform"
	OffPlatform LedgerSourceDestination = "off-platform"
)

type Entry struct {
	AccountID uuid.UUID
	Amount    decimal.Decimal
}

type TransactionResponse[T any] struct {
	ID              uuid.UUID `json:"id"`
	Type            string    `json:"type"`
	Description     string    `json:"description"`
	TransactionFlow string    `json:"transaction_flow"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Metadata        *T        `json:"metadata"`
}

type SwapTransferMetadataResponse struct {
	ID                uuid.UUID `json:"id"`
	Currency          string    `json:"currency"`
	TransferType      string    `json:"transfer_type"`
	Description       string    `json:"description,omitempty"`
	SourceWallet      uuid.UUID `json:"source_wallet"`
	DestinationWallet uuid.UUID `json:"destination_wallet"`
	UserTag           string    `json:"user_tag,omitempty"`
	Rate              string    `json:"rate"`
	Fees              string    `json:"fees"`
	ReceivedAmount    string    `json:"received_amount,omitempty"`
	SentAmount        string    `json:"sent_amount,omitempty"`
}

type CryptoMetadataResponse struct {
	ID                uuid.UUID `json:"id"`
	DestinationWallet string    `json:"destination_wallet"`
	Coin              string    `json:"coin"`
	Rate              float64   `json:"rate,omitempty"`
	Fees              float32   `json:"fees,omitempty"`
	ReceivedAmount    string    `json:"received_amount,omitempty"`
	SentAmount        float64   `json:"sent_amount,omitempty"`
	OrderID           string    `json:"order_id"`
}

type GiftcardMetadataResponse struct {
	ID                   uuid.UUID `json:"id"`
	SourceWallet         uuid.UUID `json:"source_wallet"`
	Rate                 string    `json:"rate,omitempty"`
	ReceivedAmount       string    `json:"received_amount,omitempty"`
	SentAmount           string    `json:"sent_amount,omitempty"`
	Fees                 string    `json:"fees,omitempty"`
	ServiceProvider      string    `json:"service_provider"`
	ServiceTransactionID string    `json:"service_transaction_id,omitempty"`
}

type FiatWithdrawalMetadataResponse struct {
	ID                   uuid.UUID `json:"id"`
	SourceWallet         uuid.UUID `json:"source_wallet"`
	Rate                 string    `json:"rate,omitempty"`
	ReceivedAmount       string    `json:"received_amount,omitempty"`
	SentAmount           string    `json:"sent_amount,omitempty"`
	Fees                 string    `json:"fees,omitempty"`
	AccountName          string    `json:"account_name,omitempty"`
	BankCode             string    `json:"bank_code,omitempty"`
	AccountNumber        string    `json:"account_number,omitempty"`
	ServiceProvider      string    `json:"service_provider,omitempty"`
	ServiceTransactionID string    `json:"service_transaction_id,omitempty"`
}

type BillMetadataResponse struct {
	ID                   uuid.UUID                    `json:"id"`
	SourceWallet         uuid.UUID                    `json:"source_wallet"`
	Rate                 string                       `json:"rate,omitempty"`
	ReceivedAmount       string                       `json:"received_amount,omitempty"`
	SentAmount           string                       `json:"sent_amount,omitempty"`
	Fees                 string                       `json:"fees,omitempty"`
	ServiceProvider      string                       `json:"service_provider,omitempty"`
	ServiceTransactionID string                       `json:"service_transaction_id,omitempty"`
	ElectricityMetadata  *ElectricityMetadataResponse `json:"electricity_metadata,omitempty"`
}

type ElectricityMetadataResponse struct {
	PurchasedCode     string  `json:"purchased_code"`
	CustomerName      *string `json:"customerName"`
	CustomerAddress   *string `json:"customerAddress"`
	Token             string  `json:"token"`
	TokenAmount       float64 `json:"tokenAmount"`
	ExchangeReference string  `json:"exchangeReference"`
	ResetToken        *string `json:"resetToken"`
	ConfigureToken    *string `json:"configureToken"`
	Units             string  `json:"units"`
	FixChargeAmount   *string `json:"fixChargeAmount"`
	Tariff            string  `json:"tariff"`
	TaxAmount         *string `json:"taxAmount"`
}

type CreateTransactionFeeRequest struct {
	TransactionType string  `json:"transaction_type" binding:"required"`
	FeePercentage   float64 `json:"fee_percentage" binding:"gte=0"` // changed to gte=0
	MaxFee          float64 `json:"max_fee" binding:"gte=0"`
}

type StablecoinFundingTransaction struct {
	SourceHash         string
	DestinationAddress string
	AmountInSatoshis   decimal.Decimal
	Coin               string
	Description        string
	Type               TransactionType
	ReceivedAmount     decimal.Decimal
	TransactionID      uuid.UUID
	DestinationAccount uuid.UUID
	Rate               decimal.Decimal
	SentAmount         decimal.Decimal
	Fees               decimal.Decimal
}

type StablecoinMetadataResponse struct {
	ID                   int32     `json:"id"`
	DestinationWallet    uuid.UUID `json:"destination_wallet"`
	Coin                 string    `json:"coin"`
	SourceHash           string    `json:"source_hash"`
	Rate                 string    `json:"rate"`
	Fees                 string    `json:"fees"`
	ReceivedAmount       string    `json:"received_amount"`
	SentAmount           string    `json:"sent_amount"`
	ServiceProvider      string    `json:"service_provider"`
	ServiceTransactionID string    `json:"service_transaction_id"`
}

type WalletTransferRequest struct {
	Currency           string  `json:"currency" binding:"required"`
	Amount             float64 `json:"amount" binding:"required"`
	DestinationUserTag string  `json:"target_user_tag" binding:"required"`
	Description        string  `json:"description"`
	Pin                string  `json:"pin" binding:"required"`
	IdempotencyKey     string  `json:"idempotency_key" binding:"required"`
}

type WalletTransferResponse struct {
	Sender     string    `json:"sender"`
	Recipient  string    `json:"recipient"`
	Amount     float32   `json:"amount"`
	AmountPaid float64   `json:"amount_paid"`
	Remark     string    `json:"remark"`
	Type       string    `json:"transaction_type"`
	Date       time.Time `json:"date"`
	Status     string    `json:"status"`
	Reference  string    `json:"reference"`
}

type BankTransferRequest struct {
	Name            string  `json:"name" binding:"required"`
	AccountNumber   string  `json:"account_number" binding:"required"`
	BankCode        string  `json:"bank_code" binding:"required"`
	Amount          float64 `json:"amount" binding:"required"`
	Pin             string  `json:"pin" binding:"required"`
	SaveBeneficiary bool    `json:"save_beneficiary,omitempty"`
	Description     string  `json:"description,omitempty"`
	IdempotencyKey  string  `json:"idempotency_key" binding:"required"`
}

type BankTransferResponse struct {
	Sender         string    `json:"sender"`
	Recipient      string    `json:"recipient"`
	Account_number string    `json:"account_number"`
	BankCode       string    `json:"bank_code"`
	Amount         float64   `json:"amount"`
	AmountPaid     float64   `json:"amount_paid"`
	Remark         string    `json:"remark"`
	Type           string    `json:"transaction_type"`
	Date           time.Time `json:"date"`
	Status         string    `json:"status"`
	Reference      string    `json:"reference"`
}
