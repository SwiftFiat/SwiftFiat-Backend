package repository

import (
	"context"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/wallet/domain"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/wallet/mapper"
)

type SQLWalletRepository struct {
	queries *db.Queries
}

func NewSQLWalletRepository(queries *db.Queries) *SQLWalletRepository {
	return &SQLWalletRepository{queries: queries}
}

func (r *SQLWalletRepository) GetWalletsByUserID(ctx context.Context, userID int64) (*domain.WalletCollectionResponse, error) {
	wallet, err := r.queries.GetWalletByCustomerID(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Map sqlc result to domain model
	walletResponse := mapper.ToWalletCollectionResponse(&wallet)
	return &walletResponse, nil
}
