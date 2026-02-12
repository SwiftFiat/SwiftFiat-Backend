package transaction

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bills"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/rewards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

type TransactionService struct {
	store          *db.Store
	currencyClient *currency.CurrencyService
	walletClient   *wallet.WalletService
	logger         *logging.Logger
	config         *utils.Config
	notifyr        *service.Notification
	push           *service.PushNotificationService
	streakUpdater  StreakUpdater
	billProvider   *bills.VTPassProvider
	rewardSvc      *rewards.RewardService
	audit          *audit.Service
	redis          *redis.RedisService
}

func NewTransactionService(
	store *db.Store,
	currencyClient *currency.CurrencyService,
	walletClient *wallet.WalletService,
	logger *logging.Logger,
	config *utils.Config,
	notifyr *service.Notification,
	push *service.PushNotificationService,
	streakUpdater StreakUpdater,
	billProvider *bills.VTPassProvider,
	rewardSvc *rewards.RewardService,
	audit *audit.Service,
	redis *redis.RedisService,
) *TransactionService {
	return &TransactionService{
		store:          store,
		currencyClient: currencyClient,
		walletClient:   walletClient,
		logger:         logger,
		config:         config,
		notifyr:        notifyr,
		push:           push,
		streakUpdater:  streakUpdater,
		billProvider:   billProvider,
		rewardSvc:      rewardSvc,
		audit:          audit,
		redis:          redis,
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
	tempObj, err := s.createTransactionRecord(ctx, dbTx, WalletTransaction, &tx, currFlow, string(InPlatform), fromAccount.CustomerID)
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
		},
		Credit: Entry{
			AccountID: tx.ToAccountID,
			Amount:    tx.ReceivedAmount,
		},
		Platform:        WalletTransaction,
		SourceType:      OnPlatform,
		DestinationType: OnPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	newBalance := fromAccount.Balance.Sub(sentAmount)
	if err := s.updateBalance(ctx, dbTx, tx.FromAccountID, newBalance); err != nil {
		return nil, fmt.Errorf("update from account balance: %w", err)
	}

	newBalance = toAccount.Balance.Add(receivedAmount)
	if err := s.updateBalance(ctx, dbTx, tx.ToAccountID, newBalance); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Reward Points for transfer

	s.logger.Info("wallet (swap | transfer) transaction completed successfully", tx)

	return tObj, nil
}

// May return an arbitrary error or an error defined in [transaction_strings]
// Handles CRYPTO -> WALLET transactions
// Crypto inflow transactions are those where a user sends crypto from an external wallet to their SwiftFiat wallet
// The crypto is then converted to fiat and credited to the user's fiat wallet
func (s *TransactionService) CreateCryptoInflowTransaction(ctx context.Context, orderID string, tx CryptoTransaction, prov *providers.ProviderService) (*TransactionResponse[CryptoMetadataResponse], error) {
	s.logger.Info("starting crypto inflow transaction")

	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Check if trail exists for this hash
	trailExists, err := s.store.WithTx(dbTx).CheckCryptoTransactionTrailByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("issue with checking for transactionHash %v", err)
	}

	if trailExists {
		return nil, fmt.Errorf("transaction already recorded, please check transaction hash: %v", tx.SourceHash)
	}

	/// Update amount in transaction object to prevent future problems
	params := db.CreateCryptoTransactionTrailParams{
		AddressID:       tx.DestinationAddress,
		OrderID:         orderID,
		TransactionHash: sql.NullString{String: tx.SourceHash},
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
	// rate, err := s.currencyClient.GetCryptoExchangeRate(ctx, tx.Coin, "USD", prov)
	rate, err := s.currencyClient.GetCryptoExchangeRateFromCryptomus(ctx, tx.Coin, prov)
	if err != nil {
		return nil, currency.NewCurrencyError(err, tx.Coin, "USD")
	}

	// For all other coins, use the main unit directly
	coinAmount := tx.AmountInSatoshis // tx.Amount should be the string from webhook

	amount := coinAmount.Mul(rate)

	walletAddress, err := s.store.WithTx(dbTx).GetCryptomusAddressByOrderID(ctx, orderID) //Todo: this iis not fetching the cuurate uuid
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cryptomus address: %s, err: %v", tx.TransactionID, err)
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
	tempObj, err := s.createTransactionRecord(ctx, dbTx, CryptoInflowTransaction, &tx, currFlow, string(Inflow), walletAddress.CustomerID.Int64)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[CryptoMetadataResponse])

	// Create ledger entries
	// balance, err := decimal.NewFromString(userUSDWallet.Balance.String)
	// if err != nil {
	// 	s.logger.Error("failed to parse the balance string")
	// }
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: tObj.ID,
		Credit: Entry{
			AccountID: userUSDWallet.ID,
			Amount:    amount,
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

	email := service.Plunk{Config: s.config, HttpClient: &http.Client{Timeout: time.Second * 10}}
	tplData := map[string]any{
		"Amount":        amount,
		"Currency":      tx.Coin,
		"TransactionID": tx.TransactionID,
		"Date":          time.Now().Format("2006-01-02 15:04:05"),
	}

	body, err := utils.RenderEmailTemplate("templates/cryptp_transaction.html", tplData)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, err.Error())
	}

	user, err := s.store.GetUserByID(ctx, walletAddress.CustomerID.Int64)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to fetch user by ID: %v, %v", walletAddress.CustomerID.Int64, err))
		return nil, fmt.Errorf("failed to fetch user by ID: %v", walletAddress.CustomerID.Int64)
	}

	subject := "SwiftFiat - Successful Crypto Inflow Transaction"
	err = email.SendEmail(user.Email, subject, body)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to send crypto confirmation email: %v", err))
	}
	s.logger.Info("crypto inflow transaction email sent successfully", user.Email)

	s.notifyr.CreateWithRecipients(ctx, nil, "Succcessful Crypto Transaction", fmt.Sprintf("You have received %s USD on USD wallet", amount.String()), "system", []int64{user.ID})

	s.logger.Info("crypto inflow transaction completed successfully", tx)

	return tObj, nil
}

// CreatePendingCryptoTransaction creates a pending transaction record when crypto is detected but not yet confirmed
// This is called on "confirm_check" webhook status
func (s *TransactionService) CreatePendingCryptoTransaction(
	ctx context.Context,
	orderID string,
	tx CryptoTransaction,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {
	s.logger.Info("Creating pending crypto transaction")

	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Check if trail exists for this hash
	trailExists, err := s.store.WithTx(dbTx).CheckCryptoTransactionTrailByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("issue with checking for orderID %v", err)
	}
	if trailExists {
		return nil, fmt.Errorf("transaction already recorded, please check order id: %v", orderID)
	}

	// Record trail
	params := db.CreateCryptoTransactionTrailParams{
		AddressID:       tx.DestinationAddress,
		OrderID:         orderID,
		TransactionHash: sql.NullString{String: tx.SourceHash},
		Amount: sql.NullString{
			String: tx.AmountInSatoshis.StringFixed(10),
			Valid:  !tx.AmountInSatoshis.IsZero(),
		},
	}
	if _, err = s.store.WithTx(dbTx).CreateCryptoTransactionTrail(ctx, params); err != nil {
		return nil, fmt.Errorf("error creating transaction trail: %v", err)
	}

	// Resolve the Cryptomus address and the destination currency/wallet
	walletAddress, err := s.store.WithTx(dbTx).GetCryptomusAddressByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cryptomus address: %s, err: %v", tx.TransactionID, err)
	}

	// Normalize coin symbol
	coinSym := strings.ToUpper(tx.Coin)

	// Get exchange rate from Cryptomus
	rate, err := s.currencyClient.GetCryptoExchangeRateFromCryptomus(ctx, coinSym, prov)
	if err != nil {
		s.logger.Warnf("Failed to get exchange rate for pending transaction: %v", err)
		// Continue with rate of 0 if we can't get it - will be updated on paid status
		rate = decimal.Zero
	}

	// Calculate amount in USD
	coinAmount := tx.AmountInSatoshis
	estimatedAmount := coinAmount.Mul(rate)

	// Pull destination wallet by currency and lock
	walletParams := db.GetWalletByCurrencyForUpdateParams{
		CustomerID: walletAddress.CustomerID.Int64,
		Currency:   "USD",
	}

	destWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, walletParams)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wallet for customer (%v) and currency (%v)",
			walletParams.CustomerID, walletParams.Currency)
	}

	// Set transaction object values
	tx.DestinationAccount = destWallet.ID
	tx.Rate = rate
	tx.SentAmount = tx.AmountInSatoshis
	tx.ReceivedAmount = estimatedAmount
	tx.Coin = coinSym
	// tx.Status = "pending" // Explicitly set pending status

	// Create transaction record with PENDING status
	currFlow := coinSym + " to USD"
	tempObj, err := s.createPendingTransactionRecord(ctx, dbTx, "crypto", &tx, currFlow, string(Inflow), walletAddress.CustomerID.Int64, orderID)
	if err != nil {
		return nil, fmt.Errorf("create pending transaction record: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	response := TransactionResponse[CryptoMetadataResponse]{
		ID:              tempObj.ID,
		Type:            tempObj.Type,
		Status:          tempObj.Status,
		TransactionFlow: tempObj.TransactionFlow,
		Description:     tempObj.Description.String,
		CreatedAt:       tempObj.CreatedAt,
		UpdatedAt:       tempObj.UpdatedAt,
	}

	return &response, nil
}

