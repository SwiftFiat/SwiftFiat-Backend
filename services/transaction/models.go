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

type LedgerEntries struct {
	TransactionID uuid.UUID
	Debit         Entry
	Credit        Entry
}

type Entry struct {
	AccountID uuid.UUID
	Amount    decimal.Decimal
	Balance   decimal.Decimal
}
