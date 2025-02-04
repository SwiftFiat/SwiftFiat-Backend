package transaction

import (
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

func ToTransactionModelResponse(tx *db.Transaction, userTag *string) *IntraTransaction {
	return &IntraTransaction{
		ID:          tx.ID.String(),
		Description: tx.Description.String,
		Type:        TransactionType(tx.Type),
		UserTag:     *userTag,
	}
}
