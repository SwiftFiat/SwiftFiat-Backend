package transaction

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Transaction struct {
	ID            string
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	Amount        decimal.Decimal
	Currency      string
	Description   string
	Type          string
}

type LedgerEntries struct {
	TransactionID string
	Debit         Entry
	Credit        Entry
}

type Entry struct {
	AccountID uuid.UUID
	Amount    decimal.Decimal
}
