package domain

import (
	"time"

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
