package transaction

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionService struct {
	store          *db.Store
	currencyClient *currency.CurrencyService
	walletClient   *wallet.WalletService
	logger         *logging.Logger
}

func NewTransactionService(store *db.Store, currencyClient *currency.CurrencyService, walletClient *wallet.WalletService, logger *logging.Logger) *TransactionService {
	return &TransactionService{
		store:          store,
		currencyClient: currencyClient,
		walletClient:   walletClient,
		logger:         logger,
	}
}

func (s *TransactionService) CreateTransaction(ctx context.Context, tx Transaction) error {
	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Get account details
	fromAccount, err := s.walletClient.GetWallet(ctx, dbTx, tx.FromAccountID)
	if err != nil {
		return fmt.Errorf("get from account: %w", err)
	}

	toAccount, err := s.walletClient.GetWallet(ctx, dbTx, tx.ToAccountID)
	if err != nil {
		return fmt.Errorf("get to account: %w", err)
	}

	// Handle currency conversion if needed
	amount := tx.Amount
	if fromAccount.Currency != toAccount.Currency {
		rate, err := s.currencyClient.GetExchangeRate(ctx, fromAccount.Currency, toAccount.Currency)
		if err != nil {
			return fmt.Errorf("get exchange rate: %w", err)
		}
		// TODO: Have a function that performs multiplication like Mul instead of direct aug
		amount = tx.Amount.Mul(rate)
	}

	// Check sufficient balance
	if fromAccount.Balance.LessThan(tx.Amount) {
		return ErrInsufficientFunds
	}

	// Create transaction record
	if err := s.createTransactionRecord(ctx, dbTx, tx); err != nil {
		return fmt.Errorf("create transaction record: %w", err)
	}

	// Create ledger entries
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tx.ID,
		Debit: Entry{
			AccountID: tx.FromAccountID,
			Amount:    tx.Amount,
		},
		Credit: Entry{
			AccountID: tx.ToAccountID,
			Amount:    amount,
		},
	}); err != nil {
		return fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, tx.FromAccountID, tx.Amount.Neg()); err != nil {
		return fmt.Errorf("update from account balance: %w", err)
	}

	if err := s.updateBalance(ctx, dbTx, tx.ToAccountID, amount); err != nil {
		return fmt.Errorf("update to account balance: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("transaction completed successfully", tx)

	return nil
}

func (s *TransactionService) createTransactionRecord(ctx context.Context, dbTx *sql.Tx, tx Transaction) error {
	// Convert decimal amount to string for storage
	amountStr := tx.Amount.String()

	params := db.CreateWalletTransactionParams{
		Type: tx.Type,
		FromAccountID: uuid.NullUUID{
			UUID:  tx.FromAccountID,
			Valid: tx.FromAccountID.URN() != "",
		},
		ToAccountID: uuid.NullUUID{
			UUID:  tx.ToAccountID,
			Valid: tx.ToAccountID.URN() != "",
		},
		Amount:   amountStr,
		Currency: tx.Currency,
		Description: sql.NullString{
			String: tx.Description,
			Valid:  tx.Description != "",
		},
	}

	if _, err := s.store.WithTx(dbTx).CreateWalletTransaction(ctx, params); err != nil {
		return fmt.Errorf("failed to create transaction record: %w", err)
	}

	return nil
}

func (s *TransactionService) createLedgerEntries(ctx context.Context, dbTx *sql.Tx, le LedgerEntries) error {
	// Create debit entry
	debitParams := db.CreateWalletLedgerEntryParams{
		TransactionID: uuid.MustParse(le.TransactionID),
		AccountID:     le.Debit.AccountID,
		Type:          "debit",
		Amount:        le.Debit.Amount.String(),
	}

	if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, debitParams); err != nil {
		return fmt.Errorf("failed to create debit entry: %w", err)
	}

	// Create credit entry
	creditParams := db.CreateWalletLedgerEntryParams{
		TransactionID: uuid.MustParse(le.TransactionID),
		AccountID:     le.Credit.AccountID,
		Type:          "credit",
		Amount:        le.Credit.Amount.String(),
	}

	if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, creditParams); err != nil {
		return fmt.Errorf("failed to create credit entry: %w", err)
	}

	return nil
}

// QUE: Should this be a Wallet Service function??
func (s *TransactionService) updateBalance(ctx context.Context, dbTx *sql.Tx, accId uuid.UUID, amt decimal.Decimal) error {
	params := db.UpdateWalletBalanceParams{
		ID: accId,
		Balance: sql.NullString{
			String: amt.String(),
			Valid:  amt.String() != "",
		},
	}

	if _, err := s.store.WithTx(dbTx).UpdateWalletBalance(ctx, params); err != nil {
		return fmt.Errorf("failed to update account balance: %w", err)
	}

	return nil
}
