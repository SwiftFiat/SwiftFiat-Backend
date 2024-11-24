package user_service

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/lib/pq"
)

type UserService struct {
	store        *db.Store
	logger       *logging.Logger
	walletClient *wallet.WalletService
}

func NewUserService(store *db.Store, logger *logging.Logger, walletClient *wallet.WalletService) *UserService {
	return &UserService{
		store:        store,
		logger:       logger,
		walletClient: walletClient,
	}
}

func (u *UserService) CreateUserWithWallets(ctx context.Context, arg *db.CreateUserParams) (*db.User, error) {

	/// Start a new transaction if none is provided
	dbTx, err := u.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	newUser, err := u.store.WithTx(dbTx).CreateUser(context.Background(), *arg)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == db.DuplicateEntry {
				// 23505 --> Violated Unique Constraints
				return nil, NewUserError(ErrUserAlreadyExists, "", err)
			}
		}
		return nil, err
	}

	/// We create and return the user whether or not the wallet creation was successful
	_, err = u.walletClient.CreateWallets(ctx, dbTx, newUser.ID, true)
	if err != nil {
		u.logger.Error(fmt.Sprintf("failed to create wallets for user: %v", newUser.ID))
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &newUser, nil
}

func (u *UserService) FetchUserByEmail(ctx context.Context, dbTx *sql.Tx, email string) (*db.User, error) {

	/// Start a new transaction if none is provided
	if dbTx == nil {
		// Create transaction
		newTx, err := u.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
		if err != nil {
			return nil, fmt.Errorf("begin transaction: %w", err)
		}
		dbTx = newTx
	}
	defer dbTx.Rollback()

	dbUser, err := u.store.WithTx(dbTx).GetUserByEmail(ctx, email)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	} else if err != nil {
		return nil, err
	}

	return &dbUser, nil
}