// createPendingTransactionRecord creates a transaction record with pending status
func (s *TransactionService) createPendingTransactionRecord(
	ctx context.Context,
	dbTx *sql.Tx,
	transactionType TransactionType,
	tx interface{},
	currFlow string,
	flow string,
	userID int64,
	orderID string,
) (*db.Transaction, error) {
	// This is similar to createTransactionRecord but explicitly sets status to "pending"
	cryptoTx := tx.(*CryptoTransaction)

	// Calculate amount in USD for the transaction table
	amountUSD := cryptoTx.ReceivedAmount

	// Create base transaction with pending status
	baseParams := db.CreateTransactionParams{
		UserID:          userID,
		Type:            string(transactionType),
		Description:     sql.NullString{String: cryptoTx.Description, Valid: true},
		TransactionFlow: flow,
		Amount:          cryptoTx.AmountInSatoshis.String(),
		Currency:        cryptoTx.Coin,
		AmountUsd:       amountUSD.String(),
		Status:          "pending", // PENDING status for confirm_check
	}

	baseTx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, baseParams)
	if err != nil {
		return nil, fmt.Errorf("create base transaction: %w", err)
	}

	// Create crypto metadata record
	metadataParams := db.CreateCryptoMetadataParams{
		DestinationWallet:    uuid.NullUUID{UUID: cryptoTx.DestinationAccount, Valid: true},
		TransactionID:        baseTx.ID,
		Coin:                 cryptoTx.Coin,
		SourceHash:           sql.NullString{String: cryptoTx.SourceHash, Valid: true},
		Rate:                 sql.NullString{String: cryptoTx.Rate.String(), Valid: true},
		Fees:                 sql.NullString{String: cryptoTx.Fees.String(), Valid: true},
		ReceivedAmount:       sql.NullString{String: cryptoTx.ReceivedAmount.String(), Valid: true},
		SentAmount:           sql.NullString{String: cryptoTx.AmountInSatoshis.String(), Valid: true},
		ServiceProvider:      "cryptomus",
		ServiceTransactionID: sql.NullString{String: "", Valid: false},
		OrderID:              orderID,
	}

	_, err = s.store.WithTx(dbTx).CreateCryptoMetadata(ctx, metadataParams)
	if err != nil {
		return nil, fmt.Errorf("create crypto metadata: %w", err)
	}

	return &baseTx, nil
}

// UpdateCryptoTransactionToPaid updates a pending crypto transaction to successful and credits the wallet
// This is called when the "paid" webhook is received
func (s *TransactionService) UpdateCryptoTransactionToPaid(
	ctx context.Context,
	dbTx *sql.Tx,
	transactionID uuid.UUID,
	finalRate decimal.Decimal,
	finalReceivedAmount decimal.Decimal,
) error {
	s.logger.Info("Updating crypto transaction to paid status", "transaction_id", transactionID)

	qtx := s.store.WithTx(dbTx)

	// Get the existing transaction
	existingTx, err := qtx.GetTransactionByIDForUpdate(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("failed to get transaction for update: %w", err)
	}

	// Verify it's in pending status
	if existingTx.Status != "pending" {
		return fmt.Errorf("transaction is not in pending status, current status: %s", existingTx.Status)
	}

	// Update transaction status to successful
	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     transactionID,
		Status: string(Success),
	})
	if err != nil {
		return fmt.Errorf("failed to update transaction status: %w", err)
	}

	// Get crypto metadata to find destination wallet
	// Note: You'll need to create a query to get crypto metadata by transaction_id
	// For now, we'll parse from the existing transaction flow

	// Get destination wallet
	metadata, err := qtx.GetCryptoMetadataByTransactionID(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("failed to get crypto metadata: %w", err)
	}

	// Get the wallet for update
	wallet, err := qtx.GetWalletForUpdate(ctx, metadata.DestinationWallet.UUID)
	if err != nil {
		return fmt.Errorf("failed to get destination wallet: %w", err)
	}

	// Parse current balance
	// currentBalance, err := decimal.NewFromString(wallet.Balance.String)
	// if err != nil {
	// 	return fmt.Errorf("failed to parse wallet balance: %w", err)
	// }

	// Create ledger entry for the credit
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: transactionID,
		Credit: Entry{
			AccountID: wallet.ID,
			Amount:    finalReceivedAmount,
			// Balance:   currentBalance,
		},
		Platform:        CryptoInflowTransaction,
		SourceType:      OffPlatform,
		DestinationType: OnPlatform,
	}); err != nil {
		return fmt.Errorf("create ledger entries: %w", err)
	}

	// Update wallet balance
	if err := s.updateBalance(ctx, dbTx, wallet.ID, finalReceivedAmount); err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	s.logger.Info("Successfully updated crypto transaction to paid",
		"transaction_id", transactionID,
		"amount_credited", finalReceivedAmount.String())

	return nil
}

