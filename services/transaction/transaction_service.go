package transaction

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
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

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateTransaction(ctx context.Context, tx Transaction, user *utils.TokenObject) (interface{}, error) {
	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Get account details
	fromAccount, err := s.walletClient.GetWallet(ctx, dbTx, tx.FromAccountID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.FromAccountID.String())
	}

	// set Transaction base currency
	tx.Currency = fromAccount.Currency

	// User must own source wallet
	if user.UserID != fromAccount.CustomerID {
		s.logger.Error("illegal access: ", fmt.Sprintf("user tried accessing a wallet that doesn't belong to them. USER: %v, WALLETID: %v", user.UserID, fromAccount.ID))
		return nil, wallet.NewWalletError(wallet.ErrNotYours, tx.FromAccountID.String())
	}

	toAccount, err := s.walletClient.GetWallet(ctx, dbTx, tx.ToAccountID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.ToAccountID.String())
	}

	// User should be conducting a swap from their account to their account
	if tx.Type == Swap {
		if user.UserID != toAccount.CustomerID {
			s.logger.Error("illegal access: ", fmt.Sprintf("user tried swapping to a wallet that doesn't belong to them. USER: %v, WALLETID: %v", user.UserID, toAccount.ID))
			return nil, wallet.NewWalletError(wallet.ErrNotYours, tx.ToAccountID.String())
		}
	}

	// Track transaction currency type
	// e.g. EUR to USD or NGN to NGN
	// This would help for ledger tracking
	// Anonymous function to determine the currency flow e.g USD to USD
	currFlow := func(fromCurrency, toCurrency string) string {
		if fromCurrency == toCurrency {
			return fromCurrency + " to " + toCurrency
		}
		return fromCurrency + " to " + toCurrency
	}(fromAccount.Currency, toAccount.Currency) // Call the function with the appropriate arguments

	// Handle currency conversion if needed
	amount := tx.Amount
	if fromAccount.Currency != toAccount.Currency {
		rate, err := s.currencyClient.GetExchangeRate(ctx, fromAccount.Currency, toAccount.Currency)
		if err != nil {
			return nil, currency.NewCurrencyError(err, fromAccount.Currency, toAccount.Currency)
		}
		// TODO: Have a function that performs multiplication like Mul instead of direct aug
		amount = tx.Amount.Mul(rate)
	}

	// Check sufficient balance
	if fromAccount.Balance.LessThan(tx.Amount) {
		return nil, wallet.NewWalletError(wallet.ErrInsufficientFunds, tx.FromAccountID.String())
	}

	// Create transaction record
	tObj, err := s.createTransactionRecord(ctx, dbTx, WalletTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}

	// Create ledger entries
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Debit: Entry{
			AccountID: tx.FromAccountID,
			Amount:    tx.Amount,
			Balance:   fromAccount.Balance,
		},
		Credit: Entry{
			AccountID: tx.ToAccountID,
			Amount:    amount,
			Balance:   toAccount.Balance,
		},
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, tx.FromAccountID, tx.Amount.Neg()); err != nil {
		return nil, fmt.Errorf("update from account balance: %w", err)
	}

	if err := s.updateBalance(ctx, dbTx, tx.ToAccountID, amount); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("transaction completed successfully", tx)

	return tObj, nil
}

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateCryptoInflowTransaction(ctx context.Context, tx CryptoTransaction, prov *provider.ProviderService) (interface{}, error) {
	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Check if trail exists for this hash
	trailExists, err := s.store.WithTx(dbTx).CheckCryptoTransactionTrailByTransactionHash(ctx, tx.SourceHash)
	if err != nil {
		return nil, fmt.Errorf("issue with checking for transactionHash %v", err)
	}

	if trailExists {
		return nil, fmt.Errorf("transaction already recorded, please check transaction hash: %v", tx.SourceHash)
	}

	/// Update amount in transaction object to prevent future problems
	params := db.CreateCryptoTransactionTrailParams{
		AddressID:       tx.DestinationAddress,
		TransactionHash: tx.SourceHash,
		Amount: sql.NullString{
			String: tx.AmountInSatoshis.StringFixed(10),
			Valid:  !tx.AmountInSatoshis.IsZero(),
		},
	}
	_, err = s.store.WithTx(dbTx).CreateCryptoTransactionTrail(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("error creating transaction trail: %v", err)
	}

	// Track transaction currency type
	// e.g. BTC to USD or SOL to USD
	// This would help for ledger tracking
	// Anonymous function to determine the currency flow e.g BTC to USD
	currFlow := func(fromCurrency, toCurrency string) string {
		if fromCurrency == toCurrency {
			return fromCurrency + " to " + toCurrency
		}
		return fromCurrency + " to " + toCurrency
	}(tx.Coin, "USD") // Default to USD Transactions

	// Handle Coin conversion to USD
	rate, err := s.currencyClient.GetCryptoExchangeRate(ctx, tx.Coin, "USD", prov)
	if err != nil {
		return nil, currency.NewCurrencyError(err, tx.Coin, "USD")
	}

	/// Convert satoshis to coin
	coinAmount := tx.AmountInSatoshis.Div(decimal.NewFromFloat(1e8))
	amount := coinAmount.Mul(rate)

	// Get Address Info from DB
	walletAddress, err := s.store.WithTx(dbTx).FetchByAddressID(ctx, tx.DestinationAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch swift address with this addess ID: %v", err)
	}

	// Update Address Balance with Params
	updateAddressParams := db.UpdateAddressBalanceByAddressIDParams{
		AddressID: walletAddress.AddressID,
		Balance: sql.NullString{
			String: amount.String(),
			Valid:  amount.String() != "",
		},
	}

	// Update Crypto Address Balance in DB
	_, err = s.store.WithTx(dbTx).UpdateAddressBalanceByAddressID(ctx, updateAddressParams)
	if err != nil {
		return nil, fmt.Errorf("failed to update the swift address with new changes %v", err)
	}

	// Pull users USD wallet
	walletParams := db.GetWalletByCurrencyForUpdateParams{
		CustomerID: walletAddress.CustomerID.Int64,
		Currency:   "USD", // TODO: Look into these as constants or enums
	}
	userUSDWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, walletParams)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wallet for customer (%v) and currency (%v)", walletParams.CustomerID, walletParams.Currency)
	}

	// set Transaction Object destination account
	tx.DestinationAccount = userUSDWallet.ID

	// Create transaction record
	tObj, err := s.createTransactionRecord(ctx, dbTx, CryptoInflowTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}

	// Create ledger entries
	balance, err := decimal.NewFromString(userUSDWallet.Balance.String)
	if err != nil {
		s.logger.Error("failed to parse the balance string")
	}
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Credit: Entry{
			AccountID: userUSDWallet.ID,
			Amount:    amount,
			Balance:   balance,
		},
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, userUSDWallet.ID, amount); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("transaction completed successfully", tx)

	return tObj, nil
}

