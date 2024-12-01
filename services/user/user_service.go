package user_service

import (
	"context"
	"database/sql"
	"errors"
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

func (u *UserService) CreateSwiftWalletForUser(ctx context.Context, userID int64) error {

	/// Start a new transaction if none is provided
	dbTx, err := u.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	/// We create and return the user whether or not the wallet creation was successful
	_, err = u.walletClient.CreateWallets(ctx, dbTx, userID, true)
	if err != nil {
		u.logger.Error(err)
		return fmt.Errorf("failed to create wallets for user: %v", userID)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (u *UserService) FetchUserByEmailWithTx(ctx context.Context, dbTx *sql.Tx, email string) (*db.User, error) {
	// Fetch user by email
	dbUser, err := u.store.WithTx(dbTx).GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("fetch user by email: %w", err)
	}
	return &dbUser, nil
}

func (u *UserService) FetchUserByEmail(ctx context.Context, email string) (*db.User, error) {
	// Start a new transaction
	dbTx, err := u.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Fetch user using the transactional function
	dbUser, err := u.FetchUserByEmailWithTx(ctx, dbTx, email)
	if err != nil {
		return nil, err
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return dbUser, nil
}

type AddressStatus string

// Active vs Inactive
var (
	// active   AddressStatus
	inactive AddressStatus
)

func (u *UserService) AssignWalletAddressToUser(ctx context.Context, walletAddress string, userID int64, walletCoin string) error {
	dbTx, err := u.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	/// Storing the address is better than storing the address ID as the address
	/// is what gets returned by the webhook for transaction notification
	_, err = u.store.WithTx(dbTx).AssignAddressToCustomer(ctx, db.AssignAddressToCustomerParams{
		CustomerID: sql.NullInt64{
			Int64: userID,
			Valid: userID != 0,
		},
		AddressID: walletAddress,
		Coin:      walletCoin,
		Balance: sql.NullString{
			String: "0",
			Valid:  true,
		},
		Status: string(inactive),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("customer not found: %v", err)
		} else {
			return fmt.Errorf("assigning address to user: %w", err)
		}
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