// Enhanced CreateAllCryptoINflowTXs - now handles both new transactions and updates pending ones
func (s *TransactionService) CreateAllCryptoINflowTXs(
	ctx context.Context,
	orderID string,
	tx CryptoTransaction,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {
	s.logger.Info("Processing crypto inflow transaction (paid status)")

	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Check if a pending transaction already exists for this hash
	qtx := s.store.WithTx(dbTx)

	// First check if transaction trail exists (it should if confirm_check was processed)
	trailExists, err := qtx.CheckCryptoTransactionTrailByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("issue with checking for orderid: %v", err)
	}

	var tObj *TransactionResponse[CryptoMetadataResponse]

	if trailExists {
		// A pending transaction likely exists - try to find and update it
		s.logger.Info("Transaction trail exists, looking for pending transaction to update")

		// Get the pending transaction by source hash
		// Note: You'll need to add this query: GetPendingTransactionBySourceHash
		pendingTx, err := qtx.GetPendingCryptoTransactionByOrderID(ctx, orderID)
		if err == nil {
			// Found pending transaction - update it to successful
			s.logger.Info("Found pending transaction, updating to successful", "transaction_id", pendingTx.ID)

			// Calculate final amounts with current rate
			walletAddress, err := qtx.GetCryptomusAddressByOrderID(ctx, orderID)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch cryptomus address: %s, err: %v", tx.TransactionID, err)
			}

			coinSym := strings.ToUpper(tx.Coin)
			rate, err := s.currencyClient.GetCryptoExchangeRateFromCryptomus(ctx, coinSym, prov)
			if err != nil {
				return nil, fmt.Errorf("failed to get exchange rate: %w", err)
			}

			coinAmount := tx.AmountInSatoshis
			finalAmount := coinAmount.Mul(rate)

			// Update the pending transaction to successful and credit the wallet
			err = s.UpdateCryptoTransactionToPaid(ctx, dbTx, pendingTx.ID, rate, finalAmount)
			if err != nil {
				return nil, fmt.Errorf("failed to update pending transaction: %w", err)
			}

			// Build response from updated transaction
			updatedTx, _ := qtx.GetTransactionByID(ctx, pendingTx.ID)
			tObj = buildCryptoTransactionResponse(updatedTx)

			// Send success notifications
			user, err := qtx.GetUserByID(ctx, walletAddress.CustomerID.Int64)
			if err == nil {
				s.sendCryptoSuccessNotifications(ctx, user, finalAmount, coinSym, tx.TransactionID)
			}

		} else {
			// Pending transaction not found, but trail exists - create new successful transaction
			s.logger.Warn("Trail exists but no pending transaction found, creating new successful transaction")
			tObj, err = s.createNewSuccessfulCryptoTransaction(ctx, dbTx, orderID, tx, prov)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// No trail exists - this is the first webhook (probably directly to "paid" status)
		// Create trail and process as normal
		s.logger.Info("No existing trail, creating new successful transaction from scratch")

		params := db.CreateCryptoTransactionTrailParams{
			AddressID:       tx.DestinationAddress,
			OrderID:         orderID,
			TransactionHash: sql.NullString{String: tx.SourceHash},
			Amount: sql.NullString{
				String: tx.AmountInSatoshis.StringFixed(10),
				Valid:  !tx.AmountInSatoshis.IsZero(),
			},
		}
		if _, err = qtx.CreateCryptoTransactionTrail(ctx, params); err != nil {
			return nil, fmt.Errorf("error creating transaction trail: %v", err)
		}

		tObj, err = s.createNewSuccessfulCryptoTransaction(ctx, dbTx, orderID, tx, prov)
		if err != nil {
			return nil, err
		}
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("Crypto inflow transaction completed successfully")
	return tObj, nil
}

// createNewSuccessfulCryptoTransaction creates a new crypto transaction with successful status
// This is the original logic from CreateAllCryptoINflowTXs but with status set to successful
func (s *TransactionService) createNewSuccessfulCryptoTransaction(
	ctx context.Context,
	dbTx *sql.Tx,
	orderID string,
	tx CryptoTransaction,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {

	qtx := s.store.WithTx(dbTx)

	// Resolve the Cryptomus address
	walletAddress, err := qtx.GetCryptomusAddressByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cryptomus address: %s, err: %v", tx.TransactionID, err)
	}

	// Get user to check rapid ramp status
	user, err := qtx.GetUserByID(ctx, walletAddress.CustomerID.Int64)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	// Normalize coin symbol
	coinSym := strings.ToUpper(tx.Coin)
	coinAmount := tx.AmountInSatoshis

	// Check if rapid ramp is enabled
	if user.IsRapidRampOn {
		s.logger.Info(fmt.Sprintf("Rapid ramp enabled for user %d, processing conversion and payout", user.ID))

		tObj, err := s.processRapidRampInflow(ctx, dbTx, tx, coinAmount, coinSym, walletAddress.CustomerID.Int64, prov)
		if err != nil {
			return nil, fmt.Errorf("failed to process rapid ramp: %w", err)
		}

		// Send notification
		s.notifyr.CreateWithRecipients(ctx, nil, "Crypto Converted & Sent to Bank",
			fmt.Sprintf("Your %s %s has been converted to NGN and sent to your bank account",
				coinAmount.String(), coinSym), "system", []int64{user.ID})

		return tObj, nil
	}

	// Regular processing - convert to USD
	rate, err := s.currencyClient.GetCryptoExchangeRateFromCryptomus(ctx, coinSym, prov)
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange rate: %w", err)
	}

	destCurrency := "USD"
	amount := coinAmount.Mul(rate)
	// currFlow := coinSym + " to USD"

	// Get destination wallet
	walletParams := db.GetWalletByCurrencyForUpdateParams{
		CustomerID: walletAddress.CustomerID.Int64,
		Currency:   destCurrency,
	}
	destWallet, err := qtx.GetWalletByCurrencyForUpdate(ctx, walletParams)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wallet for customer (%v) and currency (%v)",
			walletParams.CustomerID, walletParams.Currency)
	}

	// Populate transaction object
	tx.DestinationAccount = destWallet.ID
	tx.Rate = rate
	tx.SentAmount = tx.AmountInSatoshis
	tx.ReceivedAmount = amount
	tx.Coin = coinSym

	idempotency_key := "crypto_inflow_" + tx.TransactionID.String()
	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		Type:            string(CryptoInflowTransaction),
		Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
		TransactionFlow: string(Inflow),
		Status:          string(Pending),
		AmountUsd:       amount.String(),
		Amount:          amount.String(),
		Currency:        "USD",
		IdempotencyKey:  idempotency_key, // TODO: generate a better idempotency key
		UserID:          walletAddress.CustomerID.Int64,
		Direction:       string(Credit),
		TFrom:           "Cryptomus",
		TTo:             string(Wallet),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create pending transaction: %w", err)
	}

	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: txx.ID,
		Credit: Entry{
			AccountID: destWallet.ID,
			Amount:    amount,
		},
		Platform:        CryptoInflowTransaction,
		SourceType:      OffPlatform,
		DestinationType: OnPlatform,
		idempotency_key: idempotency_key,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update wallet balance
	if _, err := qtx.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
		ID:      destWallet.ID,
		Balance: sql.NullString{String: amount.String(), Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("update wallet balance: %w", err)
	}

	// update transaction record with SUCCESSFUL status
	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     txx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update transaction status: %w", err)
	}

	_, err = qtx.CreateCryptoMetadata(ctx, db.CreateCryptoMetadataParams{
		DestinationWallet: uuid.NullUUID{UUID: destWallet.ID, Valid: true},
		TransactionID:     txx.ID,
		Coin:              coinSym,
		SourceHash:        sql.NullString{String: tx.SourceHash, Valid: true},
		// Rate: rate,
		// Fees: tx.Fees, //todo: fix fees
		ReceivedAmount:       sql.NullString{String: tx.ReceivedAmount.String(), Valid: true},
		SentAmount:           sql.NullString{String: tx.SentAmount.String(), Valid: true},
		ServiceProvider:      "Cryptomus",
		OrderID:              orderID,
		ServiceTransactionID: sql.NullString{String: tx.TransactionID.String(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("create crypto metadata: %w", err)
	}

	// Send success notifications
	s.sendCryptoSuccessNotifications(ctx, user, amount, destCurrency, tx.TransactionID)

	// Build and return response from the base transaction
	resp := &TransactionResponse[CryptoMetadataResponse]{
		ID:              txx.ID,
		Type:            txx.Type,
		Description:     txx.Description.String,
		TransactionFlow: txx.TransactionFlow,
		Status:          txx.Status,
		CreatedAt:       txx.CreatedAt,
		UpdatedAt:       txx.UpdatedAt,
		// Metadata can be populated later if needed
	}

	// TODO: This referral bonus logic is not ideal here - we should ideally have a more robust system for handling referrals and bonuses that is decoupled from the transaction processing. This is just a quick implementation to ensure we don't miss out on referral bonuses for crypto inflows.
	// Check if this is users first conversion and disburse referral bonus to user's referrer
	if !user.HasCompletedFirstConversion.Bool {
		referrerID, referralBonus, err := CheckFirstConersionAndDisburseReferralBonus(ctx, s.store, dbTx, user.ID, txx.ID)
		if err != nil {
			s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to disburse referral bonus: %v", err))
			// Not returning error here because we don't want to fail the whole transaction if referral bonus disbursement fails. We can always try to disburse the bonus later or manually if needed.
		}
		// TODO: use something relaible for notifications
		if referrerID != nil && referralBonus != nil {
			ctxBg := context.Background()
			go func() {
				s.notifyr.CreateWithRecipients(ctxBg, nil, "Referral Bonus Credit", fmt.Sprintf("You have received a referral bonus of %s", referralBonus.String()), "system", []int64{*referrerID})
				s.push.ReferralBonusEarned(ctxBg, *referrerID, referralBonus.String())
			}()
		}
	}

	// Referer earns precentage of amount converted
	s.logger.Info("starting conversion bonus")
	refererID, conversionBonus, err := CreditReferrerForConversion(ctx, s.store, dbTx, user.ID, amount)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to credit referrer for conversion: %v", err))
		// Again, not returning error here to avoid failing the transaction. We can handle this asynchronously or manually if needed.
	}
	s.logger.Info(fmt.Sprintf("Conversion bonus credit process completed. RefererID: %v, Bonus Amount: %v", refererID, conversionBonus))
	if refererID != nil && conversionBonus != nil {
		ctxBg := context.Background()
		go func() {
			s.notifyr.CreateWithRecipients(ctxBg, nil, "Conversion Bonus Credit", fmt.Sprintf("You have received a conversion bonus of %s", conversionBonus.String()), "system", []int64{*refererID})
			s.push.ConversionBonusEarned(ctxBg, *refererID, conversionBonus.String())
		}()
	}

	return resp, nil
}

// sendCryptoSuccessNotifications sends email and in-app notifications for successful crypto transaction
func (s *TransactionService) sendCryptoSuccessNotifications(
	ctx context.Context,
	user db.User,
	amount decimal.Decimal,
	currency string,
	transactionID uuid.UUID,
) {
	// Send email
	email := service.Plunk{Config: s.config, HttpClient: &http.Client{Timeout: time.Second * 10}}
	tplData := map[string]any{
		"Amount":        amount,
		"Currency":      currency,
		"TransactionID": transactionID,
		"Date":          time.Now().Format("2006-01-02 15:04:05"),
	}

	body, err := utils.RenderEmailTemplate("templates/cryptp_transaction.html", tplData)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, err.Error())
	} else {
		subject := "SwiftFiat - Successful Crypto Inflow Transaction"
		if err = email.SendEmail(user.Email, subject, body); err != nil {
			s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to send crypto confirmation email: %v", err))
		} else {
			s.logger.Info("Crypto inflow transaction email sent successfully", user.Email)
		}
	}

	// Send in-app notification
	s.notifyr.CreateWithRecipients(ctx, nil, "Wallet Credit Alert",
		fmt.Sprintf("You have received %.2f %s on %s wallet", amount.InexactFloat64(), currency, currency),
		"system", []int64{user.ID})
}

