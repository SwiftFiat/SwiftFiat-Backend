package models

import db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"

func ToWalletResponse(rhs *db.SwiftWallet) *WalletResponse {
	return &WalletResponse{
		ID:         rhs.ID,
		CustomerID: ID(rhs.CustomerID),
		Type:       rhs.Type,
		Currency:   rhs.Currency,
		Balance:    rhs.Balance.String,
		Status:     rhs.Status,
		CreatedAt:  rhs.CreatedAt,
		UpdatedAt:  rhs.UpdatedAt,
	}
}

func ToWalletCollectionResponse(accounts *[]db.SwiftWallet) WalletCollectionResponse {
	response := make([]WalletResponse, len(*accounts))
	for i, account := range *accounts {
		response[i] = *ToWalletResponse(&account)
	}
	return response
}
