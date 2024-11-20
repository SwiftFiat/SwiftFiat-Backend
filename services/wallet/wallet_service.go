package wallet

import (
	"context"
	"database/sql"

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
	return ToWalletModel(db_wallet), err
}
