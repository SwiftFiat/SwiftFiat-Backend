package transaction

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionType string

const (
	Transfer TransactionType = "transfer"
	Deposit  TransactionType = "deposit"
	Swap     TransactionType = "swap"
	GiftCard TransactionType = "giftcard"
	Airtime  TransactionType = "airtime"
)

type TransactionPlatform string

const (
	WalletTransaction       TransactionPlatform = "wallet"
	CryptoInflowTransaction TransactionPlatform = "crypto"
)

type Transaction struct {
	ID            string
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	Amount        decimal.Decimal
	UserTag       string
	Currency      string
	Description   string
	Type          TransactionType
}

type CryptoTransaction struct {
	ID                 string
	SourceHash         string
	DestinationAddress string
	DestinationAccount uuid.UUID
	AmountInSatoshis   decimal.Decimal
	Coin               string
	Description        string
	Type               TransactionType
}

type LedgerEntries struct {
	TransactionID uuid.UUID
	Debit         Entry
	Credit        Entry
	Platform      TransactionPlatform
}

type Entry struct {
	AccountID uuid.UUID
	Amount    decimal.Decimal
	Balance   decimal.Decimal
}
