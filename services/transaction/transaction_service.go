package transaction

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
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

// May return an arbitrary error or an error defined in [transaction_strings]
// Handles SWAP or WALLET <->  WALLET transactions (determined internally)
func (s *TransactionService) CreateWalletTransaction(ctx context.Context, tx IntraTransaction, user *utils.TokenObject) (*TransactionResponse[SwapTransferMetadataResponse], error) {
	s.logger.Info(fmt.Sprintf("starting wallet transaction: %s", tx.Type))
	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Get account details
	fromAccount, err := s.walletClient.GetWalletForUpdate(ctx, dbTx, tx.FromAccountID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.FromAccountID.String())
	}

	// set Transaction base currency
	tx.Currency = fromAccount.Currency

	// User must own source wallet - Verify Ownership
	if user.UserID != fromAccount.CustomerID {
		s.logger.Error("illegal access: ", fmt.Sprintf("user tried accessing a wallet that doesn't belong to them. USER: %v, WALLETID: %v", user.UserID, fromAccount.ID))
		return nil, wallet.NewWalletError(wallet.ErrNotYours, tx.FromAccountID.String())
	}

	toAccount, err := s.walletClient.GetWalletForUpdate(ctx, dbTx, tx.ToAccountID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.ToAccountID.String())
	}

	// User should be conducting a swap from their account to their account - Verify Ownership
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
	}(fromAccount.Currency, toAccount.Currency)

	// Handle currency conversion if needed
	var sentAmount decimal.Decimal
	var receivedAmount decimal.Decimal
	var rate decimal.Decimal
	var fees decimal.Decimal

	sentAmount = tx.SentAmount
	if fromAccount.Currency != toAccount.Currency {
		rate, err = s.currencyClient.GetExchangeRate(ctx, fromAccount.Currency, toAccount.Currency)
		if err != nil {
			return nil, currency.NewCurrencyError(err, fromAccount.Currency, toAccount.Currency)
		}
	} else {
		rate = decimal.New(1, 0)
	}
	// Calculate the received amount
	receivedAmount = tx.SentAmount.Mul(rate)

	/// update sent amount with FEES
	sentAmount, err = s.addTransactionFeesWithTx(ctx, dbTx, sentAmount, &fees, string(tx.Type))
	if err != nil {
		return nil, err
	}

	// Check sufficient balance
	if fromAccount.Balance.LessThan(sentAmount) {
		return nil, wallet.NewWalletError(wallet.ErrInsufficientFunds, tx.FromAccountID.String(), fmt.Errorf("amount required: %v", sentAmount))
	}

	// Reset values in transaction object
	tx.ReceivedAmount = receivedAmount
	tx.Fees = fees
	tx.Rate = rate

	// Create transaction record
	tempObj, err := s.createTransactionRecord(ctx, dbTx, WalletTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[SwapTransferMetadataResponse])

	// Create ledger entries for base transaction
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Debit: Entry{
			AccountID: tx.FromAccountID,
			Amount:    sentAmount,
			Balance:   fromAccount.Balance,
		},
		Credit: Entry{
			AccountID: tx.ToAccountID,
			Amount:    tx.ReceivedAmount,
			Balance:   toAccount.Balance,
		},
		Platform:        WalletTransaction,
		SourceType:      OnPlatform,
		DestinationType: OnPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, tx.FromAccountID, sentAmount.Neg()); err != nil {
		return nil, fmt.Errorf("update from account balance: %w", err)
	}

	if err := s.updateBalance(ctx, dbTx, tx.ToAccountID, receivedAmount); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("wallet (swap | transfer) transaction completed successfully", tx)

	return tObj, nil
}

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateCryptoInflowTransaction(ctx context.Context, tx CryptoTransaction, prov *providers.ProviderService) (*TransactionResponse[CryptoMetadataResponse], error) {
	s.logger.Info("starting crypto inflow transaction")

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
	}(tx.Coin, "USD") // Default to USD Transactions - Because it's easier to just credit the user's USD wallet

	// Handle Coin conversion to USD
	rate, err := s.currencyClient.GetCryptoExchangeRate(ctx, tx.Coin, "USD", prov)
	if err != nil {
		return nil, currency.NewCurrencyError(err, tx.Coin, "USD")
	}

	/// Convert satoshis to coin
	coinAmount, err := s.currencyClient.SatoshiToCoin(tx.AmountInSatoshis, tx.Coin)
	if err != nil {
		return nil, fmt.Errorf("failed to convert satoshis to coin: %v", err)
	}
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

	// set Transaction Object values
	tx.DestinationAccount = userUSDWallet.ID
	tx.Rate = rate
	tx.SentAmount = tx.AmountInSatoshis
	tx.ReceivedAmount = amount

	// Create transaction record
	tempObj, err := s.createTransactionRecord(ctx, dbTx, CryptoInflowTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[CryptoMetadataResponse])

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
		Platform:        CryptoInflowTransaction,
		SourceType:      OffPlatform,
		DestinationType: OnPlatform,
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

	s.logger.Info("crypto inflow transaction completed successfully", tx)

	return tObj, nil
}

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateGiftCardOutflowTransactionWithTx(ctx context.Context, dbTx *sql.Tx, user *db.User, tx GiftCardTransaction) (*TransactionResponse[GiftcardMetadataResponse], error) {
	// Get account details and lock it for processing
	fromAccount, err := s.walletClient.GetWalletForUpdate(ctx, dbTx, tx.SourceWalletID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.SourceWalletID.String(), fmt.Errorf("failed to pull and lock wallet information: %s", err))
	}

	// User must own source wallet - Verify Ownership
	if user.ID != fromAccount.CustomerID {
		s.logger.Error("illegal access: ", fmt.Sprintf("user tried accessing a wallet that doesn't belong to them. USER: %v, WALLETID: %v", user.ID, fromAccount.ID))
		return nil, wallet.NewWalletError(wallet.ErrNotYours, tx.SourceWalletID.String())
	}

	// set Transaction information
	tx.WalletCurrency = fromAccount.Currency
	tx.WalletBalance = fromAccount.Balance

	// Track transaction currency type
	// e.g. USD to USD or NGN to USD
	// This would help for ledger tracking
	// Anonymous function to determine the currency flow e.g NGN to USD
	currFlow := func(fromCurrency, toCurrency string) string {
		if fromCurrency == toCurrency {
			return fromCurrency + " to " + toCurrency
		}
		return fromCurrency + " to " + toCurrency
	}(tx.WalletCurrency, tx.GiftCardCurrency) // Default to USD Transactions

	var sentAmount decimal.Decimal
	var receivedAmount decimal.Decimal
	var rate decimal.Decimal
	var fees decimal.Decimal

	if tx.WalletCurrency != tx.GiftCardCurrency {
		// We are trying to convert from the GC-Currency to Wallet Currency because
		// the value will be multiplied into the sentAmount
		rate, err = s.currencyClient.GetExchangeRate(ctx, tx.GiftCardCurrency, tx.WalletCurrency)
		if err != nil {
			return nil, currency.NewCurrencyError(err, tx.WalletCurrency, tx.GiftCardCurrency)
		}
	} else {
		rate = decimal.New(1, 0)
	}

	// Calculate the received amount
	receivedAmount = tx.SentAmount
	/// The amount to be debited from the customer is converted to the GiftCard Currency
	sentAmount = tx.SentAmount.Mul(rate)

	/// update sent amount with FEES
	sentAmount, err = s.addTransactionFeesWithTx(ctx, dbTx, sentAmount, &fees, string(tx.Type))
	if err != nil {
		return nil, err
	}

	// Check sufficient balance
	if tx.WalletBalance.LessThan(sentAmount) {
		return nil, wallet.NewWalletError(wallet.ErrInsufficientFunds, tx.SourceWalletID.String(), fmt.Errorf("amount required: %v", sentAmount))
	}

	// Reset values in transaction object
	tx.ReceivedAmount = receivedAmount
	tx.Fees = fees
	tx.Rate = rate

	// Create transaction record
	tempObj, err := s.createTransactionRecord(ctx, dbTx, GiftCardOutflowTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[GiftcardMetadataResponse])

	// Create ledger entries
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Debit: Entry{
			AccountID: tx.SourceWalletID,
			Amount:    sentAmount,
			Balance:   tx.WalletBalance,
		},
		Platform:        GiftCardOutflowTransaction,
		SourceType:      OnPlatform,
		DestinationType: OffPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, tx.SourceWalletID, sentAmount.Neg()); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	return tObj, nil
}

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateFiatOutflowTransactionWithTx(ctx context.Context, dbTx *sql.Tx, user *db.User, tx FiatTransaction) (*TransactionResponse[FiatWithdrawalMetadataResponse], error) {
	// Get account details
	fromAccount, err := s.walletClient.GetWalletForUpdate(ctx, dbTx, tx.SourceWalletID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.SourceWalletID.String())
	}

	// User must own source wallet - Verify Ownership
	if user.ID != fromAccount.CustomerID {
		s.logger.Error("illegal access: ", fmt.Sprintf("user tried accessing a wallet that doesn't belong to them. USER: %v, WALLETID: %v", user.ID, fromAccount.ID))
		return nil, wallet.NewWalletError(wallet.ErrNotYours, tx.SourceWalletID.String())
	}

	// set Transaction information
	tx.WalletCurrency = fromAccount.Currency
	tx.WalletBalance = fromAccount.Balance

	// Track transaction currency type
	// e.g. USD to USD or NGN to USD
	// This would help for ledger tracking
	// Anonymous function to determine the currency flow e.g NGN to USD
	currFlow := func(fromCurrency, toCurrency string) string {
		if fromCurrency == toCurrency {
			return fromCurrency + " to " + toCurrency
		}
		return fromCurrency + " to " + toCurrency
	}(tx.WalletCurrency, tx.DestinationAccountCurrency) // Default to NGN Transactions

	var sentAmount decimal.Decimal
	var receivedAmount decimal.Decimal
	var rate decimal.Decimal
	var fees decimal.Decimal

	sentAmount = tx.SentAmount
	if tx.WalletCurrency != tx.DestinationAccountCurrency {
		rate, err = s.currencyClient.GetExchangeRate(ctx, tx.WalletCurrency, tx.DestinationAccountCurrency)
		if err != nil {
			return nil, currency.NewCurrencyError(err, tx.WalletCurrency, tx.DestinationAccountCurrency)
		}
		receivedAmount = tx.SentAmount.Mul(rate)
	} else {
		rate = decimal.New(1, 0)
	}

	/// update sent amount with FEES
	sentAmount, err = s.addTransactionFeesWithTx(ctx, dbTx, sentAmount, &fees, string(tx.Type))
	if err != nil {
		return nil, err
	}

	// Check sufficient balance
	if tx.WalletBalance.LessThan(sentAmount) {
		return nil, wallet.NewWalletError(wallet.ErrInsufficientFunds, tx.SourceWalletID.String(), fmt.Errorf("amount required: %v", sentAmount))
	}

	// Reset values in transaction object
	tx.ReceivedAmount = receivedAmount
	tx.Fees = fees
	tx.Rate = rate

	// Create transaction record
	tempObj, err := s.createTransactionRecord(ctx, dbTx, FiatOutflowTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[FiatWithdrawalMetadataResponse])

	// Create ledger entries
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Debit: Entry{
			AccountID: tx.SourceWalletID,
			Amount:    sentAmount,
			Balance:   tx.WalletBalance,
		},
		Platform:        FiatOutflowTransaction,
		SourceType:      OnPlatform,
		DestinationType: OffPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, tx.SourceWalletID, sentAmount.Neg()); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	return tObj, nil
}

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateBillPurchaseTransactionWithTx(ctx context.Context, dbTx *sql.Tx, user *db.User, tx BillTransaction) (*TransactionResponse[BillMetadataResponse], error) {
	// Get account details
	fromAccount, err := s.walletClient.GetWalletForUpdate(ctx, dbTx, tx.SourceWalletID)
	if err != nil {
		return nil, wallet.NewWalletError(err, tx.SourceWalletID.String())
	}

	// User must own source wallet - Verify Ownership
	if user.ID != fromAccount.CustomerID {
		s.logger.Error("illegal access: ", fmt.Sprintf("user tried accessing a wallet that doesn't belong to them. USER: %v, WALLETID: %v", user.ID, fromAccount.ID))
		return nil, wallet.NewWalletError(wallet.ErrNotYours, tx.SourceWalletID.String())
	}

	// set Transaction information
	tx.WalletCurrency = fromAccount.Currency
	tx.WalletBalance = fromAccount.Balance

	// Track transaction currency type
	// e.g. USD to USD or NGN to USD
	// This would help for ledger tracking
	// Anonymous function to determine the currency flow e.g NGN to USD
	currFlow := func(fromCurrency, toCurrency string) string {
		if fromCurrency == toCurrency {
			return fromCurrency + " to " + toCurrency
		}
		return fromCurrency + " to " + toCurrency
	}(tx.WalletCurrency, tx.ServiceCurrency) // Default to NGN Transactions

	var sentAmount decimal.Decimal
	var receivedAmount decimal.Decimal
	var rate decimal.Decimal
	var fees decimal.Decimal

	/// We'd need the sent amount to be in the wallet currency
	sentAmount = tx.SentAmount
	/// The amount to be received by the service provider
	receivedAmount = tx.SentAmount
	if tx.WalletCurrency != tx.ServiceCurrency {
		rate, err = s.currencyClient.GetExchangeRate(ctx, tx.WalletCurrency, tx.ServiceCurrency)
		if err != nil {
			return nil, currency.NewCurrencyError(err, tx.WalletCurrency, tx.ServiceCurrency)
		}
		/// The amount to be debited from the customer
		// if user is buying service, we'd need to convert the amount to the wallet currency
		// e.g if the amount is 93 and the rate is 1579.5 then the sent amount would be 0.0588
		sentAmount = tx.SentAmount.Div(rate)
	} else {
		rate = decimal.New(1, 0)
	}

	/// update sent amount with FEES
	sentAmount, err = s.addTransactionFeesWithTx(ctx, dbTx, sentAmount, &fees, string(tx.Type))
	if err != nil {
		return nil, err
	}

	// Check sufficient balance
	if tx.WalletBalance.LessThan(sentAmount) {
		return nil, wallet.NewWalletError(wallet.ErrInsufficientFunds, tx.SourceWalletID.String(), fmt.Errorf("amount required: %v", sentAmount))
	}

	// Reset values in transaction object
	tx.SentAmount = sentAmount
	tx.ReceivedAmount = receivedAmount
	tx.Fees = fees
	tx.Rate = rate

	// Create transaction record
	tempObj, err := s.createTransactionRecord(ctx, dbTx, BillOutflowTransaction, &tx, currFlow)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[BillMetadataResponse])

	// Create ledger entries
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Debit: Entry{
			AccountID: tx.SourceWalletID,
			Amount:    sentAmount,
			Balance:   tx.WalletBalance,
		},
		Platform:        BillOutflowTransaction,
		SourceType:      OnPlatform,
		DestinationType: OffPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	if err := s.updateBalance(ctx, dbTx, tx.SourceWalletID, sentAmount.Neg()); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	return tObj, nil
}

