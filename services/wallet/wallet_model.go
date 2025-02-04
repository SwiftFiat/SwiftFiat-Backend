package wallet

import (
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type WalletModel struct {
	ID         uuid.UUID       `json:"id"`
	CustomerID int64           `json:"customer_id"`
	Type       string          `json:"type"`
	Currency   string          `json:"currency"`
	Balance    decimal.Decimal `json:"balance"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func ToWalletModel(wallet db.SwiftWallet) *WalletModel {
	balance, err := decimal.NewFromString(wallet.Balance.String)
	if err != nil {
		balance = decimal.Zero
	}

	return &WalletModel{
		ID:         wallet.ID,
		CustomerID: wallet.CustomerID,
		Type:       wallet.Type,
		Currency:   wallet.Currency,
		Balance:    balance,
		Status:     wallet.Status,
		CreatedAt:  wallet.CreatedAt,
		UpdatedAt:  wallet.UpdatedAt,
	}
}