// Helper function to build response from database transaction
func buildCryptoTransactionResponse(tx db.GetTransactionByIDRow) *TransactionResponse[CryptoMetadataResponse] {
	// You'll need to implement this based on your exact structures
	// This is a placeholder showing the pattern
	return &TransactionResponse[CryptoMetadataResponse]{
		ID:              tx.ID,
		Type:            tx.Type,
		Description:     tx.Description.String,
		TransactionFlow: tx.TransactionFlow,
		Status:          tx.Status,
		CreatedAt:       tx.CreatedAt,
		UpdatedAt:       tx.UpdatedAt,
		// Metadata would need to be populated from crypto_transaction_metadata table
	}
}

func (s *TransactionService) processRapidRampInflow(
	ctx context.Context,
	dbTx *sql.Tx,
	tx CryptoTransaction,
	coinAmount decimal.Decimal,
	coinSym string,
	userID int64,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {
	// Get default bank account
	bankAccount, err := s.store.WithTx(dbTx).GetDefaultBankAccount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get default bank account [processRapidRampInflow]: %w", err)
	}

	if !bankAccount.IsVerified {
		return nil, fmt.Errorf("bank account is not verified")
	}

	// Get Cryptomus provider for conversion rate
	cryptomusProvider, err := s.getCryptomusProvider(prov)
	if err != nil {
		return nil, fmt.Errorf("failed to get cryptomus provider: %w", err)
	}

	// Get crypto provider for conversion rate
	usdRateStr, err := cryptomusProvider.GetUSDRate(coinSym)
	if err != nil {
		return nil, fmt.Errorf("failed to get USD rate: %w", err)
	}

	cryptoToUSD, err := decimal.NewFromString(usdRateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse USD rate: %w", err)
	}

	// USD to NGN rate (using same logic as rapid_ramp service)
	usdToNGN := decimal.NewFromFloat(1550.0) // TODO: Use live API rate in production
	cryptoToNGN := cryptoToUSD.Mul(usdToNGN)

	// Calculate fiat amount in NGN
	fiatAmount := coinAmount.Mul(cryptoToNGN)

	// Calculate fees (same as rapid_ramp service)
	conversionFee := fiatAmount.Mul(decimal.NewFromFloat(0.00)) // TODO: Use VIP rates
	platformFee := fiatAmount.Mul(decimal.NewFromFloat(0.000))
	networkFee := decimal.NewFromFloat(0)
	totalFees := conversionFee.Add(platformFee).Add(networkFee)
	netAmount := fiatAmount.Sub(totalFees)

	// Get Paystack provider
	paystackProvider, err := s.getPaystackProvider(prov)
	if err != nil {
		return nil, fmt.Errorf("failed to get paystack provider: %w", err)
	}

	// Create transfer recipient
	recipient, err := paystackProvider.CreateTransferRecipient(
		bankAccount.AccountNumber,
		bankAccount.BankCode,
		bankAccount.AccountName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create recipient [processRapidRampInflow]: %w", err)
	}

	// Convert net amount to kobo (NGN smallest unit)
	amountInKobo := netAmount.Mul(decimal.NewFromInt(100)).IntPart()

	// Initiate transfer
	transfer, err := paystackProvider.MakeTransfer(
		recipient.RecipientCode,
		amountInKobo,
		bankAccount.AccountName,
	)
	if err != nil {
		// TODO: log critical error
		// TODO: use db tx
		// if an error happens with paystack, deposit to user NGN wallet
		NgnWallet, ierr := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
			CustomerID: userID,
			Currency:   "NGN",
		})
		if ierr != nil {
			return nil, fmt.Errorf("failed to get user NGN wallet [processRapidRampInflow]: %v", ierr)
		}

		_, err := s.store.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
			Amount: sql.NullString{String: fiatAmount.String(), Valid: true},
			ID:     NgnWallet.ID,
		})

		// TODO: increment conversiion count, add tx to db, send notifs and emails

		return nil, fmt.Errorf("failed to initiate transfer with paystack [processRapidRampInflow]: %w", err)
	}

	// Update transaction object for record keeping
	tx.Rate = cryptoToNGN
	tx.SentAmount = coinAmount
	tx.ReceivedAmount = netAmount
	tx.Coin = coinSym

	// Create transaction record with rapid ramp flow
	currFlow := fmt.Sprintf("%s to NGN (Rapid Ramp)", coinSym)
	tempObj, err := s.createTransactionRecord(ctx, dbTx, CryptoInflowTransaction, &tx, currFlow, string(Inflow), userID)
	if err != nil {
		return nil, fmt.Errorf("create transaction record: %w", err)
	}
	tObj := tempObj.(*TransactionResponse[CryptoMetadataResponse])

	// Log the rapid ramp transaction details
	s.logger.Info(fmt.Sprintf("Rapid ramp transaction: %s %s -> %s NGN (net: %s NGN, fees: %s NGN, transfer ref: %s)",
		coinAmount.String(), coinSym, fiatAmount.String(), netAmount.String(), totalFees.String(), transfer.Reference))

	return tObj, nil
}

// Helper function to get Cryptomus provider
func (s *TransactionService) getCryptomusProvider(prov *providers.ProviderService) (*cryptocurrency.CryptomusProvider, error) {
	provider, exists := prov.GetProvider(providers.Cryptomus)
	if !exists {
		return nil, fmt.Errorf("cryptomus provider not available")
	}

	cryptomusProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		return nil, fmt.Errorf("invalid cryptomus provider type")
	}

	return cryptomusProvider, nil
}

// Helper function to get Paystack provider
func (s *TransactionService) getPaystackProvider(prov *providers.ProviderService) (*fiat.PaystackProvider, error) {
	provider, exists := prov.GetProvider(providers.Paystack)
	if !exists {
		return nil, fmt.Errorf("paystack provider not available")
	}

	paystackProvider, ok := provider.(*fiat.PaystackProvider)
	if !ok {
		return nil, fmt.Errorf("invalid paystack provider type")
	}

	return paystackProvider, nil
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
	tx.SentAmount = sentAmount

	// Create transaction record
	tempObj, err := s.createTransactionRecord(ctx, dbTx, GiftCardOutflowTransaction, &tx, currFlow, string(Outflow), fromAccount.CustomerID)
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
			// Balance:   tx.WalletBalance,
		},
		Platform:        GiftCardOutflowTransaction,
		SourceType:      OnPlatform,
		DestinationType: OffPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	newBalance := tx.WalletBalance.Sub(sentAmount)
	if err := s.updateBalance(ctx, dbTx, tx.SourceWalletID, newBalance); err != nil {
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
	tempObj, err := s.createTransactionRecord(ctx, dbTx, FiatOutflowTransaction, &tx, currFlow, string(Outflow), fromAccount.CustomerID)
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
			// Balance:   tx.WalletBalance,
		},
		Platform:        FiatOutflowTransaction,
		SourceType:      OnPlatform,
		DestinationType: OffPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	newBalance := tx.WalletBalance.Sub(sentAmount)
	if err := s.updateBalance(ctx, dbTx, tx.SourceWalletID, newBalance); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	return tObj, nil
}

