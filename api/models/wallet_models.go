package models

import (
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/google/uuid"
)

type WalletCollectionResponse []WalletResponse

type WalletResponse struct {
	ID         uuid.UUID `json:"id"`
	CustomerID ID        `json:"customer_id"`
	Type       string    `json:"type"`
	Currency   string    `json:"currency"`
	Balance    string    `json:"balance"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TagResolveResponse struct {
	CustomerName string    `json:"customer_name"`
	WalletID     uuid.UUID `json:"wallet_id"`
	CustomerID   ID        `json:"customer_id"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
}

func ToTagResolveResponse(tagRes db.GetWalletByTagRow) *TagResolveResponse {
	fullName := tagRes.FirstName.String + " " + tagRes.LastName.String
	return &TagResolveResponse{
		CustomerName: fullName,
		WalletID:     tagRes.ID,
		CustomerID:   ID(tagRes.ID_2),
		Currency:     tagRes.Currency,
		Status:       tagRes.Status,
	}
}
