package wallet

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/google/uuid"
)

type WalletService struct {
	store  *db.Store
	logger *logging.Logger
}

func NewWalletService(store *db.Store, logger *logging.Logger) *WalletService {
	return &WalletService{
		store:  store,
		logger: logger,
	}
}

func (w *WalletService) GetWallet(ctx context.Context, dbTx *sql.Tx, walletID uuid.UUID) (*WalletModel, error) {
	w.logger.Info("Fetching wallet")
	db_wallet, err := w.store.WithTx(dbTx).GetWallet(ctx, walletID)
	if err == sql.ErrNoRows {
		return nil, ErrWalletNotFound
	} else if err != nil {
		return nil, err
	}
	return ToWalletModel(db_wallet), err
}

// / QUE: Should be in Wallet? or UserService?
func (w *WalletService) ResolveTag(ctx context.Context, dbTx *sql.Tx, tag string) (interface{}, error) {
	w.logger.Info(fmt.Sprintf("Fetching user account for tag -> %v", tag))
	// db_wallet, err := w.store.WithTx(dbTx).GetWallet(ctx, walletID)
	return nil, fmt.Errorf("not implemented")
}