// / May return an arbitrary error or an error defined in [transaction_strings]
func (s *TransactionService) CreateBillPurchaseTransactionWithTx(ctx context.Context, key string, dbTx *sql.Tx, user *db.User, tx BillTransaction) (*db.ServicesMetadatum, error) {
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
	amountUsd, err := utils.ConvertToUSD(ctx, tx.SentAmount, tx.WalletCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	txx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          fromAccount.CustomerID,
		Type:            string(tx.Type),
		Description:     sql.NullString{String: fmt.Sprintf("%s purchase", tx.Type), Valid: true},
		TransactionFlow: string(Outflow),
		Amount:          tx.SentAmount.String(),
		AmountUsd:       amountUsd.String(),
		IdempotencyKey:  key,
		Currency:        tx.WalletCurrency,
		Direction:       string(Debit),
		TFrom:           string(Wallet),
		TTo:             string(OffPlatform),
		Status:          string(Pending),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction record: %w", err)
	}

	billTx, err := s.store.WithTx(dbTx).CreateServiceMetadata(ctx, db.CreateServiceMetadataParams{
		SourceWallet:         uuid.NullUUID{UUID: tx.SourceWalletID, Valid: true},
		TransactionID:        txx.ID,
		ServiceID:            sql.NullString{String: tx.ServiceID, Valid: true},
		Rate:                 sql.NullString{String: tx.Rate.String(), Valid: true},
		ReceivedAmount:       sql.NullString{String: tx.ReceivedAmount.String(), Valid: true},
		SentAmount:           sql.NullString{String: tx.SentAmount.String(), Valid: true},
		ServiceType:          string(tx.Type),
		ServiceProvider:      sql.NullString{String: "VTPass", Valid: true},
		ServiceTransactionID: sql.NullString{String: tx.ServiceID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bill transaction metadata: %w", err)
	}

	// Create ledger entries
	if err := s.createLedgerEntries(ctx, dbTx, LedgerEntries{
		TransactionID: txx.ID,
		Debit: Entry{
			AccountID: tx.SourceWalletID,
			Amount:    sentAmount,
			// Balance:   tx.WalletBalance,
		},
		Platform:        BillOutflowTransaction,
		SourceType:      OnPlatform,
		DestinationType: OffPlatform,
	}); err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}

	// Update account balances
	newBalance := tx.WalletBalance.Sub(sentAmount)
	if err := s.updateBalance(ctx, dbTx, tx.SourceWalletID, newBalance); err != nil {
		return nil, fmt.Errorf("update to account balance: %w", err)
	}

	return &billTx, nil
}

func (s *TransactionService) createTransactionRecord(ctx context.Context, dbTx *sql.Tx, platform TransactionPlatform, txx interface{}, currFlow, transactionFlow string, userID int64) (interface{}, error) {

	if platform == WalletTransaction {
		tx, ok := txx.(*IntraTransaction)
		if !ok {
			return nil, fmt.Errorf("failed to parse transaction into TransactionObject")
		}

		amountUsd, err := utils.ConvertToUSD(ctx, tx.SentAmount, tx.Currency)
		if err != nil {
			return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(Transfer),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: string(transactionFlow),
			Status:          string(Success),
			AmountUsd:       amountUsd.String(),
			Amount:          tx.SentAmount.String(),
			Currency:        tx.Currency,
			UserID:          userID,
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
			TransactionFlow: string(currFlow),
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

		amountUsd, err := utils.ConvertToUSD(ctx, tx.SentAmount, tx.Coin)
		if err != nil {
			return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
		}

		s.logger.Info("=================== crypto inflow transaction record creating...")
		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(CryptoInflowTransaction),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: string(Inflow),
			Status:          string(Success),
			AmountUsd:       amountUsd.String(),
			Amount:          tx.SentAmount.String(),
			Currency:        tx.Coin,
			UserID:          userID,
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
			ServiceProvider: providers.Cryptomus,
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
			Type:            string(Transfer),
			Description:     tx.Description,
			TransactionFlow: string(Outflow),
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

		amountUsd, err := utils.ConvertToUSD(ctx, tx.SentAmount, tx.WalletCurrency)
		if err != nil {
			return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(GiftCard),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: string(Outflow),
			Status:          string(Success),
			AmountUsd:       amountUsd.String(),
			Amount:          tx.SentAmount.String(),
			Currency:        tx.WalletCurrency,
			UserID:          userID,
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
			Type:            string(Transfer),
			Description:     tx.Description,
			TransactionFlow: string(Outflow),
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

		amountUsd, err := utils.ConvertToUSD(ctx, tx.SentAmount, tx.WalletCurrency)
		if err != nil {
			return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(Transfer),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: string(Outflow),
			Status:          string(Success),
			AmountUsd:       amountUsd.String(),
			Amount:          tx.SentAmount.String(),
			Currency:        "NGN",
			UserID:          userID,
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
			Type:            string(Transfer),
			Description:     tx.Description,
			TransactionFlow: string(Outflow),
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

		amountUsd, err := utils.ConvertToUSD(ctx, tx.SentAmount, tx.WalletCurrency)
		if err != nil {
			return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
		}

		tObj, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
			Type:            string(tx.Type),
			Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
			TransactionFlow: string(Outflow),
			Status:          string(Success),
			AmountUsd:       amountUsd.String(),
			Amount:          tx.SentAmount.String(),
			Currency:        "NGN",
			UserID:          userID,
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
			Type:            string(Transfer),
			Description:     tx.Description,
			TransactionFlow: string(Outflow),
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
	debitParams := db.InsertLedgerEntryParams{
		TransactionID: uuid.NullUUID{
			UUID:  le.TransactionID,
			Valid: le.TransactionID.URN() != "",
		},
		WalletID: uuid.NullUUID{
			UUID:  le.Debit.AccountID,
			Valid: le.Debit.AccountID.URN() != "",
		},
		EntryType:       "debit",
		Amount:          le.Debit.Amount.String(),
		SourceType:      string(le.SourceType),
		DestinationType: string(le.DestinationType),
		// IdempotencyKey:  le.idempotency_key,
	}

	creditParams := db.InsertLedgerEntryParams{
		TransactionID: uuid.NullUUID{
			UUID:  le.TransactionID,
			Valid: le.TransactionID.URN() != "",
		},
		WalletID: uuid.NullUUID{
			UUID:  le.Credit.AccountID,
			Valid: le.Credit.AccountID.URN() != "",
		},
		EntryType:       "credit",
		Amount:          le.Credit.Amount.String(),
		SourceType:      string(le.SourceType),
		DestinationType: string(le.DestinationType),
		// IdempotencyKey:  le.idempotency_key,
	}

	switch le.Platform {
	case WalletTransaction:
		if _, err := s.store.WithTx(dbTx).InsertLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}

		if _, err := s.store.WithTx(dbTx).InsertLedgerEntry(ctx, creditParams); err != nil {
			return fmt.Errorf("failed to create credit entry: %w", err)
		}
		return nil
	case GiftCardOutflowTransaction:
		if _, err := s.store.WithTx(dbTx).InsertLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}
		return nil

	case CryptoInflowTransaction:
		if _, err := s.store.WithTx(dbTx).InsertLedgerEntry(ctx, creditParams); err != nil {
			return fmt.Errorf("failed to create credit entry: %w", err)
		}
		return nil

	case FiatOutflowTransaction:
		if _, err := s.store.WithTx(dbTx).InsertLedgerEntry(ctx, debitParams); err != nil {
			return fmt.Errorf("failed to create debit entry: %w", err)
		}
		return nil

	case BillOutflowTransaction:
		if _, err := s.store.WithTx(dbTx).InsertLedgerEntry(ctx, debitParams); err != nil {
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

func (s *TransactionService) ListAllTransactions(ctx context.Context) ([]db.ListAllTransactionsWithUsersRow, error) {
	transactions, err := s.store.ListAllTransactionsWithUsers(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("error fetching all transactions: %v", err))
		return nil, fmt.Errorf("error fetching all transactions: %v", err)
	}

	return transactions, nil
}

// ===============================================
// REWARDS
// ===============================================

// ProcessRewardRedemption handles reward point redemption for bill payments
func (s *TransactionService) ProcessRewardRedemption(
	ctx context.Context,
	dbTx *sql.Tx,
	user *db.User,
	pointsToRedeem decimal.Decimal,
	originalAmount decimal.Decimal,
) (finalAmount decimal.Decimal, redeemed bool, err error) {
	// if no points to redeem, return original amount
	if pointsToRedeem.LessThanOrEqual(decimal.NewFromInt(0)) {
		return originalAmount, false, nil
	}

	qtx := s.store.WithTx(dbTx)

	// Fetch user's reward balance
	balance, err := qtx.GetUserRewardBalance(ctx, user.ID)
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("failed to fetch user reward balance: %w", err)
	}

	balanceDecimal, err := decimal.NewFromString(balance.RewardBalance)
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("invalid reward balance format: %w", err)
	}

	// Validate: User has sufficient balance
	if pointsToRedeem.GreaterThan(balanceDecimal) {
		return originalAmount, false, fmt.Errorf(
			"insufficient reward balance. Available: %2.f points, Requested: %2.f points",
			balanceDecimal.InexactFloat64(), pointsToRedeem.InexactFloat64(),
		)
	}

	// Validate: Points don't exceed bill amount
	if pointsToRedeem.GreaterThan(originalAmount) {
		return originalAmount, false, fmt.Errorf(
			"cannot redeem more points (₦%d) than bill amount (₦%d)",
			pointsToRedeem, originalAmount,
		)
	}

	// calculate final amount after discount
	finalAmount = originalAmount.Sub(pointsToRedeem)

	s.logger.Info(fmt.Sprintf("Reward redemption validated: Original=₦%d, Points=₦%d, Final=₦%d",
		originalAmount, pointsToRedeem, finalAmount))

	return finalAmount, true, nil
}

// CompleteRewardRedemption completes the reward redemption after bill payment succeeds
func (s *TransactionService) CompleteRewardRedemption(
	ctx context.Context,
	dbTx *sql.Tx,
	userID int32,
	transactionID uuid.UUID,
	pointsRedeemed decimal.Decimal,
	originalAmount decimal.Decimal,
	finalAmount decimal.Decimal,
	serviceType string,
	serviceProvider string,
) error {
	qtx := s.store.WithTx(dbTx)

	// Redeem Points, deduct from balance
	description := fmt.Sprintf("Redeemed ₦%s points for %s payment via %s",
		pointsRedeemed.String(), serviceType, serviceProvider)

	rewardTx, err := qtx.RedeemRewardPointsSimple(ctx, db.RedeemRewardPointsSimpleParams{
		UserID:            int64(userID),
		PointsAmount:      pointsRedeemed.String(),
		TransactionID:     uuid.NullUUID{UUID: transactionID, Valid: true},
		TransactionAmount: sql.NullString{String: originalAmount.String(), Valid: true},
		Description:       sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to complete reward redemption: %w", err)
	}

	// Create detailed redemption record
	_, err = qtx.CreateRewardRedemption(ctx, db.CreateRewardRedemptionParams{
		RewardTransactionID:      rewardTx.ID,
		UserID:                   userID,
		PointsRedeemed:           pointsRedeemed.String(),
		BillPaymentTransactionID: transactionID,
		DiscountAmount:           pointsRedeemed.String(), // same as points redeemed
		OriginalBillAmount:       originalAmount.String(),
		FinalAmountPaid:          finalAmount.String(),
		ServiceType:              sql.NullString{String: serviceType, Valid: true},
		ServiceProvider:          sql.NullString{String: serviceProvider, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to create reward redemption record: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Reward redemption completed: User=%d, Points=₦%d, TX=%d",
		userID, pointsRedeemed, transactionID))

	return nil
}

func (s *TransactionService) AwardRewardPoints(
	ctx context.Context,
	dbTx *sql.Tx,
	userID int32,
	transactionID uuid.UUID,
	paidAmount decimal.Decimal,
	serviceType string,
	transactionType string,
) (pointsEarned decimal.Decimal, err error) {
	qtx := s.store.WithTx(dbTx)

	// Get active reward configuration
	config, err := qtx.GetActiveRewardConfiguration(ctx, transactionType)
	if err != nil {
		if err == sql.ErrNoRows {
			// No active configuration, skip reward
			s.logger.Info("No active reward configuration found, skipping reward")
			return decimal.Zero, nil
		}
		return decimal.Zero, fmt.Errorf("failed to get reward configuration: %w", err)
	}

	// Check minimum transaction amount
	minAmount, err := decimal.NewFromString(config.MinTransactionAmount)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid minimum transaction amount format: %w", err)
	}
	if paidAmount.LessThan(minAmount) {
		s.logger.Info(fmt.Sprintf("Amount ₦%d below minimum ₦%s, skipping reward",
			paidAmount, config.MinTransactionAmount))
		return decimal.Zero, nil
	}

	// Calculate reward points: amount × rate
	rewardRate, err := decimal.NewFromString(config.RewardRate)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid reward rate format: %w", err)
	}
	points := paidAmount.Mul(rewardRate)

	// Apply max points cap if configured
	if config.MaxPointsPerTransaction.Valid {
		maxPoints, err := decimal.NewFromString(config.MaxPointsPerTransaction.String)
		if err != nil {
			return decimal.Zero, fmt.Errorf("invalid max points format: %w", err)
		}
		if points.GreaterThan(maxPoints) {
			points = maxPoints
		}
	}

	pointsEarned = points
	if pointsEarned.LessThanOrEqual(decimal.Zero) {
		s.logger.Info(fmt.Sprintf("Calculated points ₦%s are zero or negative, skipping reward", pointsEarned.String()))
		return decimal.Zero, nil
	}

	// Award points
	description := fmt.Sprintf("%s bonus", serviceType)

	x, err := qtx.AwardRewardPoints(ctx, db.AwardRewardPointsParams{
		UserID:                int64(userID),
		PointsAmount:          pointsEarned.String(),
		TransactionID:         uuid.NullUUID{UUID: transactionID, Valid: true},
		SourceTransactionType: sql.NullString{String: "airtime_purchase", Valid: true},
		TransactionAmount:     sql.NullString{String: paidAmount.String(), Valid: true},
		RewardConfigID:        sql.NullInt64{Int64: config.ID, Valid: true},
		Description:           sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to award reward points: %w", err)
	}

	amountUSD, err := utils.ConvertToUSD(ctx, pointsEarned, "NGN")
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to convert to USD: %w", err)
	}

	ttx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          int64(userID),
		Amount:          pointsEarned.String(),
		Currency:        "NGN",
		AmountUsd:       amountUSD.String(),
		Type:            string(Rewards),
		TransactionFlow: string(InPlatform),
		TFrom:           "platform",
		TTo:             string(Rewards),
		IdempotencyKey:  uuid.NewString(),
		Direction:       string(Credit),
		Status:          string(Success),
		Description:     sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to create transaction: %w", err)
	}

	_, err = qtx.CreateRewardTransaction(ctx, db.CreateRewardTransactionParams{
		UserID:                ttx.UserID,
		PointsAmount:          pointsEarned.String(),
		NairaValue:            pointsEarned.String(),
		TransactionID:         uuid.NullUUID{UUID: ttx.ID, Valid: true},
		SourceTransactionType: sql.NullString{String: string(Airtime), Valid: true},
		TransactionAmount:     sql.NullString{String: paidAmount.String(), Valid: true},
		RewardConfigID:        sql.NullInt64{Int64: config.ID, Valid: true},
		BalanceAfter:          x.BalanceAfter,
		Status:                "completed",
		TransactionType:       "earned",
		Description:           sql.NullString{String: description, Valid: true},
	})
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to create reward transaction: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Reward points awarded: User=%d, Points=₦%d, TX=%d, Rate=%s%%",
		userID, pointsEarned, transactionID, config.RewardRate))

	return pointsEarned, nil

}

