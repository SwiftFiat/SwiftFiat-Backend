package repository

import (
	"context"

	"github.com/SwiftFiat/SwiftFiat-Backend/internal/wallet/domain"
)

type WalletRepository interface {
	GetWalletsByUserID(ctx context.Context, userID string) (*domain.WalletCollectionResponse, error)
	UpdateWalletBalance(ctx context.Context, userID string, balance float64) error
}