func (s *TransactionService) createTransactionRecord(ctx context.Context, dbTx *sql.Tx, platform TransactionPlatform, txx interface{}, currFlow string) (*db.Transaction, error) {
	// Convert decimal amount to string for storage

	if platform == WalletTransaction {
		tx, ok := txx.(*Transaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		amountStr := tx.Amount.String()

		params := db.CreateWalletTransactionParams{
			Type: string(tx.Type),
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
			CurrencyFlow: sql.NullString{
				String: currFlow,
				Valid:  currFlow != "",
			},
			Description: sql.NullString{
				String: tx.Description,
				Valid:  tx.Description != "",
			},
		}

		tObj, err := s.store.WithTx(dbTx).CreateWalletTransaction(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		return &tObj, nil

	}

	if platform == CryptoInflowTransaction {
		tx, ok := txx.(*CryptoTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		amountStr := tx.AmountInSatoshis.String()

		params := db.CreateWalletCryptoTransactionParams{
			Type: string(tx.Type),
			SourceHash: sql.NullString{
				String: tx.SourceHash,
				Valid:  tx.SourceHash != "",
			},
			Amount: amountStr,
			Coin: sql.NullString{
				String: tx.Coin,
				Valid:  tx.Coin != "",
			},
			ToAccountID: uuid.NullUUID{
				UUID:  tx.DestinationAccount,
				Valid: tx.DestinationAccount.URN() != "",
			},
			Currency: "XXX",
			CurrencyFlow: sql.NullString{
				String: currFlow,
				Valid:  currFlow != "",
			},
			Description: sql.NullString{
				String: tx.Description,
				Valid:  tx.Description != "",
			},
		}

		tObj, err := s.store.WithTx(dbTx).CreateWalletCryptoTransaction(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		return &tObj, nil

	}

	return nil, fmt.Errorf("cannot decipher platform type: %v", platform)

}

func (s *TransactionService) createLedgerEntries(ctx context.Context, dbTx *sql.Tx, le LedgerEntries) error {

	if le.Platform == WalletTransaction {
		/// There MUST be a DEBIT
		// Create debit entry
		debitParams := db.CreateWalletLedgerEntryParams{
			TransactionID: uuid.NullUUID{
				UUID:  le.TransactionID,
				Valid: le.TransactionID.URN() != "",
			},
			AccountID: uuid.NullUUID{
				UUID:  le.Debit.AccountID,
				Valid: le.Debit.AccountID.URN() != "",
			},
			Type:    "debit",
			Amount:  le.Debit.Amount.String(),
			Balance: le.Debit.Balance.String(),
		}

		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}
	}

	// Create credit entry
	creditParams := db.CreateWalletLedgerEntryParams{
		TransactionID: uuid.NullUUID{
			UUID:  le.TransactionID,
			Valid: le.TransactionID.URN() != "",
		},
		AccountID: uuid.NullUUID{
			UUID:  le.Credit.AccountID,
			Valid: le.Credit.AccountID.URN() != "",
		},
		Type:    "credit",
		Amount:  le.Credit.Amount.String(),
		Balance: le.Debit.Balance.String(),
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
		Amount: sql.NullString{
			String: amt.String(),
			Valid:  amt.String() != "",
		},
	}

	if _, err := s.store.WithTx(dbTx).UpdateWalletBalance(ctx, params); err != nil {
		return fmt.Errorf("failed to update account balance: %w", err)
	}

	return nil
}