// GetUserRewardBalance is a convenience method to get user's reward balance
func (s *TransactionService) GetUserRewardBalance(ctx context.Context, userID int64) (string, error) {
	balance, err := s.store.GetUserRewardBalance(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to get reward balance: %w", err)
	}
	return balance.RewardBalance, nil
}

// UpdateStreakAfterBillPayment updates streak after successful bill payment
func (s *TransactionService) UpdateStreakAfterBillPayment(
	ctx context.Context,
	userID int64,
	transactionID uuid.UUID,
	transactionType TransactionType,
) error {
	if s.streakUpdater == nil {
		s.logger.Warn("Streak updater not configured")
		return nil
	}

	return s.streakUpdater.UpdateStreakOnTransaction(
		ctx,
		userID,
		transactionID,
		string(transactionType),
	)
}

func (s *TransactionService) HandleAirtime(ctx context.Context, user *db.User, req BuyAirtimeRequest) (*BuyAirtimeResponse, error) {
	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	existingTx, err := s.store.WithTx(dbTx).GetTransactionByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil {
		switch existingTx.Status {
		case string(Pending):
			return nil, fmt.Errorf("transaction with reference %s is already pending", existingTx.IdempotencyKey)
		case string(Success):
			return nil, fmt.Errorf("transaction with reference %s has already been completed successfully", existingTx.IdempotencyKey)
		//TODO: case string(Failed):
		// 	s.logger.Info(fmt.Sprintf("Retrying failed transaction with idempotency key: %s", req.IdempotencyKey))
		// Proceed to retry the transaction
		default:
			return nil, fmt.Errorf("a transaction with the same idempotency key already exists with status: %s", existingTx.Status)
		}
	}

	// 1. lock user NGN wallet
	NGNWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user NGN wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(NGNWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance format: %w", err)
	}

	amount := decimal.NewFromInt(req.Amount)

	// Check sufficient balance
	if walletBalance.LessThan(amount) {
		return nil, fmt.Errorf("insufficient balance in Naira wallet")
	}

	var redemptionApplied bool
	var finalAmount decimal.Decimal
	var pointsEarned float64
	notificationMsg := fmt.Sprintf("You have received an airtime of %d to %s", req.Amount, req.Phone)

	finalAmount = amount
	pointsToUseDecimal := decimal.NewFromFloat32(req.PointsToUse)
	if req.UseRewardPoints && req.PointsToUse > 0 {
		finalAmount, redemptionApplied, err = s.ProcessRewardRedemption(
			ctx, dbTx, user, pointsToUseDecimal, amount,
		)
		if err != nil {
			return nil, err
		}

		if redemptionApplied {
			s.logger.Info(fmt.Sprintf("Reward redemption: User=%d, Points=₦%d, Original=₦%d, Final=₦%d",
				user.ID, pointsToUseDecimal, req.Amount, finalAmount))
		}
	}

	// Create transaction record
	amountUsd, err := utils.ConvertToUSD(ctx, amount, NGNWallet.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	txx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            string(Airtime),
		Description:     sql.NullString{String: fmt.Sprintf("%s purchase", Airtime), Valid: true},
		TransactionFlow: string(Outflow),
		Amount:          amount.String(),
		AmountUsd:       amountUsd.String(),
		IdempotencyKey:  req.IdempotencyKey,
		Currency:        NGNWallet.Currency,
		Direction:       string(Debit),
		TFrom:           string(Wallet),
		TTo:             string(OffPlatform),
		Status:          string(Pending),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction record: %w", err)
	}

	stx, err := s.store.WithTx(dbTx).CreateServiceMetadata(ctx, db.CreateServiceMetadataParams{
		SourceWallet:    uuid.NullUUID{UUID: NGNWallet.ID, Valid: true},
		TransactionID:   txx.ID,
		ServiceID:       sql.NullString{String: req.ServiceID, Valid: true},
		ReceivedAmount:  sql.NullString{String: amount.String(), Valid: true},
		SentAmount:      sql.NullString{String: finalAmount.String(), Valid: true},
		ServiceType:     string(Airtime),
		ServiceProvider: sql.NullString{String: "VTPass", Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bill transaction metadata: %w", err)
	}

	_, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: amount.String(), Valid: true},
		ID:      NGNWallet.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to debit wallet: %v", err)
	}

	purchaseRequestID := time.Now().UTC().Add(time.Hour * 1).Format("20060102150405")

	btx, err := s.billProvider.BuyAirtime(bills.PurchaseAirtimeRequest{
		ServiceID: req.ServiceID,
		Phone:     req.Phone,
		RequestID: purchaseRequestID,
		Amount:    req.Amount,
	})
	if err != nil {
		return nil, fmt.Errorf("airtime purchase failed: %v", err)
	}

	switch btx.Status {
	case "failed":
		// Refund wallet
		_, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: amount.String(), Valid: true},
			ID:      NGNWallet.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to refund wallet on failed airtime: %v", err)
		}

		// Mark transaction as failed in DB
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: string(Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Failed),
			TransactionID: stx.TransactionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update service metadata status: %v", err)
		}

		// Commit the refund
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}

		return &BuyAirtimeResponse{
			Amount:               amount,
			AmountPaid:           finalAmount.InexactFloat64(),
			BonusEarned:          pointsEarned,
			Phone:                req.Phone,
			TransactionType:      txx.Type,
			Date:                 txx.CreatedAt,
			TransactionReference: txx.IdempotencyKey,
			Status:               btx.Status,
		}, nil

	case "pending":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: string(Pending),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update tx record: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Pending),
			TransactionID: stx.TransactionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update service metadata status: %v", err)
		}

		// Commit the pending status
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}

		return &BuyAirtimeResponse{
			Amount:               amount,
			AmountPaid:           finalAmount.InexactFloat64(),
			BonusEarned:          pointsEarned,
			Phone:                req.Phone,
			TransactionType:      txx.Type,
			Date:                 txx.CreatedAt,
			TransactionReference: txx.IdempotencyKey,
			Status:               btx.Status,
		}, nil

	case "delivered":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update tx record: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateBillServiceTransactionID(ctx, db.UpdateBillServiceTransactionIDParams{
			ServiceTransactionID: sql.NullString{String: btx.TransactionID, Valid: true},
			TransactionID:        txx.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update bill tx record: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Success),
			TransactionID: stx.TransactionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update service metadata status: %v", err)
		}

		if redemptionApplied {
			err = s.CompleteRewardRedemption(
				ctx,
				dbTx,
				int32(user.ID),
				txx.ID,
				pointsToUseDecimal,
				amount,
				finalAmount,
				"airtime",
				req.ServiceID,
			)
			if err != nil {
				// Log but don't fail transaction
				s.logger.Error("Failed to complete reward redemption:", err)
			}
		}

		pointAmount, err := s.AwardRewardPoints(
			ctx,
			dbTx,
			int32(user.ID),
			txx.ID,
			amount,
			"airtime",
			"bill_payment",
		)
		if err != nil {
			// Log but don't fail transaction
			s.logger.Error("Failed to award reward points:", err)
		}

		if pointAmount.GreaterThan(decimal.Zero) {
			s.logger.Info(fmt.Sprintf("Points awarded for airtime purchase: User=%d, Points=₦%s, TX=%d",
				user.ID, pointAmount.String(), txx.ID))
			pointsEarned = pointAmount.InexactFloat64()
		}
		pointsEarned = pointAmount.InexactFloat64()

		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("airtime purchase failed: %w", err)
		}

		// audit log
		logEntry := audit.NewTransactionLog(
			audit.EventAirtimePurchase,
			txx.ID.String(),
			user.Role,
			user.ID,
			amount.InexactFloat64(),
			"NGN",
			true,
		)
		logEntry.Metadata = map[string]any{} // TODO:
		s.audit.Log(logEntry)

		err = s.streakUpdater.UpdateStreakOnTransaction(
			ctx,
			user.ID,
			txx.ID,
			string(Airtime),
		)
		if err != nil {
			s.logger.Error("Failed to update streak:", err)
		}

		if pointsToUseDecimal.GreaterThan(decimal.Zero) {
			notificationMsg += fmt.Sprintf(". You saved ₦%2.f using reward points", pointsToUseDecimal.InexactFloat64())
		}
		if pointsEarned > 0 {
			notificationMsg += fmt.Sprintf(". You earned ₦%2.f in reward points!", pointsEarned)
		}

		_, err = s.notifyr.CreateWithRecipients(ctx, nil, "Airtime Purchase", notificationMsg, "system", []int64{user.ID})
		if err != nil {
			entry := audit.WarningLog("InApp Notification failed", err.Error())
			s.audit.Log(entry)
		}

		err = s.push.SuccessfulAirtimePurchase(ctx, user.ID, req.Amount, req.Phone)
		if err != nil {
			s.logger.Error(fmt.Sprintf("=============Error sending push notification: %v", err))
			entry := audit.WarningLog("Push Notification failed", err.Error())
			s.audit.Log(entry)
		}

		return &BuyAirtimeResponse{
			Amount:               amount,
			AmountPaid:           finalAmount.InexactFloat64(),
			BonusEarned:          pointsEarned,
			Phone:                req.Phone,
			TransactionType:      txx.Type,
			Date:                 txx.CreatedAt,
			TransactionReference: txx.IdempotencyKey,
			Status:               btx.Status,
		}, nil

	default:
		return nil, fmt.Errorf("unknown airtime status: %s", btx.Status)
	}

}

