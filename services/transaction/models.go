package transaction

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionStatus string

const (
	Success TransactionStatus = "success"
	Pending TransactionStatus = "pending"
	Failed  TransactionStatus = "failed"
	Unknown TransactionStatus = "unknown"
)

type TransactionType string // type of transaction

const (
	Transfer    TransactionType = "transfer"
	Withdrawal  TransactionType = "withdrawal"
	Deposit     TransactionType = "deposit"
	Swap        TransactionType = "swap"
	GiftCard    TransactionType = "giftcard"
	Airtime     TransactionType = "airtime"
	Data        TransactionType = "data"
	TV          TransactionType = "tv"
	Electricity TransactionType = "electricity"
)

var SupportedTransactions = []TransactionType{Transfer, Withdrawal, Deposit, Swap, GiftCard, Airtime, Data, TV, Electricity}

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
	WalletTransaction          TransactionPlatform = "wallet"
	CryptoInflowTransaction    TransactionPlatform = "crypto"
	GiftCardOutflowTransaction TransactionPlatform = "giftcard"
	FiatOutflowTransaction     TransactionPlatform = "fiat"
	BillOutflowTransaction     TransactionPlatform = "bill"
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
	Balance   decimal.Decimal
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
	ID                   uuid.UUID `json:"id"`
	DestinationWallet    uuid.UUID `json:"destination_wallet"`
	Coin                 string    `json:"coin"`
	SourceHash           string    `json:"source_hash,omitempty"`
	Rate                 string    `json:"rate,omitempty"`
	Fees                 string    `json:"fees,omitempty"`
	ReceivedAmount       string    `json:"received_amount,omitempty"`
	SentAmount           string    `json:"sent_amount,omitempty"`
	ServiceProvider      string    `json:"service_provider"`
	ServiceTransactionID string    `json:"service_transaction_id,omitempty"`
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
