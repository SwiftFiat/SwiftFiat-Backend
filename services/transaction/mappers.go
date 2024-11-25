package transaction

import (
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/shopspring/decimal"
)

func ToTransactionModelResponse(tx *db.Transaction, userTag *string) *Transaction {
	return &Transaction{
		ID:            tx.ID.String(),
		FromAccountID: tx.FromAccountID.UUID,
		ToAccountID:   tx.ToAccountID.UUID,
		Amount:        decimal.RequireFromString(tx.Amount),
		Currency:      tx.Currency,
		Description:   tx.Description.String,
		Type:          TransactionType(tx.Type),
		UserTag:       *userTag,
	}
}