func (s *TransactionService) HandleData(ctx context.Context, user *db.User, req BuyDataRequest) (*BuyDataResponse, error) {
	variations, err := s.redis.GetVariations(ctx, fmt.Sprintf("variations:%s", req.ServiceID))
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to get variations from cache: %v", err))
	}
	if len(variations) == 0 {
		remoteVariations, err := s.billProvider.GetServiceVariation(req.ServiceID)
		if err != nil {
			return nil, err
		}

		if remoteVariations != nil {
			variations = make([]models.BillVariation, len(remoteVariations))
			for i, variation := range remoteVariations {
				variations[i] = models.BillVariation{
					VariationCode:   variation.VariationCode,
					Name:            variation.Name,
					VariationAmount: variation.VariationAmount,
					FixedPrice:      variation.FixedPrice,
				}
			}

			err = s.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", req.ServiceID), variations)
			if err != nil {
				s.logger.Error(fmt.Sprintf("failed to store variations in cache: %v", err))
			}
		}
	}

	var selectedVariation *models.BillVariation
	for _, variation := range variations {
		if variation.VariationCode == req.VariationCode {
			selectedVariation = &variation
			break
		}
	}

	if selectedVariation == nil {
		return nil, fmt.Errorf("invalid variation code")
	}

	amount, err := decimal.NewFromString(selectedVariation.VariationAmount)
	if err != nil {
		return nil, err
	}

	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	existingTx, err := s.store.WithTx(dbTx).GetTransactionByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil {
		switch existingTx.Status {
		case string(Pending):
			return nil, fmt.Errorf("transaction with reference %s is already pending", existingTx.IdempotencyKey)
		case string(Success):
			return nil, fmt.Errorf("transaction with reference %s has already been completed successfully", existingTx.IdempotencyKey)
		//TODO: case string(Failed):
		// 	s.logger.Info(fmt.Sprintf("Retrying failed transaction with idempotency key: %s", req.IdempotencyKey))
		// Proceed to retry the transaction
		default:
			return nil, fmt.Errorf("a transaction with the same idempotency key already exists with status: %s", existingTx.Status)
		}
	}

	// 1. lock user NGN wallet
	NGNWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user NGN wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(NGNWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance format: %w", err)
	}

	// Check sufficient balance
	if walletBalance.LessThan(amount) {
		return nil, fmt.Errorf("insufficient balance in Naira wallet")
	}

	var redemptionApplied bool
	var finalAmount decimal.Decimal
	var pointsEarned float64
	notificationMsg := fmt.Sprintf("You have received %s to %s", selectedVariation.VariationCode, req.Phone)

	finalAmount = amount
	pointsToUseDecimal := decimal.NewFromFloat32(req.PointsToUse)
	if req.UseRewardPoints && req.PointsToUse > 0 {
		finalAmount, redemptionApplied, err = s.ProcessRewardRedemption(
			ctx, dbTx, user, pointsToUseDecimal, amount,
		)
		if err != nil {
			return nil, err
		}

		if redemptionApplied {
			s.logger.Info(fmt.Sprintf("Reward redemption: User=%d, Points=₦%d, Original=₦%d, Final=₦%d",
				user.ID, pointsToUseDecimal, amount, finalAmount))
		}
	}

	// Create transaction record
	amountUsd, err := utils.ConvertToUSD(ctx, amount, NGNWallet.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	txx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            string(Data),
		Description:     sql.NullString{String: fmt.Sprintf("%s purchase", Data), Valid: true},
		TransactionFlow: string(Outflow),
		Amount:          amount.String(),
		AmountUsd:       amountUsd.String(),
		IdempotencyKey:  req.IdempotencyKey,
		Currency:        NGNWallet.Currency,
		Direction:       string(Debit),
		TFrom:           string(Wallet),
		TTo:             string(OffPlatform),
		Status:          string(Pending),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction record: %w", err)
	}

	stx, err := s.store.WithTx(dbTx).CreateServiceMetadata(ctx, db.CreateServiceMetadataParams{
		SourceWallet:    uuid.NullUUID{UUID: NGNWallet.ID, Valid: true},
		TransactionID:   txx.ID,
		ServiceID:       sql.NullString{String: req.ServiceID, Valid: true},
		ReceivedAmount:  sql.NullString{String: amount.String(), Valid: true},
		SentAmount:      sql.NullString{String: finalAmount.String(), Valid: true},
		ServiceType:     string(Data),
		ServiceProvider: sql.NullString{String: "VTPass", Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bill transaction metadata: %w", err)
	}

	_, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: amount.String(), Valid: true},
		ID:      NGNWallet.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to debit wallet: %v", err)
	}

	purchaseRequestID := time.Now().UTC().Add(time.Hour * 1).Format("20060102150405")

	btx, err := s.billProvider.BuyData(bills.PurchaseDataRequest{
		ServiceID:     req.ServiceID,
		BillersCode:   req.Phone,
		VariationCode: req.VariationCode,
		Phone:         req.Phone,
		RequestID:     purchaseRequestID,
		Amount:        amount.IntPart(),
	})
	if err != nil {
		return nil, fmt.Errorf("airtime purchase failed: %v", err)
	}

	switch btx.Status {
	case "failed":
		// Refund wallet
		_, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: amount.String(), Valid: true},
			ID:      NGNWallet.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to refund wallet on failed airtime: %v", err)
		}

		// Mark transaction as failed in DB
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: string(Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Failed),
			TransactionID: stx.TransactionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update service metadata status: %v", err)
		}

		// Commit the refund
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}

		return &BuyDataResponse{
			Amount:               amount,
			AmountPaid:           finalAmount.InexactFloat64(),
			BonusEarned:          pointsEarned,
			Phone:                req.Phone,
			TransactionType:      txx.Type,
			Date:                 txx.CreatedAt,
			TransactionReference: txx.IdempotencyKey,
			Status:               btx.Status,
			Plan:                 selectedVariation.VariationCode,
		}, nil

	case "pending":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: string(Pending),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update tx record: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Pending),
			TransactionID: stx.TransactionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update service metadata status: %v", err)
		}

		// Commit the pending status
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}

		return &BuyDataResponse{
			Amount:               amount,
			AmountPaid:           finalAmount.InexactFloat64(),
			BonusEarned:          pointsEarned,
			Phone:                req.Phone,
			TransactionType:      txx.Type,
			Date:                 txx.CreatedAt,
			TransactionReference: txx.IdempotencyKey,
			Status:               btx.Status,
			Plan:                 selectedVariation.VariationCode,
		}, nil

	case "delivered":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: string(Success),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update tx record: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateBillServiceTransactionID(ctx, db.UpdateBillServiceTransactionIDParams{
			ServiceTransactionID: sql.NullString{String: btx.TransactionID, Valid: true},
			TransactionID:        txx.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update bill tx record: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Success),
			TransactionID: stx.TransactionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update service metadata status: %v", err)
		}

		if redemptionApplied {
			err = s.CompleteRewardRedemption(
				ctx,
				dbTx,
				int32(user.ID),
				txx.ID,
				pointsToUseDecimal,
				amount,
				finalAmount,
				"airtime",
				req.ServiceID,
			)
			if err != nil {
				// Log but don't fail transaction
				s.logger.Error("Failed to complete reward redemption:", err)
			}
		}

		pointAmount, err := s.AwardRewardPoints(
			ctx,
			dbTx,
			int32(user.ID),
			txx.ID,
			amount,
			"airtime",
			"bill_payment",
		)
		if err != nil {
			// Log but don't fail transaction
			s.logger.Error("Failed to award reward points:", err)
		}

		if pointAmount.GreaterThan(decimal.Zero) {
			s.logger.Info(fmt.Sprintf("Points awarded for airtime purchase: User=%d, Points=₦%s, TX=%d",
				user.ID, pointAmount.String(), txx.ID))
			pointsEarned = pointAmount.InexactFloat64()
		}
		pointsEarned = pointAmount.InexactFloat64()

		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("airtime purchase failed: %w", err)
		}

		// audit log
		logEntry := audit.NewTransactionLog(
			audit.EventAirtimePurchase,
			txx.ID.String(),
			user.Role,
			user.ID,
			amount.InexactFloat64(),
			"NGN",
			true,
		)
		logEntry.Metadata = map[string]any{} // TODO:
		s.audit.Log(logEntry)

		err = s.streakUpdater.UpdateStreakOnTransaction(
			ctx,
			user.ID,
			txx.ID,
			string(Airtime),
		)
		if err != nil {
			s.logger.Error("Failed to update streak:", err)
		}

		// if pointsToUseDecimal.GreaterThan(decimal.Zero) {
		// 	notificationMsg += fmt.Sprintf(". You saved ₦%2.f using reward points", pointsToUseDecimal.InexactFloat64())
		// }
		if pointsEarned > 0 {
			notificationMsg += fmt.Sprintf(". You earned ₦%2.f in reward points!", pointsEarned)
		}

		_, err = s.notifyr.CreateWithRecipients(ctx, nil, "Data Purchase", notificationMsg, "system", []int64{user.ID})
		if err != nil {
			entry := audit.WarningLog("InApp Notification failed", err.Error())
			s.audit.Log(entry)
		}

		err = s.push.SuccessfulDataPurchase(ctx, user.ID, selectedVariation.Name, req.Phone)
		if err != nil {
			s.logger.Error(fmt.Sprintf("=============Error sending push notification: %v", err))
			entry := audit.WarningLog("Push Notification failed", err.Error())
			s.audit.Log(entry)
		}

		return &BuyDataResponse{
			Amount:               amount,
			AmountPaid:           finalAmount.InexactFloat64(),
			BonusEarned:          pointsEarned,
			Phone:                req.Phone,
			TransactionType:      txx.Type,
			Date:                 txx.CreatedAt,
			TransactionReference: txx.IdempotencyKey,
			Status:               btx.Status,
			Plan:                 selectedVariation.VariationCode,
		}, nil

	default:
		return nil, fmt.Errorf("unknown Data status: %s", btx.Status)
	}

}
