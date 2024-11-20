package transaction

import "github.com/google/uuid"

type Transaction struct {
	ID            string
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	Amount        int64
	Currency      string
	// other fields...
}

type LedgerEntries struct {
	TransactionID string
	Debit         Entry
	Credit        Entry
}

type Entry struct {
	AccountID uuid.UUID
	Amount    int64
}
