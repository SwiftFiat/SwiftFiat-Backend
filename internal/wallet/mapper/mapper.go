package mapper

import (
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/common/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/wallet/domain"
	"github.com/shopspring/decimal"
)

func ToWalletResponse(rhs *db.SwiftWallet) *domain.WalletResponse {
	return &domain.WalletResponse{
		ID:         rhs.ID,
		CustomerID: models.ID(rhs.CustomerID),
		Type:       rhs.Type,
		Currency:   rhs.Currency,
		Balance:    rhs.Balance.String,
		Status:     rhs.Status,
		CreatedAt:  rhs.CreatedAt,
		UpdatedAt:  rhs.UpdatedAt,
	}
}

func ToWalletCollectionResponse(accounts *[]db.SwiftWallet) domain.WalletCollectionResponse {
	response := make([]domain.WalletResponse, len(*accounts))
	for i, account := range *accounts {
		response[i] = *ToWalletResponse(&account)
	}
	return response
}

func ToWalletModel(wallet db.SwiftWallet) *domain.WalletModel {
	balance, err := decimal.NewFromString(wallet.Balance.String)
	if err != nil {
		balance = decimal.Zero
	}

	return &domain.WalletModel{
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
