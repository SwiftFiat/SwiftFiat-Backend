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
)

type TransactionService struct {
	store          *db.Store
	currencyClient *currency.CurrencyService
	walletClient   *wallet.WalletService
	logger         *logging.Logger
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
		amount = tx.Amount * rate
	}

	// Check sufficient balance
	// TODO perform less_than comparison in int64, not string
	if fromAccount.Balance.String < string(tx.Amount) {
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
	if err := s.updateBalance(ctx, dbTx, tx.FromAccountID, -1*(tx.Amount)); err != nil {
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
	// TODO -> Implement transaction
	return nil
}

func (s *TransactionService) createLedgerEntries(ctx context.Context, dbTx *sql.Tx, le LedgerEntries) error {
	// TODO -> Implement ledge creation
	return nil
}

func (s *TransactionService) updateBalance(ctx context.Context, dbTx *sql.Tx, accId uuid.UUID, amt int64) error {
	// TODO -> Implement balance update
	return nil
}
