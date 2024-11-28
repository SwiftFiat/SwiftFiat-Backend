package models

import (
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/cryptocurrency"
	"github.com/google/uuid"
)

type AddressUserResponse struct {
	AddressID  uuid.UUID `json:"id"`
	CustomerID ID        `json:"customer_id"`
	Coin       string    `json:"currency"`
	Balance    string    `json:"balance"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func MapWalletAddressToAddressUserResponse(walletAddress *cryptocurrency.WalletAddress, customerID ID, balance string, status string, createdAt, updatedAt time.Time) *AddressUserResponse {
	return &AddressUserResponse{
		AddressID:  uuid.MustParse(walletAddress.ID), // Assuming walletAddress.ID is a valid UUID
		CustomerID: customerID,
		Coin:       walletAddress.Coin,
		Balance:    balance,
		Status:     status,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}
}