func (s *TransactionService) createTransactionRecord(ctx context.Context, dbTx *sql.Tx, platform TransactionPlatform, txx interface{}, currFlow string) (interface{}, error) {

	if platform == WalletTransaction {
		tx, ok := txx.(*IntraTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(tx.Type),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: sql.NullString{String: currFlow, Valid: currFlow != ""},
			Status:          string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		params := db.CreateSwapTransferMetadataParams{
			Currency:          tx.Currency,
			TransactionID:     tObj.ID,
			TransferType:      string(tx.Type),
			Description:       sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			SourceWallet:      uuid.NullUUID{UUID: tx.FromAccountID, Valid: tx.FromAccountID.URN() != ""},
			DestinationWallet: uuid.NullUUID{UUID: tx.ToAccountID, Valid: tx.ToAccountID.URN() != ""},
			UserTag:           sql.NullString{String: tx.UserTag, Valid: tx.UserTag != ""},
			Rate:              sql.NullString{String: tx.Rate.String(), Valid: true},           // Assuming rate is provided in the current context
			Fees:              sql.NullString{String: tx.Fees.String(), Valid: true},           // Assuming fees are provided in the current context
			ReceivedAmount:    sql.NullString{String: tx.ReceivedAmount.String(), Valid: true}, // Assuming received amount is based on the rate
			SentAmount:        sql.NullString{String: tx.SentAmount.String(), Valid: true},     // Assuming sent amount is based on the rate
		}

		swapTransMeta, err := s.store.WithTx(dbTx).CreateSwapTransferMetadata(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		response := TransactionResponse[SwapTransferMetadataResponse]{
			ID:              tObj.ID,
			Type:            string(tx.Type),
			Description:     tx.Description,
			TransactionFlow: currFlow,
			Status:          string(Success),
			CreatedAt:       tObj.CreatedAt,
			UpdatedAt:       tObj.UpdatedAt,
			Metadata: &SwapTransferMetadataResponse{
				ID:                swapTransMeta.ID,
				Currency:          swapTransMeta.Currency,
				TransferType:      swapTransMeta.TransferType,
				Description:       swapTransMeta.Description.String,
				SourceWallet:      swapTransMeta.SourceWallet.UUID,
				DestinationWallet: swapTransMeta.DestinationWallet.UUID,
				UserTag:           swapTransMeta.UserTag.String,
				Rate:              swapTransMeta.Rate.String,
				Fees:              swapTransMeta.Fees.String,
				ReceivedAmount:    swapTransMeta.ReceivedAmount.String,
				SentAmount:        swapTransMeta.SentAmount.String,
			},
		}

		return &response, nil

	}

	if platform == CryptoInflowTransaction {
		tx, ok := txx.(*CryptoTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(tx.Type),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: sql.NullString{String: currFlow, Valid: currFlow != ""},
			Status:          string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		params := db.CreateCryptoMetadataParams{
			DestinationWallet: uuid.NullUUID{
				UUID:  tx.DestinationAccount,
				Valid: tx.DestinationAccount.URN() != "",
			},
			TransactionID: tObj.ID,
			Coin:          tx.Coin,
			SourceHash: sql.NullString{
				String: tx.SourceHash,
				Valid:  tx.SourceHash != "",
			},
			Rate: sql.NullString{
				String: tx.Rate.String(),
				Valid:  true,
			},
			Fees: sql.NullString{
				String: tx.Fees.String(),
				Valid:  true,
			},
			ReceivedAmount: sql.NullString{
				String: tx.ReceivedAmount.String(),
				Valid:  true,
			},
			SentAmount: sql.NullString{
				String: tx.SentAmount.String(),
				Valid:  true,
			},
			ServiceProvider: providers.Bitgo,
			ServiceTransactionID: sql.NullString{
				String: "",    // Placeholder for service transaction ID -- TODO: Create function to update CryptoTransactionID
				Valid:  false, // Assuming service transaction ID is not always available
			},
		}

		cryptoMeta, err := s.store.WithTx(dbTx).CreateCryptoMetadata(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		response := TransactionResponse[CryptoMetadataResponse]{
			ID:              tObj.ID,
			Type:            string(tx.Type),
			Description:     tx.Description,
			TransactionFlow: currFlow,
			Status:          string(Success),
			CreatedAt:       tObj.CreatedAt,
			UpdatedAt:       tObj.UpdatedAt,
			Metadata: &CryptoMetadataResponse{
				ID:                   cryptoMeta.ID,
				DestinationWallet:    cryptoMeta.DestinationWallet.UUID,
				Coin:                 cryptoMeta.Coin,
				SourceHash:           cryptoMeta.SourceHash.String,
				Rate:                 cryptoMeta.Rate.String,
				Fees:                 cryptoMeta.Fees.String,
				ReceivedAmount:       cryptoMeta.ReceivedAmount.String,
				SentAmount:           cryptoMeta.SentAmount.String,
				ServiceProvider:      cryptoMeta.ServiceProvider,
				ServiceTransactionID: cryptoMeta.ServiceTransactionID.String,
			},
		}

		return &response, nil

	}

	if platform == GiftCardOutflowTransaction {
		tx, ok := txx.(*GiftCardTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(tx.Type),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: sql.NullString{String: currFlow, Valid: currFlow != ""},
			Status:          string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		params := db.CreateGiftcardMetadataParams{
			SourceWallet:    uuid.NullUUID{UUID: tx.SourceWalletID, Valid: tx.SourceWalletID.URN() != ""},
			TransactionID:   tObj.ID,
			Rate:            sql.NullString{String: tx.Rate.String(), Valid: true},
			ReceivedAmount:  sql.NullString{String: tx.ReceivedAmount.String(), Valid: true},
			SentAmount:      sql.NullString{String: tx.SentAmount.String(), Valid: true},
			Fees:            sql.NullString{String: tx.Fees.String(), Valid: true},
			ServiceProvider: providers.Reloadly,
			ServiceTransactionID: sql.NullString{
				String: "",    // Placeholder for service transaction ID -- TODO: Create function to update GiftCardTransactionID
				Valid:  false, // Assuming service transaction ID is not always available
			},
		}

		giftMeta, err := s.store.WithTx(dbTx).CreateGiftcardMetadata(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create giftcard metadata record: %w", err)
		}

		response := TransactionResponse[GiftcardMetadataResponse]{
			ID:              tObj.ID,
			Type:            string(tx.Type),
			Description:     tx.Description,
			TransactionFlow: currFlow,
			Status:          string(Success),
			CreatedAt:       tObj.CreatedAt,
			UpdatedAt:       tObj.UpdatedAt,
			Metadata: &GiftcardMetadataResponse{
				ID:                   giftMeta.ID,
				SourceWallet:         giftMeta.SourceWallet.UUID,
				Rate:                 giftMeta.Rate.String,
				ReceivedAmount:       giftMeta.ReceivedAmount.String,
				SentAmount:           giftMeta.SentAmount.String,
				Fees:                 giftMeta.Fees.String,
				ServiceProvider:      giftMeta.ServiceProvider,
				ServiceTransactionID: giftMeta.ServiceTransactionID.String,
			},
		}

		return &response, nil

	}

	if platform == FiatOutflowTransaction {
		tx, ok := txx.(*FiatTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(tx.Type),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: sql.NullString{String: currFlow, Valid: currFlow != ""},
			Status:          string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		params := db.CreateFiatWithdrawalMetadataParams{
			SourceWallet: uuid.NullUUID{
				UUID:  tx.SourceWalletID,
				Valid: tx.SourceWalletID.URN() != "",
			},
			TransactionID: tObj.ID,
			Rate: sql.NullString{
				String: tx.Rate.String(),
				Valid:  true,
			},
			ReceivedAmount: sql.NullString{
				String: tx.ReceivedAmount.String(),
				Valid:  true,
			},
			SentAmount: sql.NullString{
				String: tx.SentAmount.String(),
				Valid:  true,
			},
			Fees: sql.NullString{
				String: tx.Fees.String(),
				Valid:  true,
			},
			AccountName: sql.NullString{
				String: tx.DestinationAccountName,
				Valid:  true,
			},
			BankCode: sql.NullString{
				String: tx.DestinationAccountBankCode,
				Valid:  true,
			},
			AccountNumber: sql.NullString{
				String: tx.DestinationAccountNumber,
				Valid:  true,
			},
			ServiceProvider: sql.NullString{
				String: providers.Paystack, // Assuming Paystack as the service provider for fiat transactions
				Valid:  true,
			},
			ServiceTransactionID: sql.NullString{
				String: "",    // Placeholder for service transaction ID -- TODO: Create function to update FiatTransactionID
				Valid:  false, // Assuming service transaction ID is not always available
			},
		}

		fiatMeta, err := s.store.WithTx(dbTx).CreateFiatWithdrawalMetadata(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		response := TransactionResponse[FiatWithdrawalMetadataResponse]{
			ID:              tObj.ID,
			Type:            string(tx.Type),
			Description:     tx.Description,
			TransactionFlow: currFlow,
			Status:          string(Success),
			CreatedAt:       tObj.CreatedAt,
			UpdatedAt:       tObj.UpdatedAt,
			Metadata: &FiatWithdrawalMetadataResponse{
				ID:                   fiatMeta.ID,
				SourceWallet:         fiatMeta.SourceWallet.UUID,
				Rate:                 fiatMeta.Rate.String,
				ReceivedAmount:       fiatMeta.ReceivedAmount.String,
				SentAmount:           fiatMeta.SentAmount.String,
				Fees:                 fiatMeta.Fees.String,
				AccountName:          fiatMeta.AccountName.String,
				BankCode:             fiatMeta.BankCode.String,
				AccountNumber:        fiatMeta.AccountNumber.String,
				ServiceProvider:      fiatMeta.ServiceProvider.String,
				ServiceTransactionID: fiatMeta.ServiceTransactionID.String,
			},
		}

		return &response, nil

	}

	if platform == BillOutflowTransaction {
		tx, ok := txx.(*BillTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(tx.Type),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: sql.NullString{String: currFlow, Valid: currFlow != ""},
			Status:          string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create transaction record: %w", err)
		}

		params := db.CreateServiceMetadataParams{
			TransactionID: tObj.ID,
			SourceWallet: uuid.NullUUID{
				UUID:  tx.SourceWalletID,
				Valid: tx.SourceWalletID.URN() != "",
			},
			ServiceProvider: sql.NullString{
				String: providers.VTPass,
				Valid:  true,
			},
			Rate: sql.NullString{
				String: tx.Rate.String(),
				Valid:  true,
			},
			ReceivedAmount: sql.NullString{
				String: tx.ReceivedAmount.String(),
				Valid:  true,
			},
			SentAmount: sql.NullString{
				String: tx.SentAmount.String(),
				Valid:  true,
			},
			Fees: sql.NullString{
				String: tx.Fees.String(),
				Valid:  true,
			},
			ServiceType: string(tx.Type),
			ServiceID: sql.NullString{
				String: tx.ServiceID,
				Valid:  true,
			},
		}

		billMeta, err := s.store.WithTx(dbTx).CreateServiceMetadata(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create bill metadata record: %w", err)
		}

		response := TransactionResponse[BillMetadataResponse]{
			ID:              tObj.ID,
			Type:            string(tx.Type),
			Description:     tx.Description,
			TransactionFlow: currFlow,
			Status:          string(Success),
			CreatedAt:       tObj.CreatedAt,
			UpdatedAt:       tObj.UpdatedAt,
			Metadata: &BillMetadataResponse{
				ID:                   billMeta.ID,
				SourceWallet:         billMeta.SourceWallet.UUID,
				Rate:                 billMeta.Rate.String,
				ReceivedAmount:       billMeta.ReceivedAmount.String,
				SentAmount:           billMeta.SentAmount.String,
				Fees:                 billMeta.Fees.String,
				ServiceProvider:      billMeta.ServiceProvider.String,
				ServiceTransactionID: billMeta.ServiceTransactionID.String,
			},
		}

		return &response, nil
	}

	return nil, fmt.Errorf("cannot decipher platform type: %v", platform)

}

func (s *TransactionService) createLedgerEntries(ctx context.Context, dbTx *sql.Tx, le LedgerEntries) error {

	/// TODO: Figure out how to always have a ledger entry even though destination | source may be off-platform
	/// e.g. CryptoInflow | GiftCard outflow
	debitParams := db.CreateWalletLedgerEntryParams{
		TransactionID: uuid.NullUUID{
			UUID:  le.TransactionID,
			Valid: le.TransactionID.URN() != "",
		},
		WalletID: uuid.NullUUID{
			UUID:  le.Debit.AccountID,
			Valid: le.Debit.AccountID.URN() != "",
		},
		Type:            "debit",
		Amount:          le.Debit.Amount.String(),
		Balance:         le.Debit.Balance.String(),
		SourceType:      string(le.SourceType),
		DestinationType: string(le.DestinationType),
	}

	creditParams := db.CreateWalletLedgerEntryParams{
		TransactionID: uuid.NullUUID{
			UUID:  le.TransactionID,
			Valid: le.TransactionID.URN() != "",
		},
		WalletID: uuid.NullUUID{
			UUID:  le.Credit.AccountID,
			Valid: le.Credit.AccountID.URN() != "",
		},
		Type:            "credit",
		Amount:          le.Credit.Amount.String(),
		Balance:         le.Debit.Balance.String(),
		SourceType:      string(le.SourceType),
		DestinationType: string(le.DestinationType),
	}

	switch le.Platform {
	case WalletTransaction:
		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}

		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, creditParams); err != nil {
			return fmt.Errorf("failed to create credit entry: %w", err)
		}
		return nil
	case GiftCardOutflowTransaction:
		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}
		return nil

	case CryptoInflowTransaction:
		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, creditParams); err != nil {
			return fmt.Errorf("failed to create credit entry: %w", err)
		}
		return nil

	case FiatOutflowTransaction:
		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}
		return nil

	case BillOutflowTransaction:
		if _, err := s.store.WithTx(dbTx).CreateWalletLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("invalid transaction platform: should be Wallet | Crypto | GiftCard | Bill | Fiat")
	}
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

// This function calculates and updates the transaction fees object based on the transaction type.
// It takes the context, a database transaction, the initial sent amount, a pointer to the fees object, and the transaction type as parameters.
// It updates the fees object with the calculated fee amount, adds the fee to the sent amount, and returns the updated sent amount and an error if any occurs during the process.
func (s *TransactionService) addTransactionFeesWithTx(ctx context.Context, dbTX *sql.Tx, sentAmount decimal.Decimal, fees *decimal.Decimal, transactionType string) (decimal.Decimal, error) {
	feeObject, err := s.store.WithTx(dbTX).GetLatestTransactionFee(ctx, transactionType)
	if err != nil {
		if err == sql.ErrNoRows {
			return sentAmount, nil
		}
		return sentAmount, fmt.Errorf("failed to fetch transaction fee (%s): %w", transactionType, err)
	}

	if !feeObject.FeePercentage.Valid && !feeObject.FlatFee.Valid {
		return sentAmount, fmt.Errorf("fee amount is invalid - no fee percentage or flat fee")
	}

	if feeObject.FeePercentage.String != "" {
		feePercentage, err := decimal.NewFromString(feeObject.FeePercentage.String)
		if err != nil {
			return sentAmount, fmt.Errorf("fee amount is invalid - could not convert to decimal")
		}

		if feeObject.MaxFee.Valid {
			maxFee, err := decimal.NewFromString(feeObject.MaxFee.String)
			if err != nil {
				return sentAmount, fmt.Errorf("MAX fee amount is invalid - could not convert to decimal")
			}

			// use maxFee if Fees are greater than sentAmount
			if sentAmount.Mul(feePercentage).GreaterThan(maxFee) {
				*fees = maxFee
				sentAmount = sentAmount.Add(maxFee)
			} else {
				*fees = sentAmount.Mul(feePercentage)
				sentAmount = sentAmount.Add(sentAmount.Mul(feePercentage))
			}
		} else {
			*fees = sentAmount.Mul(feePercentage)
			sentAmount = sentAmount.Add(sentAmount.Mul(feePercentage))
		}
		return sentAmount, nil
	}

	return sentAmount, fmt.Errorf("failed to find fee type")
}

func (s *TransactionService) CreateTransactionFee(ctx context.Context, request CreateTransactionFeeRequest) (db.TransactionFee, error) {

	s.logger.Info("creating transaction fee")

	feeInfo, err := s.store.CreateTransactionFee(ctx, db.CreateTransactionFeeParams{
		TransactionType: request.TransactionType,
		FeePercentage: sql.NullString{
			String: decimal.NewFromFloat(request.FeePercentage).String(),
			Valid:  true,
		},
		MaxFee: sql.NullString{
			String: decimal.NewFromFloat(request.MaxFee).String(),
			Valid:  true,
		},
		Source: "MANUAL", // TODO: May be extended to also take in the ID of the change author for tracking
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("error creating transaction Fee: %v", err))
		return feeInfo, fmt.Errorf("error creating transaction Fee: %v", err)
	}

	return feeInfo, nil
}

func (s *TransactionService) GetTransactionFee(ctx context.Context, transactionType TransactionType) (db.TransactionFee, error) {

	s.logger.Info("creating transaction fee")

	feeInfo, err := s.store.GetLatestTransactionFee(ctx, string(transactionType))
	if err != nil {
		if err == sql.ErrNoRows {
			return feeInfo, nil
		}
		s.logger.Error(fmt.Sprintf("error fetching transaction Fee: %v", err))
		return feeInfo, fmt.Errorf("error fetching transaction Fee: %v", err)
	}

	return feeInfo, nil
}
