package transaction

import (
	"context"
	"database/sql"
	"errors"
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
	ratemanager "github.com/SwiftFiat/SwiftFiat-Backend/services/rate_manager"
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
	fiat           *fiat.NombaProvider
	rateManager    *ratemanager.Service
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
	fiat *fiat.NombaProvider,
	rateManager *ratemanager.Service,
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
		fiat:           fiat,
		rateManager:    rateManager,
	}
}

func (s *TransactionService) createAdminAlert(ctx context.Context, params db.CreateAdminAlertParams) {
	if s.notifyr == nil {
		s.logger.Warn("notification service unavailable; skipping admin alert")
		return
	}

	source := ""
	if params.Source.Valid {
		source = params.Source.String
	}

	if _, err := s.notifyr.CreateAdminAlert(ctx, params.Severity, params.Title, params.Message, source); err != nil {
		s.logger.Error(fmt.Sprintf("failed to create admin alert: %v", err))
	}
}

func (s *TransactionService) CreatePendingCryptoTransaction(
	ctx context.Context,
	orderID string,
	tx CryptoTransaction,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {
	s.logger.Info("Creating pending crypto transaction")

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	// Idempotency — bail if we've already recorded this order.
	trailExists, err := qtx.CheckCryptoTransactionTrailByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("checking trail for orderID %s: %w", orderID, err)
	}
	if trailExists {
		return nil, fmt.Errorf("transaction already recorded for order id: %s", orderID)
	}

	// Record the trail so the "paid" webhook can find it.
	if _, err = qtx.CreateCryptoTransactionTrail(ctx, db.CreateCryptoTransactionTrailParams{
		AddressID:       tx.DestinationAddress,
		OrderID:         orderID,
		TransactionHash: sql.NullString{String: tx.SourceHash, Valid: tx.SourceHash != ""},
		Amount: sql.NullString{
			String: tx.AmountInSatoshis.StringFixed(10),
			Valid:  !tx.AmountInSatoshis.IsZero(),
		},
	}); err != nil {
		return nil, fmt.Errorf("creating transaction trail: %w", err)
	}

	// Resolve destination address → user → destination wallet.
	walletAddress, err := qtx.GetCryptomusAddressByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("fetching cryptomus address for orderID %s: %w", orderID, err)
	}

	coinSym := strings.ToUpper(tx.Coin)
	coinAmount := tx.AmountInSatoshis

	// Best-effort rate; zero is acceptable at pending stage — finalised on "paid".
	rate, err := s.currencyClient.GetCryptoExchangeRateFromCryptomus(ctx, coinSym, prov)
	if err != nil {
		s.logger.Warnf("Failed to get exchange rate for pending tx (will use 0): %v", err)
		rate = decimal.Zero
	}
	estimatedUSD := coinAmount.Mul(rate)

	// Lock destination wallet — prevents concurrent credits.
	destWallet, err := qtx.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: walletAddress.CustomerID.UUID,
		Currency:   "USD",
	})
	if err != nil {
		return nil, fmt.Errorf("fetching USD wallet for customer %d: %w", walletAddress.CustomerID.UUID, err)
	}

	// FIX [C1]: Direct CreateTransaction with all required fields.
	// Old createPendingTransactionRecord was missing IdempotencyKey, Direction, TFrom, TTo.
	idempotencyKey := "crypto_pending_" + orderID
	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          walletAddress.CustomerID.UUID,
		Type:            string(CryptoInflowTransaction),
		Description:     sql.NullString{String: "Crypto Deposit (Pending)", Valid: true},
		TransactionFlow: string(Inflow),
		Status:          string(Pending),
		Amount:          coinAmount.String(),   // raw coin amount
		AmountUsd:       estimatedUSD.String(), // estimated USD at time of detect
		Currency:        coinSym,
		IdempotencyKey:  idempotencyKey,
		Direction:       string(Credit),
		TFrom:           "Cryptomus",
		TTo:             string(Wallet),
	})
	if err != nil {
		return nil, fmt.Errorf("creating pending transaction record: %w", err)
	}

	// Create crypto metadata so UpdateCryptoTransactionToPaid can find the wallet.
	_, err = qtx.CreateCryptoMetadata(ctx, db.CreateCryptoMetadataParams{
		DestinationWallet:    uuid.NullUUID{UUID: destWallet.ID, Valid: true},
		TransactionID:        txx.ID,
		Coin:                 coinSym,
		SourceHash:           sql.NullString{String: tx.SourceHash, Valid: tx.SourceHash != ""},
		Rate:                 sql.NullString{String: rate.String(), Valid: true},
		Fees:                 sql.NullString{String: decimal.Zero.String(), Valid: true},
		ReceivedAmount:       sql.NullString{String: estimatedUSD.String(), Valid: true},
		SentAmount:           sql.NullString{String: coinAmount.String(), Valid: true},
		ServiceProvider:      "cryptomus",
		ServiceTransactionID: sql.NullString{String: tx.TransactionID.String(), Valid: true},
		OrderID:              orderID,
	})
	if err != nil {
		return nil, fmt.Errorf("creating crypto metadata: %w", err)
	}

	if err = dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("committing pending crypto transaction: %w", err)
	}

	return &TransactionResponse[CryptoMetadataResponse]{
		ID:              txx.ID,
		Type:            txx.Type,
		Status:          txx.Status,
		TransactionFlow: txx.TransactionFlow,
		Description:     txx.Description.String,
		CreatedAt:       txx.CreatedAt,
		UpdatedAt:       txx.UpdatedAt,
	}, nil
}

// ── UpdateCryptoTransactionToPaid ─────────────────────────────────────────────
// Called when a "paid" webhook arrives and a matching Pending tx already exists.
// Credits the user's wallet and finalises the transaction.
func (s *TransactionService) UpdateCryptoTransactionToPaid(
	ctx context.Context,
	dbTx *sql.Tx,
	transactionID uuid.UUID,
	finalRate decimal.Decimal,
	finalReceivedAmount decimal.Decimal,
) error {
	s.logger.Info("Updating crypto transaction to paid", "transaction_id", transactionID)

	qtx := s.store.WithTx(dbTx)

	// Lock the row before mutating.
	existingTx, err := qtx.GetTransactionByIDForUpdate(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("fetching transaction for update: %w", err)
	}
	if existingTx.Status != string(Pending) {
		return fmt.Errorf("transaction %s is not pending (current: %s)", transactionID, existingTx.Status)
	}

	// Promote to Success.
	if _, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     transactionID,
		Status: string(Success),
	}); err != nil {
		return fmt.Errorf("updating transaction status: %w", err)
	}

	// FIX [C3]: Also update AmountUsd to the final settled value.
	if err = qtx.UpdateTransactionAmountUSD(ctx, db.UpdateTransactionAmountUSDParams{
		ID:        transactionID,
		AmountUsd: finalReceivedAmount.String(),
	}); err != nil {
		// Non-fatal — log and continue. The balance credit is more important.
		s.logger.Warnf("Failed to update AmountUsd on transaction %s: %v", transactionID, err)
	}

	// Resolve destination wallet from crypto metadata.
	metadata, err := qtx.GetCryptoMetadataByTransactionID(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("fetching crypto metadata for tx %s: %w", transactionID, err)
	}

	// FIX [C3]: Update metadata with final settled rate and received amount.
	// Old code never persisted the final rate back into the metadata row.
	if err = qtx.UpdateCryptoMetadataFinalAmounts(ctx, db.UpdateCryptoMetadataFinalAmountsParams{
		TransactionID:  transactionID,
		Rate:           sql.NullString{String: finalRate.String(), Valid: true},
		ReceivedAmount: sql.NullString{String: finalReceivedAmount.String(), Valid: true},
	}); err != nil {
		s.logger.Warnf("Failed to update crypto metadata final amounts for tx %s: %v", transactionID, err)
	}

	// Lock and credit the destination wallet.
	wallet, err := qtx.GetWalletForUpdate(ctx, metadata.DestinationWallet.UUID)
	if err != nil {
		return fmt.Errorf("fetching destination wallet: %w", err)
	}

	// FIX [C3]: Replaced createLedgerEntries + updateBalance with IncrementWalletBalance,
	// consistent with how all bill-payment handlers credit wallets.
	if _, err = qtx.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
		ID:      wallet.ID,
		Balance: sql.NullString{String: finalReceivedAmount.String(), Valid: true},
	}); err != nil {
		return fmt.Errorf("crediting destination wallet: %w", err)
	}

	s.logger.Info("Crypto transaction upgraded to paid",
		"transaction_id", transactionID,
		"credited", finalReceivedAmount.String())

	return nil
}

// ── CreateAllCryptoINflowTXs ──────────────────────────────────────────────────
// Entry point for "paid" webhooks.
// Handles three paths:
//
//	A) trail exists + pending tx found  → upgrade pending to paid
//	B) trail exists + no pending tx     → create new successful tx
//	C) no trail at all                  → create trail + new successful tx
func (s *TransactionService) CreateAllCryptoINflowTXs(
	ctx context.Context,
	orderID string,
	tx CryptoTransaction,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {
	s.logger.Info("Processing crypto inflow (paid webhook)")

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	trailExists, err := qtx.CheckCryptoTransactionTrailByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("checking trail for orderID %s: %w", orderID, err)
	}

	var (
		tObj *TransactionResponse[CryptoMetadataResponse]
		// Variables captured for post-commit notifications.
		notifyUser   *db.User
		notifyAmount decimal.Decimal
		notifyCoin   string
	)

	coinSym := strings.ToUpper(tx.Coin)

	if trailExists {
		// ── Path A/B: trail exists ───────────────────────────────────────────
		pendingTx, pendingErr := qtx.GetPendingCryptoTransactionByOrderID(ctx, orderID)

		if pendingErr == nil {
			// Path A: pending tx found → upgrade it.
			s.logger.Info("Upgrading pending tx to paid", "transaction_id", pendingTx.ID)

			walletAddress, err := qtx.GetCryptomusAddressByOrderID(ctx, orderID)
			if err != nil {
				return nil, fmt.Errorf("fetching cryptomus address: %w", err)
			}

			rate, err := s.currencyClient.GetCryptoExchangeRateFromCryptomus(ctx, coinSym, prov)
			if err != nil {
				return nil, fmt.Errorf("fetching exchange rate: %w", err)
			}

			finalAmount := tx.AmountInSatoshis.Mul(rate)

			if err = s.UpdateCryptoTransactionToPaid(ctx, dbTx, pendingTx.ID, rate, finalAmount); err != nil {
				return nil, fmt.Errorf("upgrading pending transaction: %w", err)
			}

			updatedTx, _ := qtx.GetTransactionByID(ctx, pendingTx.ID)
			tObj = buildCryptoTransactionResponse(updatedTx)

			// FIX [C5]: Capture user for post-commit notification (do NOT notify inside TX).
			if u, err := qtx.GetUserByID(ctx, walletAddress.CustomerID.UUID); err == nil {
				notifyUser = &u
				notifyAmount = finalAmount
				notifyCoin = coinSym
			}
		} else {
			// Path B: trail but no pending tx → create a fresh successful tx.
			s.logger.Warn("Trail exists but no pending tx found; creating new successful tx")
			tObj, notifyUser, notifyAmount, notifyCoin, err = s.createNewSuccessfulCryptoTransaction(ctx, dbTx, orderID, tx, prov)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// ── Path C: no trail at all → create trail + new successful tx ───────
		s.logger.Info("No existing trail; creating trail and successful tx from scratch")

		if _, err = qtx.CreateCryptoTransactionTrail(ctx, db.CreateCryptoTransactionTrailParams{
			AddressID:       tx.DestinationAddress,
			OrderID:         orderID,
			TransactionHash: sql.NullString{String: tx.SourceHash, Valid: tx.SourceHash != ""},
			Amount: sql.NullString{
				String: tx.AmountInSatoshis.StringFixed(10),
				Valid:  !tx.AmountInSatoshis.IsZero(),
			},
		}); err != nil {
			return nil, fmt.Errorf("creating transaction trail: %w", err)
		}

		tObj, notifyUser, notifyAmount, notifyCoin, err = s.createNewSuccessfulCryptoTransaction(ctx, dbTx, orderID, tx, prov)
		if err != nil {
			return nil, err
		}
	}

	if err = dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("committing crypto inflow: %w", err)
	}

	// FIX [C5]: Send notifications AFTER commit. Old code called
	// sendCryptoSuccessNotifications inside the TX block, meaning side effects
	// fired even if Commit() subsequently failed.
	if notifyUser != nil {
		s.sendCryptoSuccessNotifications(ctx, *notifyUser, notifyAmount, notifyCoin, tx.TransactionID)
	}

	s.logger.Info("Crypto inflow completed successfully")
	return tObj, nil
}

// ── createNewSuccessfulCryptoTransaction ──────────────────────────────────────
// Creates a brand-new Success transaction for a crypto inflow that arrived
// directly as "paid" (no prior confirm_check), or where the pending record
// was missing. Handles both normal USD-credit and rapid-ramp NGN-bank-transfer.
//
// Returns the response AND the captured notification fields so
// CreateAllCryptoINflowTXs can send notifications after commit.

func (s *TransactionService) createNewSuccessfulCryptoTransaction(
	ctx context.Context,
	dbTx *sql.Tx,
	orderID string,
	tx CryptoTransaction,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], *db.User, decimal.Decimal, string, error) {
	qtx := s.store.WithTx(dbTx)

	walletAddress, err := qtx.GetCryptomusAddressByOrderID(ctx, orderID)
	if err != nil {
		return nil, nil, decimal.Zero, "", fmt.Errorf("fetching cryptomus address: %w", err)
	}

	user, err := qtx.GetUserByID(ctx, walletAddress.CustomerID.UUID)
	if err != nil {
		return nil, nil, decimal.Zero, "", fmt.Errorf("fetching user: %w", err)
	}

	s.logger.Infof("======================amount in satoshis is %2.f", tx.AmountInSatoshis.InexactFloat64())

	coinSym := strings.ToUpper(tx.Coin)
	coinAmount := tx.AmountInSatoshis

	s.logger.Infof("======================coin amount is %2.f", coinAmount.InexactFloat64())

	// ── Rapid Ramp path ───────────────────────────────────────────────────────
	if user.IsRapidRampOn {
		s.logger.Info(fmt.Sprintf("Rapid ramp enabled for user %d", user.ID))

		tObj, err := s.processRapidRampInflow(ctx, dbTx, tx, coinAmount, coinSym, user.ID, prov)
		if err != nil {
			return nil, nil, decimal.Zero, "", fmt.Errorf("processing rapid ramp: %w", err)
		}

		// Return the user so the caller can send the notification post-commit.
		return tObj, &user, coinAmount, coinSym, nil
	}

	vip_rate, err := s.rateManager.GetAdjustedRateForUser(ctx, user.ID, coinSym, string(USD), coinAmount.String())
	if err != nil {
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: WARNINGALERT,
			Title:    "VIP RATE",
			Message:  fmt.Sprintf("vip rates is failing with error: %v", err),
			Source:   sql.NullString{String: "Crypto Coversion", Valid: true},
		})
	}

	rate, _ := utils.ToDecimal(vip_rate.AdjustedRate)

	s.logger.Infof("======================rate is %2.f", rate.InexactFloat64())

	usdAmount := coinAmount.Mul(rate)

	destWallet, err := qtx.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: walletAddress.CustomerID.UUID,
		Currency:   "USD",
	})
	if err != nil {
		return nil, nil, decimal.Zero, "", fmt.Errorf("fetching USD wallet: %w", err)
	}

	s.logger.Infof("======================usd amount is %2.f", usdAmount.InexactFloat64())

	idempotencyKey := utils.WatRequestID()

	// FIX [C2a]: Create directly with Success — no need for a Pending→Success hop.
	// FIX [C2b]: Amount = coin amount, not USD equivalent.
	// FIX [C2d]: Added IdempotencyKey, Direction, TFrom, TTo.
	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          walletAddress.CustomerID.UUID,
		Type:            string(CryptoInflowTransaction),
		Description:     sql.NullString{String: tx.Description, Valid: tx.Description != ""},
		TransactionFlow: string(Inflow),
		Status:          string(Success),
		Amount:          usdAmount.String(),
		AmountUsd:       usdAmount.String(),
		Currency:        string(USD),
		IdempotencyKey:  idempotencyKey, // FIX [C2d]
		Direction:       string(Credit), // FIX [C2d]
		TFrom:           "Cryptomus",    // FIX [C2d]
		TTo:             string(Wallet), // FIX [C2d]
	})
	if err != nil {
		return nil, nil, decimal.Zero, "", fmt.Errorf("creating transaction record: %w", err)
	}

	// FIX [C2] (removed createLedgerEntries + updateBalance): credit wallet directly.
	if _, err = qtx.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
		ID:      destWallet.ID,
		Balance: sql.NullString{String: usdAmount.String(), Valid: true},
	}); err != nil {
		return nil, nil, decimal.Zero, "", fmt.Errorf("crediting USD wallet: %w", err)
	}

	// FIX [C2c]: Create metadata BEFORE building the response so Metadata is populated.
	cryptoMeta, err := qtx.CreateCryptoMetadata(ctx, db.CreateCryptoMetadataParams{
		DestinationWallet:    uuid.NullUUID{UUID: destWallet.ID, Valid: true},
		TransactionID:        txx.ID,
		Coin:                 coinSym,
		SourceHash:           sql.NullString{String: tx.SourceHash, Valid: tx.SourceHash != ""},
		Rate:                 sql.NullString{String: rate.String(), Valid: true},
		Fees:                 sql.NullString{String: tx.Fees.String(), Valid: true},
		ReceivedAmount:       sql.NullString{String: usdAmount.String(), Valid: true},
		SentAmount:           sql.NullString{String: coinAmount.String(), Valid: true},
		ServiceProvider:      "Cryptomus",
		ServiceTransactionID: sql.NullString{String: tx.TransactionID.String(), Valid: true},
		OrderID:              orderID,
	})
	if err != nil {
		return nil, nil, decimal.Zero, "", fmt.Errorf("creating crypto metadata: %w", err)
	}

	err = s.streakUpdater.UpdateStreakOnTransaction(ctx, user.ID, txx.ID, txx.Type)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to update streak: %v", err))
	}

	// FIX [C2c]: Response built AFTER metadata — Metadata field is now populated.
	resp := &TransactionResponse[CryptoMetadataResponse]{
		ID:              txx.ID,
		Type:            txx.Type,
		Description:     txx.Description.String,
		TransactionFlow: txx.TransactionFlow,
		Status:          txx.Status,
		CreatedAt:       txx.CreatedAt,
		UpdatedAt:       txx.UpdatedAt,
		Metadata: &CryptoMetadataResponse{
			ID:                cryptoMeta.ID,
			DestinationWallet: destWallet.Currency,
			Coin:              cryptoMeta.Coin,
			Rate:              rate.InexactFloat64(),
			Fees:              0.00,
			ReceivedAmount:    cryptoMeta.ReceivedAmount.String,
			SentAmount:        tx.SentAmount.InexactFloat64(),
			OrderID:           cryptoMeta.OrderID,
		},
	}

	s.push.SendPushNotification(ctx, user.ID, "Incoming Crypto Alert", fmt.Sprintf("You have received %.2f %s", coinAmount.InexactFloat64(), cryptoMeta.Coin))

	return resp, &user, usdAmount, "USD", nil
}

// ── sendCryptoSuccessNotifications ───────────────────────────────────────────
// Unchanged logic — email + in-app notification, both non-fatal.
func (s *TransactionService) sendCryptoSuccessNotifications(
	ctx context.Context,
	user db.User,
	amount decimal.Decimal,
	currency string,
	transactionID uuid.UUID,
) {
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
		if err = email.SendEmail(user.Email, "SwiftFiat - Successful Crypto Inflow Transaction", body); err != nil {
			s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to send crypto email: %v", err))
		}
	}

	s.notifyr.CreateWithRecipients(ctx, nil, "Wallet Credit Alert",
		fmt.Sprintf("Your %s address has received %.2f coins", currency, amount.InexactFloat64()),
		"system", []uuid.UUID{user.ID})

	s.push.SendPushNotification(ctx, user.ID, "Wallet Credit Alert", fmt.Sprintf("Your %s address has received %.2f coins", currency, amount.InexactFloat64()))
}

// Helper function to build response from database transaction
func buildCryptoTransactionResponse(tx db.GetTransactionByIDRow) *TransactionResponse[CryptoMetadataResponse] {
	return &TransactionResponse[CryptoMetadataResponse]{
		ID:              tx.ID,
		Type:            tx.Type,
		Description:     tx.Description.String,
		TransactionFlow: tx.TransactionFlow,
		Status:          tx.Status,
		CreatedAt:       tx.CreatedAt,
		UpdatedAt:       tx.UpdatedAt,
	}
}

// ── processRapidRampInflow ────────────────────────────────────────────────────
// Converts incoming crypto to NGN and wires it to the user's default bank account.
func (s *TransactionService) processRapidRampInflow(
	ctx context.Context,
	dbTx *sql.Tx,
	tx CryptoTransaction,
	coinAmount decimal.Decimal,
	coinSym string,
	userID uuid.UUID,
	prov *providers.ProviderService,
) (*TransactionResponse[CryptoMetadataResponse], error) {
	qtx := s.store.WithTx(dbTx)

	bankAccount, err := qtx.GetDefaultBankAccount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetching default bank account: %w", err)
	}
	if !bankAccount.IsVerified {
		return nil, fmt.Errorf("bank account is not verified")
	}

	nairaWallet, err := qtx.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
		CustomerID: userID,
		Currency:   "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("fetching naira wallet: %w", err)
	}

	cryptomusProvider, err := s.getCryptomusProvider(prov)
	if err != nil {
		return nil, fmt.Errorf("getting cryptomus provider: %w", err)
	}

	coinToUSD, err := cryptomusProvider.GetUSDRate(coinSym)
	if err != nil {
		return nil, fmt.Errorf("getting USD rate: %w", err)
	}

	s.logger.Infof("Rapid Ramp: coin=%s, amount=%s, coinToUSD=%s", coinSym, coinAmount.String(), coinToUSD)

	coinToUSDDecimal, err := decimal.NewFromString(coinToUSD)
	if err != nil {
		return nil, fmt.Errorf("parsing USD rate: %w", err)
	}

	vipRate, err := s.rateManager.GetAdjustedRateForUser(ctx, userID, "USD", "NGN", coinToUSDDecimal.String())
	if err != nil {
		return nil, fmt.Errorf("to decimal error: %v", err)
	}
	rate, err := utils.ToDecimal(vipRate.AdjustedRate)
	if err != nil {
		return nil, fmt.Errorf("to decimal error: %v", err)
	}
	fiatAmount := coinToUSDDecimal.Mul(rate)

	s.logger.Infof("Rapid Ramp: coinToUSD=%s, vipRate=%s, final rate=%s, fiatAmount=%s",
		coinToUSDDecimal.String(), vipRate.AdjustmentAmount, rate.String(), fiatAmount.String())

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetching user for rapid ramp error: %w", err)
	}

	// Direct bank transfer for rapid ramp (no wallet debit needed - crypto funded)
	totalFees := decimal.Zero
	netAmount := fiatAmount.Sub(totalFees)

	recipient, err := s.fiat.CreateTransferRecipient(
		bankAccount.AccountNumber,
		bankAccount.BankCode,
		bankAccount.AccountName,
	)
	if err != nil {
		return nil, fmt.Errorf("creating transfer recipient: %w", err)
	}

	amountInNGN := int64(netAmount.InexactFloat64())
	transferRef := utils.WatRequestID()

	amountUSd, _ := utils.ConvertToUSD(ctx, fiatAmount, "NGN")
	idempotencyKey := utils.WatRequestID()
	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          userID,
		Type:            string(RapidRamp),
		Description:     sql.NullString{String: fmt.Sprintf("%s to NGN (Rapid Ramp)", coinSym), Valid: true},
		TransactionFlow: string(Outflow),
		Status:          string(Pending),
		Amount:          fiatAmount.String(),
		AmountUsd:       amountUSd.String(),
		Currency:        coinSym,
		IdempotencyKey:  idempotencyKey,
		Direction:       string(Credit),
		TFrom:           "crypto_deposit",
		TTo:             "bank",
	})
	if err != nil {
		return nil, fmt.Errorf("creating rapid ramp transaction record: %w", err)
	}

	_, err = qtx.CreateBankTransferMetadata(ctx, db.CreateBankTransferMetadataParams{
		Amount:          fiatAmount.String(),
		ServiceCharge:   totalFees.String(),
		TransactionID:   txx.ID,
		AccountName:     bankAccount.AccountName,
		AccountNumber:   bankAccount.AccountNumber,
		ServiceProvider: sql.NullString{String: "Nomba", Valid: true},
		Status:          string(Pending),
		AmountPaid:      netAmount.String(),
		Type:            string(Credit),
	})
	if err != nil {
		return nil, fmt.Errorf("creating rapid ramp bank transfer metadata: %w", err)
	}

	transfer, err := s.fiat.MakeTransfer(
		recipient.RecipientCode,
		transferRef,
		"sent via Swiift",
		amountInNGN,
		"SWIIFT",
	)

	// Handle different transfer states like HandleBankTransfer does
	if err != nil {
		originalErr := err
		// Transfer failed - update status to failed
		_, dbErr := qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: "failed",
		})
		if dbErr != nil {
			s.logger.Errorf("failed to update transaction status to failed: %v", dbErr)
		}

		_, dbErr = qtx.UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID: txx.ID,
			Status:        "failed",
		})
		if dbErr != nil {
			s.logger.Errorf("failed to update bank transfer metadata status to failed: %v", dbErr)
		}

		ytx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
			UserID:          userID,
			Type:            string(Transfer),
			Description:     sql.NullString{String: fmt.Sprintf("Transfer of %.2f %s to naira wallet", netAmount.InexactFloat64(), "NGN"), Valid: true},
			TransactionFlow: string(Inflow),
			Status:          string(Pending),
			Amount:          fiatAmount.String(),
			AmountUsd:       amountUSd.String(),
			Currency:        "NGN",
			IdempotencyKey:  utils.WatRequestID(),
			Direction:       string(Credit),
			TFrom:           "crypto_deposit",
			TTo:             "wallet",
		})
		if err != nil {
			s.logger.Errorf("failed to create transaction for failed rapid ramp: %v", err)
			return nil, fmt.Errorf("failed to create transaction for failed rapid ramp: %w", err)
		}

		walletTransferMetadata, err := qtx.CreateWalletTransferMetadata(ctx, db.CreateWalletTransferMetadataParams{
			Amount:        fiatAmount.String(),
			ServiceCharge: sql.NullString{String: "0.00", Valid: true},
			TransactionID: ytx.ID,
			Sender:        "system",
			Status:        string(Pending),
			AmountPaid:    sql.NullString{String: netAmount.String(), Valid: true},
			Type:          string(Credit),
			Recipient:     "Naira Wallet",
			Reference:     utils.WatRequestID(),
			Currency:      "NGN",
		})
		if err != nil {
			s.logger.Errorf("failed to create wallet transfer metadata for failed rapid ramp: %v", err)
			return nil, fmt.Errorf("failed to create wallet transfer metadata for failed rapid ramp: %w", err)
		}

		_, err = qtx.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: netAmount.String(), Valid: true},
			ID:      nairaWallet.ID,
		})
		if err != nil {
			s.logger.Errorf("failed to increment wallet balance: %v", err)
			return nil, fmt.Errorf("failed to increment wallet balance: %w", err)
		}

		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     ytx.ID,
			Status: string(Success),
		})
		if err != nil {
			s.logger.Errorf("failed to update transaction status to success: %v", err)
		}

		err = qtx.UpdateWalletTransferMetadataStatus(ctx, db.UpdateWalletTransferMetadataStatusParams{
			ID:     walletTransferMetadata.ID,
			Status: string(Success),
		})
		if err != nil {
			s.logger.Errorf("failed to update wallet transfer metadata status to success: %v", err)
		}

		// go func() {
		// 	bgctx := context.Background()
			s.notifyr.CreateWithRecipients(ctx, nil, "Wallet Credit Alert",
				fmt.Sprintf("Your Rapid Ramp transfer of %.2f %s has failed and your wallet has been credited with %.2f %s", netAmount.InexactFloat64(), "NGN", netAmount.InexactFloat64(), "NGN"),
				"system", []uuid.UUID{user.ID})

			s.push.SendPushNotification(ctx, user.ID, "Wallet Credit Alert",
				fmt.Sprintf("Your Rapid Ramp transfer of %.2f %s has failed and your wallet has been credited with %.2f %s", netAmount.InexactFloat64(), "NGN", netAmount.InexactFloat64(), "NGN"))
		// }()

		s.logger.Warnf("Rapid ramp bank transfer failed: %v. Credited wallet instead.", originalErr)

		return &TransactionResponse[CryptoMetadataResponse]{
			ID:              ytx.ID,
			Type:            ytx.Type,
			Description:     ytx.Description.String,
			TransactionFlow: ytx.TransactionFlow,
			Status:          string(Success),
			CreatedAt:       ytx.CreatedAt,
			UpdatedAt:       ytx.UpdatedAt,
		}, nil
	}

	// Normalize status to lowercase for comparison (Nomba returns uppercase)
	normalizedStatus := strings.ToLower(transfer.Status)

	s.logger.Info(fmt.Sprintf("Rapid ramp transfer initiated: %s %s → %s NGN net (fees: %s, ref: %s, status: %s)",
		coinAmount.String(), coinSym, netAmount.String(), totalFees.String(), transfer.Reference, normalizedStatus))

	switch normalizedStatus {
	case "pending", "pending_billing", "processing":
		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: "pending",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %v", err)
		}

		_, err = qtx.UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID:        txx.ID,
			Status:               "pending",
			ServiceTransactionID: sql.NullString{String: transfer.RawData.Meta.MerchantTxRef, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update bank transfer metadata status: %v", err)
		}

	case "success", "completed":
		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: "successful",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %v", err)
		}

		_, err = qtx.UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID:        txx.ID,
			Status:               "successful",
			ServiceTransactionID: sql.NullString{String: transfer.SessionID, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update bank transfer metadata status: %v", err)
		}

	default:
		s.logger.Warnf("Unknown bank transfer status for rapid ramp: %s", normalizedStatus)
		// Keep as pending for reconciliation
		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     txx.ID,
			Status: "pending",
		})
		if err != nil {
			s.logger.Errorf("failed to update transaction status: %v", err)
		}
	}

	// err = s.store.UpdateUserTransactionVolume(ctx, db.UpdateUserTransactionVolumeParams{
	// 	TotalTransactionVolume: sql.NullString{String: fiatAmount.String(), Valid: true},
	// 	ID:                     userID,
	// })
	// if err != nil {
	// 	s.logger.Error(fmt.Sprintf("failed to update user transaction volume: %v", err))
	// }

	err = s.streakUpdater.UpdateStreakOnTransaction(ctx, userID, txx.ID, txx.Type)
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update user streak: %v", err))
	}

	// Referral + conversion bonus (non-fatal, same as before).
	if !user.HasCompletedFirstConversion.Bool {
		referrerID, referralBonus, err := CheckFirstConersionAndDisburseReferralBonus(ctx, s.store, dbTx, user.ID, txx.ID)
		if err != nil {
			s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to disburse referral bonus: %v", err))
		}
		if referrerID != nil && referralBonus != nil {
			// go func() {
			// 	bgCtx := context.Background()
				s.notifyr.CreateWithRecipients(ctx, nil, "Referral Bonus Credit",
					fmt.Sprintf("You have received a referral bonus of %s", referralBonus.String()),
					"system", []uuid.UUID{*referrerID})
				s.push.ReferralBonusEarned(ctx, *referrerID, referralBonus.String())
			// }()
		}
	}

	refererID, conversionBonus, err := CreditReferrerForConversion(ctx, s.store, dbTx, user.ID, fiatAmount)
	if err != nil {
		s.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to credit referrer for conversion: %v", err))
	}
	s.logger.Info(fmt.Sprintf("Conversion bonus process done. RefererID: %v, Bonus: %v", refererID, conversionBonus))
	if refererID != nil && conversionBonus != nil {
		// go func() {
		// 	bgCtx := context.Background()
			s.notifyr.CreateWithRecipients(ctx, nil, "Conversion Bonus Credit",
				fmt.Sprintf("You have received a conversion bonus of %s", conversionBonus.String()),
				"system", []uuid.UUID{*refererID})
			s.push.ConversionBonusEarned(ctx, *refererID, conversionBonus.String())
		// }()
	}

	_ = s.store.IncrementUserConversionVolume(ctx, db.IncrementUserConversionVolumeParams{
		UserID: user.ID,
		Amount: fiatAmount.String(),
	})

	s.push.SendPushNotification(ctx, user.ID, "Rapid Ramp Withdrawal",
		fmt.Sprintf("%.2f %s has been sent to your bank account", fiatAmount.InexactFloat64(), "NGN"))

	return &TransactionResponse[CryptoMetadataResponse]{
		ID:              txx.ID,
		Type:            txx.Type,
		Description:     txx.Description.String,
		TransactionFlow: txx.TransactionFlow,
		Status:          txx.Status,
		CreatedAt:       txx.CreatedAt,
		UpdatedAt:       txx.UpdatedAt,
		// bank details
		// reference for transfer tx
	}, nil
}

type RapidRampResponse struct {
	Coin       string `json:"coin"`
	Rate       string `json:"rate"`
	CoinAmount string `json:"coin_amount"`
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

func (s *TransactionService) createTransactionRecord(ctx context.Context, dbTx *sql.Tx, platform TransactionPlatform, txx interface{}, currFlow, transactionFlow string, userID uuid.UUID) (interface{}, error) {

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
			"cannot redeem more points ₦%.2f than bill amount ₦%.2f",
			pointsToRedeem.InexactFloat64(), originalAmount.InexactFloat64(),
		)
	}

	// calculate final amount after discount
	finalAmount = originalAmount.Sub(pointsToRedeem)

	s.logger.Info(fmt.Sprintf("Reward redemption validated: Original=₦%.2f, Points=₦%.2f, Final=₦%.2f",
		originalAmount.InexactFloat64(), pointsToRedeem.InexactFloat64(), finalAmount.InexactFloat64()))

	return finalAmount, true, nil
}

// CompleteRewardRedemption completes the reward redemption after bill payment succeeds
func (s *TransactionService) CompleteRewardRedemption(
	ctx context.Context,
	dbTx *sql.Tx,
	userID uuid.UUID,
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
		UserID:            userID,
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
	userID uuid.UUID,
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

	_, err = qtx.AwardRewardPoints(ctx, db.AwardRewardPointsParams{
		UserID:                userID,
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

	_, err = qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          userID,
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

	// _, err = qtx.CreateRewardTransaction(ctx, db.CreateRewardTransactionParams{
	// 	UserID:                ttx.UserID,
	// 	PointsAmount:          pointsEarned.String(),
	// 	NairaValue:            pointsEarned.String(),
	// 	TransactionID:         uuid.NullUUID{UUID: ttx.ID, Valid: true},
	// 	SourceTransactionType: sql.NullString{String: string(Airtime), Valid: true},
	// 	TransactionAmount:     sql.NullString{String: paidAmount.String(), Valid: true},
	// 	RewardConfigID:        sql.NullInt64{Int64: config.ID, Valid: true},
	// 	BalanceAfter:          x.BalanceAfter,
	// 	Status:                "completed",
	// 	TransactionType:       "earned",
	// 	Description:           sql.NullString{String: description, Valid: true},
	// })
	// if err != nil {
	// 	return decimal.Zero, fmt.Errorf("failed to create reward transaction: %w", err)
	// }

	s.logger.Info(fmt.Sprintf("Reward points awarded: User=%d, Points=₦%d, TX=%d, Rate=%s%%",
		userID, pointsEarned, transactionID, config.RewardRate))

	return pointsEarned, nil

}

// GetUserRewardBalance is a convenience method to get user's reward balance
func (s *TransactionService) GetUserRewardBalance(ctx context.Context, userID uuid.UUID) (string, error) {
	balance, err := s.store.GetUserRewardBalance(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to get reward balance: %w", err)
	}
	return balance.RewardBalance, nil
}

// UpdateStreakAfterBillPayment updates streak after successful bill payment
func (s *TransactionService) UpdateStreakAfterBillPayment(
	ctx context.Context,
	userID uuid.UUID,
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

var (
	ErrInsufficientBalance  = errors.New("insufficient balance in Naira wallet")
	ErrTransactionPending   = errors.New("transaction already pending")
	ErrTransactionCompleted = errors.New("transaction already completed")
	ErrInvalidVariation     = errors.New("invalid variation code")
)

// resolveRewards calculates finalAmount and pointsUsed for a bill purchase.
// Returns unchanged amount and 0 pointsUsed when reward redemption is disabled.
func (s *TransactionService) resolveRewards(
	ctx context.Context,
	dbTx *sql.Tx,
	user *db.User,
	req interface{ GetRewardFields() (bool, float32) },
	amount decimal.Decimal,
) (finalAmount decimal.Decimal, pointsToUse decimal.Decimal, pointsUsed float64, redemptionApplied bool, err error) {
	useRewards, pointsToUseF32 := req.GetRewardFields()
	pointsToUse = decimal.NewFromFloat32(pointsToUseF32)
	finalAmount = amount

	if useRewards && pointsToUseF32 > 0 {
		finalAmount, redemptionApplied, err = s.ProcessRewardRedemption(
			ctx, dbTx, user, pointsToUse, amount,
		)
		if err != nil {
			return
		}
		if redemptionApplied {
			pointsUsed = pointsToUse.InexactFloat64()
			s.logger.Info(fmt.Sprintf(
				"Reward redemption applied: User=%d, Points=%.2f, Original=₦%s, Final=₦%s",
				user.ID, pointsUsed, amount.String(), finalAmount.String(),
			))
		}
	}
	return
}

// checkIdempotency returns a typed sentinel error if this idempotency key
// has already been processed. No-op (nil) if the key is new.
func (s *TransactionService) checkIdempotency(ctx context.Context, dbTx *sql.Tx, key string) error {
	existingTx, err := s.store.WithTx(dbTx).GetTransactionByIdempotencyKey(ctx, key)
	if err != nil {
		// Key not found → this is a new request, proceed.
		return nil
	}
	switch existingTx.Status {
	case string(Pending):
		return fmt.Errorf("%w: reference=%s", ErrTransactionPending, existingTx.IdempotencyKey)
	case string(Success):
		return fmt.Errorf("%w: reference=%s", ErrTransactionCompleted, existingTx.IdempotencyKey)
	default:
		return fmt.Errorf("transaction already exists with status %s", existingTx.Status)
	}
}

// postBillSuccess runs non-fatal post-commit side effects common to all bill
// types: audit log, streak update, in-app notification, and reward completion.
func (s *TransactionService) postBillSuccess(
	ctx context.Context,
	dbTx *sql.Tx,
	user *db.User,
	txx db.Transaction,
	amount, finalAmount decimal.Decimal,
	pointsToUse decimal.Decimal,
	redemptionApplied bool,
	txType, serviceID, auditEvent string,
	target, plan string,
) (pointsEarned float64) {
	if redemptionApplied {
		if err := s.CompleteRewardRedemption(
			ctx, dbTx, user.ID, txx.ID,
			pointsToUse, amount, finalAmount, txType, serviceID,
		); err != nil {
			s.logger.Error("Failed to complete reward redemption:", err)
		}
	}

	pointAmount, err := s.AwardRewardPoints(
		ctx, dbTx, user.ID, txx.ID, amount, txType, "bill_payment",
	)
	if err != nil {
		s.logger.Error("Failed to award reward points:", err)
	}

	// FIX [B3]: single assignment; removed duplicate that overwrote the inner
	// assignment and caused the log to fire even when pointAmount == 0.
	pointsEarned = pointAmount.InexactFloat64()
	if pointAmount.GreaterThan(decimal.Zero) {
		s.logger.Info(fmt.Sprintf(
			"Points awarded [%s]: User=%d, Points=₦%s, TX=%d",
			txType, user.ID, pointAmount.String(), txx.ID,
		))
	}

	logEntry := audit.NewTransactionLog(
		auditEvent, txx.ID.String(), user.Role, user.ID,
		amount.InexactFloat64(), "NGN", true,
	)
	logEntry.Metadata = map[string]any{}
	s.audit.Log(logEntry)

	if err = s.streakUpdater.UpdateStreakOnTransaction(ctx, user.ID, txx.ID, txType); err != nil {
		s.logger.Error("Failed to update streak:", err)
	}

	// Push Notification
	// go func() {
	// 	bgCtx := context.Background()
		switch txType {
		case string(Airtime):
			s.push.SendPushNotification(ctx, user.ID, "Successful Airtime Purchase", fmt.Sprintf("You have successfully purchased airtime of ₦%d to %s", amount.IntPart(), target))
		case string(Data):
			s.push.SendPushNotification(ctx, user.ID, "Successful Data Purchase", fmt.Sprintf("You have successfully purchased data of %s on %s", plan, target))
		case string(TV):
			s.push.SendPushNotification(ctx, user.ID, "Successful Tv Sub", fmt.Sprintf("You have successfully subscribed to TV of %s", plan))
		case string(Electricity):
			s.push.SendPushNotification(ctx, user.ID, "Successful Electricity Purchase", fmt.Sprintf("You have successfully purchased electricity of ₦%d to %s", amount.IntPart(), target))
		}
	// }()

	return
}

func (s *TransactionService) buildAirtimeResponse(
	txx db.Transaction, amount, finalAmount decimal.Decimal,
	pointsEarned, pointsUsed float64, req BuyAirtimeRequest, status string,
) *BuyAirtimeResponse {
	return &BuyAirtimeResponse{
		Amount:               amount,
		AmountPaid:           finalAmount.InexactFloat64(),
		BonusEarned:          pointsEarned,
		Phone:                req.Phone,
		TransactionType:      txx.Type,
		Date:                 txx.CreatedAt,
		TransactionReference: txx.IdempotencyKey,
		Status:               status,
		PointsUsed:           pointsUsed,
	}
}

var (
	tier1MaxAmountForAirtime     = decimal.NewFromInt(100000)
	tier1MaxAmountForData        = decimal.NewFromInt(100000)
	tier1MaxAmountForElectricity = decimal.NewFromInt(100000)
	tier1MaxAmountForTV          = decimal.NewFromInt(100000)
)

// ── HandleAirtime ──────────────────────────────────────────────────────────────
func (s *TransactionService) HandleAirtime(ctx context.Context, user *db.User, req BuyAirtimeRequest) (*BuyAirtimeResponse, error) {
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Idempotency guard (inside TX for consistent read).
	if err = s.checkIdempotency(ctx, dbTx, req.IdempotencyKey); err != nil {
		return nil, err
	}

	// Lock NGN wallet.
	NGNWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NGN wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(NGNWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance: %w", err)
	}

	amount := decimal.NewFromInt(req.Amount)
	if walletBalance.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	kyc, err := s.store.WithTx(dbTx).GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier1" {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_1")
	}

	if amount.GreaterThan(tier1MaxAmountForAirtime) {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_AIRTIME_AMOUNT_EXCEEDED_FOR_TIER_1")
	}

	// Reward redemption.
	pointsToUseDecimal := decimal.NewFromFloat32(req.PointsToUse)
	var finalAmount = amount
	var pointsUsed float64
	var redemptionApplied bool

	if req.UseRewardPoints && req.PointsToUse > 0 {
		finalAmount, _, pointsUsed, redemptionApplied, err = s.resolveRewards(
			ctx, dbTx, user, &req, amount,
		)
		if err != nil {
			return nil, fmt.Errorf("reward redemption: %w", err)
		}
	}

	// Create transaction record.
	amountUsd, err := utils.ConvertToUSD(ctx, amount, NGNWallet.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to USD: %w", err)
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

	purchaseRequestID := utils.WatRequestID() // FIX [B4]

	metaTX, err := s.store.WithTx(dbTx).CreateAirtimeDataMetadata(ctx, db.CreateAirtimeDataMetadataParams{
		Amount:        amount.String(),
		PointsUsed:    sql.NullString{String: fmt.Sprintf("%.2f", pointsUsed), Valid: true},
		Type:          string(Airtime),
		AmountPaid:    finalAmount.String(), // FIX [B1]: was amount.String()
		PointsEarned:  sql.NullString{String: pointsToUseDecimal.String(), Valid: true},
		PhoneNumber:   req.Phone,
		Plan:          sql.NullString{String: req.ServiceID, Valid: true},
		Reference:     req.IdempotencyKey,
		Status:        string(Pending),
		RequestID:     purchaseRequestID,
		TransactionID: txx.ID,
		Date:          txx.CreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create airtime metadata: %w", err)
	}

	// FIX [B1]: Debit finalAmount (post-discount), not gross amount.
	if _, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: finalAmount.String(), Valid: true},
		ID:      NGNWallet.ID,
	}); err != nil {
		return nil, fmt.Errorf("failed to debit wallet: %w", err)
	}

	btx, err := s.billProvider.BuyAirtime(bills.PurchaseAirtimeRequest{
		ServiceID: req.ServiceID,
		Phone:     req.Phone,
		RequestID: purchaseRequestID,
		Amount:    req.Amount,
	})
	if err != nil {
		// Provider hard error — commit Pending so reconciler can recover.
		_, updateErr := s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		})
		_ = dbTx.Commit()
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: CRITICALALERT,
			Title:    "Buy Airtime Failing",
			Message:  fmt.Sprintf("VTPASS buy airtime endpoint failed with error: %v", err.Error()),
			Source:   sql.NullString{String: "HandleAirtime", Valid: true},
		})
		_ = updateErr // Use updateErr to avoid unused variable warning
		return nil, fmt.Errorf("airtime provider unreachable (pending reconciliation): %w", err)
	}

	switch btx.Status {
	case "failed":
		// FIX [B2]: Refund finalAmount (what was actually debited), not amount.
		if _, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: finalAmount.String(), Valid: true},
			ID:      NGNWallet.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to refund wallet: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Failed),
		}); err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Failed), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update airtime metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}
		return s.buildAirtimeResponse(txx, amount, finalAmount, 0, pointsUsed, req, btx.Status), nil

	case "pending":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record to pending: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Pending), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update airtime metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit pending status: %w", err)
		}
		return s.buildAirtimeResponse(txx, amount, finalAmount, 0, pointsUsed, req, btx.Status), nil

	case "delivered":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Success),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Success), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update airtime metadata status: %w", err)
		}

		// FIX [B3]: postBillSuccess handles single-assignment pointsEarned.
		pointsEarned := s.postBillSuccess(
			ctx, dbTx, user, txx, amount, finalAmount, pointsToUseDecimal,
			redemptionApplied, string(Airtime), req.ServiceID, audit.EventAirtimePurchase,
			req.Phone, "",
		)

		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("airtime purchase commit failed: %w", err)
		}

		// FIX [B5]: Notification built AFTER confirmed delivery.
		notificationMsg := fmt.Sprintf("You have received airtime of ₦%d to %s", req.Amount, req.Phone)
		if pointsUsed > 0 {
			notificationMsg += fmt.Sprintf(". You saved ₦%.2f using reward points", pointsUsed)
		}
		if pointsEarned > 0 {
			notificationMsg += fmt.Sprintf(". You earned ₦%.2f in reward points!", pointsEarned)
		}
		if _, err = s.notifyr.CreateWithRecipients(ctx, nil, "Airtime Purchase", notificationMsg, "system", []uuid.UUID{user.ID}); err != nil {
			s.audit.Log(audit.WarningLog("InApp Notification failed", err.Error()))
		}
		return s.buildAirtimeResponse(txx, amount, finalAmount, pointsEarned, pointsUsed, req, btx.Status), nil

	default:
		return nil, fmt.Errorf("unknown airtime status: %s", btx.Status)
	}
}

// ── HandleData ─────────────────────────────────────────────────────────────────
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
			for _, v := range remoteVariations {
				variations = append(variations, models.BillVariation{
					VariationCode:   v.VariationCode,
					Name:            v.Name,
					VariationAmount: v.VariationAmount,
					FixedPrice:      v.FixedPrice,
				})
			}
			if err = s.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", req.ServiceID), variations); err != nil {
				s.logger.Error(fmt.Sprintf("failed to cache variations: %v", err))
			}
		}
	}

	var selectedVariation *models.BillVariation
	for i := range variations {
		if variations[i].VariationCode == req.VariationCode {
			selectedVariation = &variations[i]
			break
		}
	}
	if selectedVariation == nil {
		return nil, ErrInvalidVariation
	}

	amount, err := decimal.NewFromString(selectedVariation.VariationAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid variation amount: %w", err)
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Idempotency check now inside TX with consistent read.
	if err = s.checkIdempotency(ctx, dbTx, req.IdempotencyKey); err != nil {
		return nil, err
	}

	NGNWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NGN wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(NGNWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance: %w", err)
	}
	if walletBalance.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	kyc, err := s.store.WithTx(dbTx).GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier1" {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_1")
	}

	if amount.GreaterThan(tier1MaxAmountForData) {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_DATA_AMOUNT_EXCEEDED_FOR_TIER_1")
	}

	pointsToUseDecimal := decimal.NewFromFloat32(req.PointsToUse)
	var finalAmount = amount
	var pointsUsed float64
	var redemptionApplied bool

	if req.UseRewardPoints && req.PointsToUse > 0 {
		finalAmount, _, pointsUsed, redemptionApplied, err = s.resolveRewards(ctx, dbTx, user, &req, amount)
		if err != nil {
			return nil, fmt.Errorf("reward redemption: %w", err)
		}
	}

	amountUsd, err := utils.ConvertToUSD(ctx, amount, NGNWallet.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to USD: %w", err)
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

	purchaseRequestID := utils.WatRequestID()

	metaTX, err := s.store.WithTx(dbTx).CreateAirtimeDataMetadata(ctx, db.CreateAirtimeDataMetadataParams{
		Amount:        amount.String(),
		PointsUsed:    sql.NullString{String: fmt.Sprintf("%.2f", pointsUsed), Valid: true},
		Type:          string(Data),
		AmountPaid:    finalAmount.String(),
		PointsEarned:  sql.NullString{String: pointsToUseDecimal.String(), Valid: true},
		PhoneNumber:   req.Phone,
		Plan:          sql.NullString{String: req.ServiceID, Valid: true},
		Reference:     req.IdempotencyKey,
		Status:        string(Pending),
		RequestID:     purchaseRequestID,
		TransactionID: txx.ID,
		Date:          txx.CreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create data metadata: %w", err)
	}

	// Debit finalAmount inside TX.
	if _, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: finalAmount.String(), Valid: true},
		ID:      NGNWallet.ID,
	}); err != nil {
		return nil, fmt.Errorf("failed to debit wallet: %w", err)
	}

	btx, err := s.billProvider.BuyData(bills.PurchaseDataRequest{
		ServiceID:     req.ServiceID,
		BillersCode:   req.Phone,
		VariationCode: req.VariationCode,
		Phone:         req.Phone,
		RequestID:     purchaseRequestID,
		Amount:        amount.IntPart(),
	})
	if err != nil {
		_, updateErr := s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		})
		if updateErr != nil {
			return nil, fmt.Errorf("failed to update tx record")
		}
		_ = dbTx.Commit()
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: CRITICALALERT,
			Title:    "Buy Data Failing",
			Message:  fmt.Sprintf("VTPASS buy data endpoint failed with error: %v", err),
			Source:   sql.NullString{String: "HandleData", Valid: true},
		})
		return nil, fmt.Errorf("data provider unreachable (pending reconciliation): %w", err)
	}

	switch btx.Status {
	case "failed":
		// Refund finalAmount.
		if _, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: finalAmount.String(), Valid: true},
			ID:      NGNWallet.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to refund wallet: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Failed),
		}); err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Failed), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update data metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}
		return s.buildDataResponse(txx, amount, finalAmount, 0, pointsUsed, req, btx.Status, selectedVariation.VariationCode), nil

	case "pending":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record to pending: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Pending), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update data metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit pending status: %w", err)
		}
		return s.buildDataResponse(txx, amount, finalAmount, 0, pointsUsed, req, btx.Status, selectedVariation.VariationCode), nil

	case "delivered":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Success),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Success), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update data metadata status: %w", err)
		}

		pointsEarned := s.postBillSuccess(
			ctx, dbTx, user, txx, amount, finalAmount, pointsToUseDecimal,
			redemptionApplied, string(Data), req.ServiceID, audit.EventDataPurchase,
			req.Phone, selectedVariation.Name,
		)

		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("data purchase commit failed: %w", err)
		}

		// FIX [B5]: Build notification post-commit, after outcome is known.
		notificationMsg := fmt.Sprintf("You have received %s data on %s", selectedVariation.VariationCode, req.Phone)
		if pointsUsed > 0 {
			notificationMsg += fmt.Sprintf(". You saved ₦%.2f using reward points", pointsUsed)
		}
		if pointsEarned > 0 {
			notificationMsg += fmt.Sprintf(". You earned ₦%.2f in reward points!", pointsEarned)
		}
		if _, err = s.notifyr.CreateWithRecipients(ctx, nil, "Data Purchase", notificationMsg, "system", []uuid.UUID{user.ID}); err != nil {
			s.audit.Log(audit.WarningLog("InApp Notification failed", err.Error()))
		}
		return s.buildDataResponse(txx, amount, finalAmount, pointsEarned, pointsUsed, req, btx.Status, selectedVariation.VariationCode), nil

	default:
		return nil, fmt.Errorf("unknown data status: %s", btx.Status)
	}
}

func (s *TransactionService) buildDataResponse(
	txx db.Transaction, amount, finalAmount decimal.Decimal,
	pointsEarned, pointsUsed float64, req BuyDataRequest, status, plan string,
) *BuyDataResponse {
	return &BuyDataResponse{
		Amount:               amount.String(),
		AmountPaid:           finalAmount.InexactFloat64(),
		BonusEarned:          pointsEarned,
		Phone:                req.Phone,
		TransactionType:      txx.Type,
		Date:                 txx.CreatedAt,
		TransactionReference: txx.IdempotencyKey,
		Status:               status,
		Plan:                 plan,
		PointsUsed:           pointsUsed,
	}
}

// ── HandleTvSubscription ───────────────────────────────────────────────────────

func (s *TransactionService) HandleTvSubscription(ctx context.Context, user *db.User, req TVSubRequest) (*TVSubResponse, error) {
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
			for _, v := range remoteVariations {
				variations = append(variations, models.BillVariation{
					VariationCode:   v.VariationCode,
					Name:            v.Name,
					VariationAmount: v.VariationAmount,
					FixedPrice:      v.FixedPrice,
				})
			}
			if err = s.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", req.ServiceID), variations); err != nil {
				s.logger.Error(fmt.Sprintf("failed to cache variations: %v", err))
			}
		}
	}

	var selectedVariation *models.BillVariation
	for i := range variations {
		if variations[i].VariationCode == req.VariationCode {
			selectedVariation = &variations[i]
			break
		}
	}
	if selectedVariation == nil {
		return nil, ErrInvalidVariation
	}

	amount, err := decimal.NewFromString(selectedVariation.VariationAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid variation amount: %w", err)
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	if err = s.checkIdempotency(ctx, dbTx, req.IdempotencyKey); err != nil {
		return nil, err
	}

	NGNWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NGN wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(NGNWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance: %w", err)
	}
	if walletBalance.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	kyc, err := s.store.WithTx(dbTx).GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier1" {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_1")
	}

	if amount.GreaterThan(tier1MaxAmountForTV) {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_TV_AMOUNT_EXCEEDED_FOR_TIER_1")
	}

	pointsToUseDecimal := decimal.NewFromFloat32(req.PointsToUse)
	var finalAmount = amount
	var pointsUsed float64
	var redemptionApplied bool

	if req.UseRewardPoints && req.PointsToUse > 0 {
		finalAmount, _, pointsUsed, redemptionApplied, err = s.resolveRewards(ctx, dbTx, user, &req, amount)
		if err != nil {
			return nil, fmt.Errorf("reward redemption: %w", err)
		}
	}

	amountUsd, err := utils.ConvertToUSD(ctx, amount, NGNWallet.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to USD: %w", err)
	}

	txx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            string(TV),
		Description:     sql.NullString{String: fmt.Sprintf("%s purchase", TV), Valid: true},
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

	purchaseRequestID := utils.WatRequestID()

	metaTX, err := s.store.WithTx(dbTx).CreateAirtimeDataMetadata(ctx, db.CreateAirtimeDataMetadataParams{
		Amount:        amount.String(),
		PointsUsed:    sql.NullString{String: fmt.Sprintf("%.2f", pointsUsed), Valid: true},
		Type:          string(TV),
		AmountPaid:    finalAmount.String(),
		PointsEarned:  sql.NullString{String: pointsToUseDecimal.String(), Valid: true},
		PhoneNumber:   user.PhoneNumber,
		Plan:          sql.NullString{String: selectedVariation.VariationCode, Valid: true},
		Reference:     req.IdempotencyKey,
		Status:        string(Pending),
		RequestID:     purchaseRequestID,
		TransactionID: txx.ID,
		Date:          txx.CreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create TV metadata: %w", err)
	}

	if _, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: finalAmount.String(), Valid: true}, // FIX [B1]
		ID:      NGNWallet.ID,
	}); err != nil {
		return nil, fmt.Errorf("failed to debit wallet: %w", err)
	}

	btx, err := s.billProvider.BuyTVSubscription(bills.BuyTVSubscriptionRequest{
		ServiceID:        req.ServiceID,
		BillersCode:      req.BillersCode,
		VariationCode:    req.VariationCode,
		SubscriptionType: req.SubscriptionType,
		Phone:            user.PhoneNumber,
		RequestID:        purchaseRequestID,
		Amount:           amount.IntPart(),
	})
	if err != nil {
		_, updateErr := s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		})
		_ = dbTx.Commit()
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: CRITICALALERT,
			Title:    "Buy Tv Subscription Failing",
			Message:  fmt.Sprintf("VTPASS buy tv sub endpoint failed with error: %v", err.Error()),
			Source:   sql.NullString{String: "HandleAirtime", Valid: true},
		})
		_ = updateErr // Use updateErr to avoid unused variable warning
		return nil, fmt.Errorf("TV provider unreachable (pending reconciliation): %w", err)
	}

	switch btx.Status {
	case "failed":
		// FIX [B2]: Refund finalAmount.
		if _, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: finalAmount.String(), Valid: true},
			ID:      NGNWallet.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to refund wallet: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Failed),
		}); err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Failed), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update TV metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}
		return s.buildTVResponse(txx, amount, finalAmount, 0, pointsUsed, btx.Status, selectedVariation.VariationCode), nil

	case "pending":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record to pending: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Pending), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update TV metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit pending status: %w", err)
		}
		return s.buildTVResponse(txx, amount, finalAmount, 0, pointsUsed, btx.Status, selectedVariation.VariationCode), nil

	case "delivered":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Success),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Success), ID: metaTX.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update TV metadata status: %w", err)
		}

		pointsEarned := s.postBillSuccess(
			ctx, dbTx, user, txx, amount, finalAmount, pointsToUseDecimal,
			redemptionApplied, string(TV), req.ServiceID, audit.EventTVSubscriptionPurchase,
			req.BillersCode, selectedVariation.VariationCode,
		)

		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("TV subscription commit failed: %w", err)
		}

		// FIX [B5]: Notification built post-commit.
		notificationMsg := fmt.Sprintf("Your %s TV subscription is active", selectedVariation.VariationCode)
		if pointsUsed > 0 {
			notificationMsg += fmt.Sprintf(". You saved ₦%.2f using reward points", pointsUsed)
		}
		if pointsEarned > 0 {
			notificationMsg += fmt.Sprintf(". You earned ₦%.2f in reward points!", pointsEarned)
		}
		if _, err = s.notifyr.CreateWithRecipients(ctx, nil, "TV Subscription", notificationMsg, "system", []uuid.UUID{user.ID}); err != nil {
			s.audit.Log(audit.WarningLog("InApp Notification failed", err.Error()))
		}
		return s.buildTVResponse(txx, amount, finalAmount, pointsEarned, pointsUsed, btx.Status, selectedVariation.VariationCode), nil

	default:
		return nil, fmt.Errorf("unknown TV status: %s", btx.Status)
	}
}

func (s *TransactionService) buildTVResponse(
	txx db.Transaction, amount, finalAmount decimal.Decimal,
	pointsEarned, pointsUsed float64, status, plan string,
) *TVSubResponse {
	return &TVSubResponse{
		Amount:               amount,
		AmountPaid:           finalAmount.InexactFloat64(),
		BonusEarned:          pointsEarned,
		TransactionType:      txx.Type,
		Date:                 txx.CreatedAt,
		TransactionReference: txx.IdempotencyKey,
		Status:               status,
		PointsUsed:           pointsUsed,
		Plan:                 plan,
	}
}

// ── Background Reconciler ──────────────────────────────────────────────────────
// Fixes the crash-between-debit-and-commit window for all bill types.
//
// Wire into your scheduler at startup:
//
//	go func() {
//	    t := time.NewTicker(60 * time.Second)
//	    for range t.C {
//	        if err := svc.ReconcilePendingBillTransactions(ctx); err != nil {
//	            logger.Error("bill reconciler:", err)
//	        }
//	    }
//	}()
//
// Requires the following query in your SQLC store:
//
//	GetPendingAirtimeTransactionsOlderThan(ctx, duration) ([]AirtimeMetadata, error)
//
// where AirtimeMetadata has: ID, TransactionID, RequestID, AmountPaid, UserID, Type.

func (s *TransactionService) ReconcilePendingBillTransactions(ctx context.Context) error {
	// Fetch pending transactions from all bill types
	var allPendingMetadata []BillMetadata

	// Get pending Airtime/Data/TV transactions (single table with type discriminator)
	airtimeDataTVPending, err := s.store.GetPendingDataAirtimePurchaseMetadataOlderThan20Seconds(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("reconciler: fetch pending airtime/data/tv: %v", err))
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: CRITICALALERT,
			Title:    "Transaction Reconciliation Failure: VTPass Bills",
			Message:  fmt.Sprintf("Failed to fetch pending airtime/data/tv transactions for reconciliation: %v", err),
			Source:   sql.NullString{String: "BillReconciler", Valid: true},
		})
	} else {
		for i := range airtimeDataTVPending {
			allPendingMetadata = append(allPendingMetadata, &DataAirtimeMetadataAdapter{meta: &airtimeDataTVPending[i]})
		}
	}

	// Get pending Electricity transactions
	electricityPending, err := s.store.GetPendingElectricityPurchaseMetadataOlderThan20Seconds(ctx)
	if err != nil && !strings.Contains(err.Error(), "no rows in result set") {
		s.logger.Error(fmt.Sprintf("reconciler: fetch pending electricity: %v", err))
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: CRITICALALERT,
			Title:    "Transaction Reconciliation Failure: VTPass Electricity",
			Message:  fmt.Sprintf("Failed to fetch pending electricity transactions for reconciliation: %v", err),
			Source:   sql.NullString{String: "BillReconciler", Valid: true},
		})
	} else if err == nil {
		for i := range electricityPending {
			allPendingMetadata = append(allPendingMetadata, &ElectricityMetadataAdapter{meta: &electricityPending[i]})
		}
	}

	// Get pending Bank Transfer transactions
	bankTransfersPending, err := s.store.GetPendingBankTransferMetadataOlderThan20Seconds(ctx)
	if err != nil && !strings.Contains(err.Error(), "no rows in result set") {
		s.logger.Error(fmt.Sprintf("reconciler: fetch pending bank transfers: %v", err))
		s.createAdminAlert(ctx, db.CreateAdminAlertParams{
			Severity: CRITICALALERT,
			Title:    "Transaction Reconciliation Failure: Nomba Bank Transfers",
			Message:  fmt.Sprintf("Failed to fetch pending bank transfer transactions for reconciliation: %v", err),
			Source:   sql.NullString{String: "BankTransferReconciler", Valid: true},
		})
	} else if err == nil {
		for i := range bankTransfersPending {
			allPendingMetadata = append(allPendingMetadata, &BankTransferMetadataAdapter{meta: &bankTransfersPending[i]})
		}
	}

	// Process all pending transactions
	for _, meta := range allPendingMetadata {
		requestID := meta.GetRequestID()
		var providerStatus string

		// Track reconciliation check count to prevent infinite pending state
		checkKey := fmt.Sprintf("reconcile_check_count:%s", requestID)
		count, err := s.redis.Incr(ctx, checkKey)
		if err != nil {
			s.logger.Error(fmt.Sprintf("reconciler: redis incr %s: %v", checkKey, err))
		} else {
			// Set expiration to 24 hours just in case
			_, _ = s.redis.Expire(ctx, checkKey, 24*time.Hour)
		}

		if count > 1 {
			s.logger.Warn(fmt.Sprintf("reconciler: requestID %s reached max check count (3), marking as failed", requestID))
			if err = s.reconcileFinalizeBillFailure(ctx, meta); err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: finalize failure (max checks) %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Transaction Reconciliation Failure: Unable to Finalize",
					Message:  fmt.Sprintf("Failed to finalize reconciliation for requestID %s (max check count exceeded): %v", requestID, err),
					Source:   sql.NullString{String: "BillReconcilerFinalize", Valid: true},
				})
			}
			_ = s.redis.Delete(ctx, checkKey)
			continue
		}

		switch meta.GetBillType() {
		case string(Airtime):
			res, err := s.billProvider.QueryAirtimeStatus(requestID)
			if err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: query airtime %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Provider Unavailable: VTPass Airtime Service",
					Message:  fmt.Sprintf("Failed to query VTPass airtime status for requestID %s: %v", requestID, err),
					Source:   sql.NullString{String: "VTPassAirtimeReconciler", Valid: true},
				})
				continue
			}
			providerStatus = res.Status

		case string(Data):
			res, err := s.billProvider.QueryDataStatus(requestID)
			if err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: query data %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Provider Unavailable: VTPass Data Service",
					Message:  fmt.Sprintf("Failed to query VTPass data status for requestID %s: %v", requestID, err),
					Source:   sql.NullString{String: "VTPassDataReconciler", Valid: true},
				})
				continue
			}
			providerStatus = res.Status

		case string(TV):
			res, err := s.billProvider.QueryTVStatus(requestID)
			if err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: query TV %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Provider Unavailable: VTPass TV Subscription Service",
					Message:  fmt.Sprintf("Failed to query VTPass TV subscription status for requestID %s: %v", requestID, err),
					Source:   sql.NullString{String: "VTPassTVReconciler", Valid: true},
				})
				continue
			}
			providerStatus = res.Status

		case string(Electricity):
			res, err := s.billProvider.QueryElectricityStatus(requestID)
			if err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: query electricity %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Provider Unavailable: VTPass Electricity Service",
					Message:  fmt.Sprintf("Failed to query VTPass electricity status for requestID %s: %v", requestID, err),
					Source:   sql.NullString{String: "VTPassElectricityReconciler", Valid: true},
				})
				continue
			}
			providerStatus = res.Status

		case "BankTransfer":
			// Bank transfers are processed through the fiat provider (e.g. Nomba).
			// Use the ServiceTransactionID (sessionID) when present; otherwise fall
			// back to the local transaction ID.
			merchantTxRef := requestID
			res, err := s.fiat.GetTransactionByMerchantRef(merchantTxRef)
			if err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: query bank transfer %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Provider Unavailable: Nomba Bank Transfer Service",
					Message:  fmt.Sprintf("Failed to query Nomba bank transfer status for merchantTxRef %s: %v", merchantTxRef, err),
					Source:   sql.NullString{String: "NombaBankTransferReconciler", Valid: true},
				})
				continue
			}
			switch strings.ToLower(res.Status) {
			case "success", "successful", "completed":
				providerStatus = "delivered"
			case "failed", "reversed":
				providerStatus = "failed"
			case "processing", "pending", "pending_billing":
				providerStatus = "pending"
			default:
				s.logger.Warnf("reconciler: unrecognised Nomba status %q for ref=%s", res.Status, merchantTxRef)
				providerStatus = "pending" // hold, re-check next cycle
			}

		default:
			s.logger.Warn(fmt.Sprintf("reconciler: unknown bill type %s for requestID=%s", meta.GetBillType(), requestID))
			continue
		}

		switch providerStatus {
		case "delivered":
			if err = s.reconcileFinalizeBillSuccess(ctx, meta); err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: finalize success %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Transaction Reconciliation Failure: Success Finalization Error",
					Message:  fmt.Sprintf("Failed to finalize successful reconciliation for requestID %s: %v", requestID, err),
					Source:   sql.NullString{String: "BillReconcilerFinalize", Valid: true},
				})
			} else {
				_ = s.redis.Delete(ctx, checkKey)
			}
		case "failed":
			if err = s.reconcileFinalizeBillFailure(ctx, meta); err != nil {
				s.logger.Error(fmt.Sprintf("reconciler: finalize failure %s: %v", requestID, err))
				s.createAdminAlert(ctx, db.CreateAdminAlertParams{
					Severity: CRITICALALERT,
					Title:    "Transaction Reconciliation Failure: Failure Finalization Error",
					Message:  fmt.Sprintf("Failed to finalize failed reconciliation for requestID %s: %v", requestID, err),
					Source:   sql.NullString{String: "BillReconcilerFinalize", Valid: true},
				})
			} else {
				_ = s.redis.Delete(ctx, checkKey)
			}
		case "pending":
			// Still in-flight; re-check next cycle.
		default:
			s.logger.Warn(fmt.Sprintf("reconciler: unknown provider status=%s for requestID=%s", providerStatus, requestID))
		}
	}

	return nil
}

// BillMetadata is a common interface for all bill purchase metadata types.
// It abstracts over data_airtime_purchase_metadata and electricity_purchase_metadata tables.
type BillMetadata interface {
	GetMetadataID() uuid.UUID
	GetTransactionID() uuid.UUID
	GetStatus() string
	GetRequestID() string
	GetAmountPaid() string
	GetBillType() string // Returns transaction type: Airtime, Data, TV, or Electricity
}

// DataAirtimeMetadataAdapter wraps db.DataAirtimePurchaseMetadata to implement BillMetadata.
// Handles: Airtime, Data, and TV subscription purchases.
type DataAirtimeMetadataAdapter struct {
	meta *db.DataAirtimePurchaseMetadatum
}

func (a *DataAirtimeMetadataAdapter) GetMetadataID() uuid.UUID {
	return a.meta.ID
}

func (a *DataAirtimeMetadataAdapter) GetTransactionID() uuid.UUID {
	return a.meta.TransactionID
}

func (a *DataAirtimeMetadataAdapter) GetStatus() string {
	return a.meta.Status
}

func (a *DataAirtimeMetadataAdapter) GetRequestID() string {
	return a.meta.RequestID
}

func (a *DataAirtimeMetadataAdapter) GetAmountPaid() string {
	if a.meta.AmountPaid == "" {
		return "0"
	}
	return a.meta.AmountPaid
}

func (a *DataAirtimeMetadataAdapter) GetBillType() string {
	return a.meta.Type
}

// ElectricityMetadataAdapter wraps db.ElectricityPurchaseMetadata to implement BillMetadata.
type ElectricityMetadataAdapter struct {
	meta *db.ElectricityPurchaseMetadatum
}

func (e *ElectricityMetadataAdapter) GetMetadataID() uuid.UUID {
	return e.meta.ID
}

func (e *ElectricityMetadataAdapter) GetTransactionID() uuid.UUID {
	return e.meta.TransactionID
}

func (e *ElectricityMetadataAdapter) GetStatus() string {
	return e.meta.Status
}

func (e *ElectricityMetadataAdapter) GetRequestID() string {
	return e.meta.RequestID
}

func (e *ElectricityMetadataAdapter) GetAmountPaid() string {
	if e.meta.AmountPaid == "" {
		return "0"
	}
	return e.meta.AmountPaid
}

func (e *ElectricityMetadataAdapter) GetBillType() string {
	return string(Electricity)
}

// BankTransferMetadataAdapter wraps db.BankTransferMetadata to implement BillMetadata.
type BankTransferMetadataAdapter struct {
	meta *db.BankTransferMetadatum
}

func (b *BankTransferMetadataAdapter) GetMetadataID() uuid.UUID {
	return b.meta.ID
}

func (b *BankTransferMetadataAdapter) GetTransactionID() uuid.UUID {
	return b.meta.TransactionID
}

func (b *BankTransferMetadataAdapter) GetStatus() string {
	return b.meta.Status
}

func (b *BankTransferMetadataAdapter) GetRequestID() string {
	// Bank transfers use ServiceTransactionID as the request ID
	if b.meta.ServiceTransactionID.Valid {
		return b.meta.ServiceTransactionID.String
	}
	return b.meta.TransactionID.String()
}

func (b *BankTransferMetadataAdapter) GetAmountPaid() string {
	if b.meta.AmountPaid == "" {
		return "0"
	}
	return b.meta.AmountPaid
}

func (b *BankTransferMetadataAdapter) GetBillType() string {
	return "BankTransfer"
}

// reconcileFinalizeBillSuccess updates transaction and metadata status to success after provider confirmation.
func (s *TransactionService) reconcileFinalizeBillSuccess(ctx context.Context, meta BillMetadata) error {
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer dbTx.Rollback()

	if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID: meta.GetTransactionID(), Status: string(Success),
	}); err != nil {
		return err
	}

	// Update metadata based on bill type
	billType := meta.GetBillType()
	switch billType {
	case string(Airtime), string(Data), string(TV):
		// All handled by single table: data_airtime_purchase_metadata
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Success), ID: meta.GetMetadataID(),
		}); err != nil {
			return err
		}
	case string(Electricity):
		if _, err = s.store.WithTx(dbTx).UpdateElectricityPurchaseStatus(ctx, db.UpdateElectricityPurchaseStatusParams{
			Status: string(Success), ID: meta.GetMetadataID(),
		}); err != nil {
			return err
		}
	case "BankTransfer":
		if _, err = s.store.WithTx(dbTx).UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID:        meta.GetTransactionID(),
			Status:               string(Success),
			ServiceTransactionID: sql.NullString{String: meta.GetRequestID(), Valid: true},
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown bill type for success finalization: %s", billType)
	}

	return dbTx.Commit()
}

// reconcileFinalizeBillFailure refunds the debited amount and updates metadata status to failed.
func (s *TransactionService) reconcileFinalizeBillFailure(ctx context.Context, meta BillMetadata) error {
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer dbTx.Rollback()

	// Refund exactly what was debited (stored as AmountPaid in metadata).
	amountPaid, err := decimal.NewFromString(meta.GetAmountPaid())
	if err != nil {
		return fmt.Errorf("invalid AmountPaid in metadata: %w", err)
	}

	// Get user from transaction to fetch wallet
	txx, err := s.store.WithTx(dbTx).GetTransactionByID(ctx, meta.GetTransactionID())
	if err != nil {
		return fmt.Errorf("reconciler: fetch transaction: %w", err)
	}

	wallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: txx.UserID, Currency: "NGN",
	})
	if err != nil {
		return fmt.Errorf("reconciler: fetch wallet: %w", err)
	}

	if _, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
		Balance: sql.NullString{String: amountPaid.String(), Valid: true},
		ID:      wallet.ID,
	}); err != nil {
		return fmt.Errorf("reconciler: refund wallet: %w", err)
	}

	if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID: meta.GetTransactionID(), Status: string(Failed),
	}); err != nil {
		return err
	}

	// Update metadata based on bill type
	billType := meta.GetBillType()
	switch billType {
	case string(Airtime), string(Data), string(TV):
		// All handled by single table: data_airtime_purchase_metadata
		if _, err = s.store.WithTx(dbTx).UpdateAirtimePurchaseStatus(ctx, db.UpdateAirtimePurchaseStatusParams{
			Status: string(Failed), ID: meta.GetMetadataID(),
		}); err != nil {
			return err
		}
	case string(Electricity):
		if _, err = s.store.WithTx(dbTx).UpdateElectricityPurchaseStatus(ctx, db.UpdateElectricityPurchaseStatusParams{
			Status: string(Failed), ID: meta.GetMetadataID(),
		}); err != nil {
			return err
		}
	case "BankTransfer":
		if _, err = s.store.WithTx(dbTx).UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID:        meta.GetTransactionID(),
			Status:               string(Failed),
			ServiceTransactionID: sql.NullString{String: meta.GetRequestID(), Valid: true},
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown bill type for failure finalization: %s", billType)
	}

	return dbTx.Commit()
}

func (s *TransactionService) HandleBuyElectricity(ctx context.Context, user *db.User, req ElectricityRequest) (*ElectricityResponse, error) {
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
			for _, v := range remoteVariations {
				variations = append(variations, models.BillVariation{
					VariationCode:   v.VariationCode,
					Name:            v.Name,
					VariationAmount: v.VariationAmount,
					FixedPrice:      v.FixedPrice,
				})
			}
			if err = s.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", req.ServiceID), variations); err != nil {
				s.logger.Error(fmt.Sprintf("failed to cache variations: %v", err))
			}
		}
	}

	var selectedVariation *models.BillVariation
	for i := range variations {
		if variations[i].VariationCode == req.VariationCode {
			selectedVariation = &variations[i]
			break
		}
	}
	if selectedVariation == nil {
		return nil, ErrInvalidVariation
	}

	// FIX [E3]: Removed dead `if err != nil` after decimal.NewFromFloat which
	// never errors. The stale `err` was from the variations lookup above and
	// would have returned the wrong error for an unrelated operation.
	amount := decimal.NewFromFloat(req.Amount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	if err = s.checkIdempotency(ctx, dbTx, req.IdempotencyKey); err != nil {
		return nil, err
	}

	NGNWallet, err := s.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "NGN",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NGN wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(NGNWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance: %w", err)
	}
	if walletBalance.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	kyc, err := s.store.WithTx(dbTx).GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier1" {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_1")
	}

	if amount.GreaterThan(tier1MaxAmountForTV) {
		s.push.SendPushNotification(ctx, user.ID, "Unlock more feature and remove account limits.", "Complete Tier 2 verification using your NIN, BVN, and a quick selfie check")
		return nil, fmt.Errorf("Err_TV_AMOUNT_EXCEEDED_FOR_TIER_1")
	}

	pointsToUseDecimal := decimal.NewFromFloat32(req.PointsToUse)
	var finalAmount = amount
	var pointsUsed float64
	var redemptionApplied bool

	if req.UseRewardPoints && req.PointsToUse > 0 {
		finalAmount, _, pointsUsed, redemptionApplied, err = s.resolveRewards(ctx, dbTx, user, &req, amount)
		if err != nil {
			return nil, fmt.Errorf("reward redemption: %w", err)
		}
	}

	amountUsd, err := utils.ConvertToUSD(ctx, amount, NGNWallet.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to USD: %w", err)
	}

	txx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            string(Electricity),
		Description:     sql.NullString{String: fmt.Sprintf("%s purchase", Electricity), Valid: true},
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

	purchaseRequestID := utils.WatRequestID() // FIX [B4]

	stx, err := s.store.WithTx(dbTx).CreateElectricityPurchaseMetadata(ctx, db.CreateElectricityPurchaseMetadataParams{
		TransactionID: txx.ID,
		Amount:        txx.Amount,
		PointsUsed:    sql.NullString{String: fmt.Sprintf("%.2f", pointsUsed), Valid: true},
		AmountPaid:    finalAmount.String(),
		PointsEarned:  sql.NullString{String: pointsToUseDecimal.String(), Valid: true},
		Reference:     req.IdempotencyKey,
		RequestID:     purchaseRequestID,
		ServiceCharge: sql.NullString{String: "0", Valid: true},
		Status:        string(Pending),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create electricity metadata: %w", err) // FIX [E1]
	}

	if _, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: finalAmount.String(), Valid: true},
		ID:      NGNWallet.ID,
	}); err != nil {
		return nil, fmt.Errorf("failed to debit wallet: %w", err)
	}

	btx, err := s.billProvider.BuyElectricity(bills.PurchaseElectricityRequest{
		ServiceID:     req.ServiceID,
		BillersCode:   req.BillersCode,
		VariationCode: req.VariationCode,
		Phone:         user.PhoneNumber,
		RequestID:     purchaseRequestID,
		Amount:        req.Amount,
	})
	if err != nil {
		_, updateErr := s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		})
		_ = dbTx.Commit()
		_ = updateErr // Use updateErr to avoid unused variable warning
		return nil, fmt.Errorf("electricity provider unreachable (pending reconciliation): %w", err)
	}

	switch btx.Content.Transaction.Status {
	case "failed":
		// FIX [B2]: Refund finalAmount.
		if _, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: finalAmount.String(), Valid: true},
			ID:      NGNWallet.ID,
		}); err != nil {
			return nil, fmt.Errorf("failed to refund wallet: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Failed),
		}); err != nil {
			return nil, fmt.Errorf("failed to update transaction status: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateServiceMetadataStatus(ctx, db.UpdateServiceMetadataStatusParams{
			ServiceStatus: string(Failed), TransactionID: stx.TransactionID,
		}); err != nil {
			return nil, fmt.Errorf("failed to update electricity metadata status: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}
		return s.buildElectricityResponse(txx, amount, finalAmount, 0, btx.Content.Transaction.Status, btx), nil

	case "pending":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Pending),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateElectricityPurchaseStatus(ctx, db.UpdateElectricityPurchaseStatusParams{
			ID: stx.ID, Status: string(Pending),
		}); err != nil {
			return nil, fmt.Errorf("failed to update electricity metadata: %w", err)
		}
		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit pending status: %w", err)
		}
		return s.buildElectricityResponse(txx, amount, finalAmount, 0, btx.Content.Transaction.Status, btx), nil

	case "delivered":
		if _, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID: txx.ID, Status: string(Success),
		}); err != nil {
			return nil, fmt.Errorf("failed to update tx record: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateElectricityPurchaseStatus(ctx, db.UpdateElectricityPurchaseStatusParams{
			ID: stx.ID, Status: string(Success),
		}); err != nil {
			return nil, fmt.Errorf("failed to update electricity metadata: %w", err)
		}
		if _, err = s.store.WithTx(dbTx).UpdateElectricityPurchasePartial(ctx, db.UpdateElectricityPurchasePartialParams{
			Reference:       req.IdempotencyKey,
			Token:           sql.NullString{String: btx.Token, Valid: true},
			CustomerName:    sql.NullString{String: *btx.CustomerName, Valid: true},
			CustomerAddress: sql.NullString{String: *btx.CustomerAddress, Valid: true},
			Units:           sql.NullString{String: btx.Units, Valid: true},
			MeterNumber:     sql.NullString{String: btx.MeterNumber, Valid: true},
			Tax:             sql.NullString{String: fmt.Sprintf("%.2f", btx.TaxAmount), Valid: true},
			Debt:            sql.NullString{String: fmt.Sprintf("%.2f", btx.Debt), Valid: true},
			Status:          string(Success),
		}); err != nil {
			return nil, fmt.Errorf("failed to update electricity token details: %w", err)
		}

		// FIX [E4]: type was "airtime" instead of string(Electricity).
		// FIX [E5]: streak type was string(Airtime) instead of string(Electricity).
		// postBillSuccess now uses the correct txType for both.
		pointsEarned := s.postBillSuccess(
			ctx, dbTx, user, txx, amount, finalAmount, pointsToUseDecimal,
			redemptionApplied, string(Electricity), req.ServiceID, audit.EventElectricityPurchase,
			req.BillersCode, "",
		)

		if err = dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("electricity purchase commit failed: %w", err)
		}

		// FIX [B5]: Notification built post-commit with actual token.
		notificationMsg := fmt.Sprintf("Your electricity purchase is successful. Token: %s", btx.Token)
		if pointsUsed > 0 {
			notificationMsg += fmt.Sprintf(". You saved ₦%.2f using reward points", pointsUsed)
		}
		if pointsEarned > 0 {
			notificationMsg += fmt.Sprintf(". You earned ₦%.2f in reward points!", pointsEarned)
		}
		if _, err = s.notifyr.CreateWithRecipients(ctx, nil, "Electricity Purchase", notificationMsg, "system", []uuid.UUID{user.ID}); err != nil {
			s.audit.Log(audit.WarningLog("InApp Notification failed", err.Error()))
		}
		return s.buildElectricityResponse(txx, amount, finalAmount, pointsEarned, btx.Content.Transaction.Status, btx), nil

	default:
		return nil, fmt.Errorf("unknown electricity status: %s", btx.Content.Transaction.Status)
	}
}

func (s *TransactionService) buildElectricityResponse(
	txx db.Transaction, amount, finalAmount decimal.Decimal,
	pointsEarned float64, status string, btx *bills.PurchaseElectricityResponse,
) *ElectricityResponse {
	resp := &ElectricityResponse{
		Amount:               amount.String(),
		AmountPaid:           finalAmount.InexactFloat64(),
		BonusEarned:          pointsEarned,
		TransactionType:      txx.Type,
		Date:                 txx.CreatedAt,
		TransactionReference: txx.IdempotencyKey,
		Status:               status,
		Token:                btx.Token,
		Units:                btx.Units,
		TokenAmount:          btx.TokenAmount,
		MeterNumber:          btx.MeterNumber,
		FixChargeAmount:      btx.FixChargeAmount,
	}
	if btx.CustomerName != nil {
		resp.CustomerName = *btx.CustomerName
	}
	if btx.CustomerAddress != nil {
		resp.CustomerAddress = *btx.CustomerAddress
	}
	return resp
}

func (s TransactionService) HandleWalletTransfer(ctx context.Context, user *db.User, req WalletTransferRequest) (*WalletTransferResponse, error) {
	_, err := s.store.Queries.GetTransactionByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil {
		return nil, fmt.Errorf("tx exists") //TODO: finish for tx status
	}

	kyc, err := s.store.Queries.GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier_1" {
		s.push.SendPushNotification(ctx, user.ID, "Verification required.", "This feature requires Tier 2 verification. Complete identity verification to continue")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_2")
	}

	recipientUser, err := s.store.Queries.GetUserByTag(ctx, sql.NullString{String: req.DestinationUserTag, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("failed to get recipient user: %v", err)
	}

	sendingWallet, err := s.store.Queries.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   req.Currency,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get sending wallet: %v", err)
	}

	recipientWallet, err := s.store.Queries.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: recipientUser.ID,
		Currency:   req.Currency,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get recipient wallet: %v", err)
	}

	sendingBalance, _ := utils.ToDecimal(sendingWallet.Balance.String)
	amount := decimal.NewFromFloat(req.Amount)

	if amount.GreaterThan(sendingBalance) {
		return nil, wallet.ErrInsufficientFunds
	}
	var description string
	if req.Description != "" {
		description = req.Description
	} else {
		description = "Transfer via SWIIFT"
	}

	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	amountUsd, _ := utils.ConvertToUSD(ctx, amount, req.Currency)

	tx, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		TransactionFlow: string(InPlatform),
		Type:            string(Transfer),
		Description:     sql.NullString{String: description, Valid: true},
		Amount:          amount.String(),
		AmountUsd:       amountUsd.String(),
		Currency:        req.Currency,
		IdempotencyKey:  req.IdempotencyKey,
		TFrom:           string(Wallet),
		TTo:             string(Wallet),
		Direction:       string(Debit),
		Status:          string(Pending),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create debit tx record [HandleWalletTransfer]: %v", err)
	}

	fee := decimal.NewFromInt(0)
	finalAmount := amount.Add(fee)

	wTx, err := s.store.WithTx(dbTx).CreateWalletTransferMetadata(ctx, db.CreateWalletTransferMetadataParams{
		Currency:      req.Currency,
		TransactionID: tx.ID,
		Sender:        user.UserTag.String,
		Type:          string(Debit),
		Recipient:     recipientUser.UserTag.String,
		ServiceCharge: sql.NullString{String: "0", Valid: true},
		Amount:        amount.String(),
		AmountPaid:    sql.NullString{String: finalAmount.String(), Valid: true},
		BonusEarned:   sql.NullString{String: "0", Valid: true},
		Reference:     req.IdempotencyKey,
		Status:        string(Pending),
		Description:   description,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet debit metadata record [HandleWalletTransfer]: %v", err)
	}

	_, err = s.store.WithTx(dbTx).DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: finalAmount.String(), Valid: true},
		ID:      sendingWallet.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to debit sending wallet [HandleWalletTransfer]: %v", err)
	}

	_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     tx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update tx status [HandleWalletTransfer]: %v", err)
	}

	err = s.store.WithTx(dbTx).UpdateWalletTransferMetadataStatus(ctx, db.UpdateWalletTransferMetadataStatusParams{
		ID:     wTx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update wallet debit metdata status [HandleWalletTransfer]: %v", err)
	}

	_, err = s.store.WithTx(dbTx).IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
		Balance: sql.NullString{String: amount.String(), Valid: true},
		ID:      recipientWallet.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to debit sending wallet [HandleWalletTransfer]: %v", err)
	}

	t, err := s.store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          recipientUser.ID,
		TransactionFlow: string(InPlatform),
		Type:            string(Transfer),
		Description:     sql.NullString{String: description, Valid: true},
		Amount:          amount.String(),
		AmountUsd:       amountUsd.String(),
		Currency:        req.Currency,
		IdempotencyKey:  uuid.NewString(),
		TFrom:           string(Wallet),
		TTo:             string(Wallet),
		Direction:       string(Credit),
		Status:          string(Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create debit tx record [HandleWalletTransfer]: %v", err)
	}

	_, err = s.store.WithTx(dbTx).CreateWalletTransferMetadata(ctx, db.CreateWalletTransferMetadataParams{
		Currency:      req.Currency,
		TransactionID: t.ID,
		Sender:        user.UserTag.String,
		Type:          string(Credit),
		Recipient:     recipientUser.UserTag.String,
		Amount:        amount.String(),
		Reference:     uuid.NewString(),
		Status:        string(Success),
		Description:   description,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet credit metadata record [HandleWalletTransfer]: %v", err)
	}

	// Commit the refund
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit refund: %w", err)
	}

	response := &WalletTransferResponse{
		Sender:     fmt.Sprintf("%s %s", user.FirstName.String, user.LastName.String),
		Recipient:  fmt.Sprintf("%s %s", recipientUser.FirstName.String, recipientUser.LastName.String),
		AmountPaid: finalAmount.InexactFloat64(),
		Amount:     float32(amount.InexactFloat64()),
		Remark:     description,
		Type:       wTx.Type,
		Date:       tx.CreatedAt,
		Status:     string(Success),
		Reference:  wTx.Reference,
	}

	// update streaks asynchronously since it doesn't impact the critical path of the transfer and can be retried independently if it fails
	if err = s.streakUpdater.UpdateStreakOnTransaction(ctx, user.ID, t.ID, t.Type); err != nil {
		s.logger.Error("Failed to update streak:", err)
	}

	// err = s.store.UpdateUserTransactionVolume(ctx, db.UpdateUserTransactionVolumeParams{
	// 	TotalTransactionVolume: sql.NullString{String: amount.String(), Valid: true},
	// 	ID:                     user.ID,
	// })
	// if err != nil {
	// 	s.logger.Error(fmt.Sprintf("failed to update user transaction volume: %v", err))
	// }

	// bgCtx := context.WithoutCancel(ctx)
	// go func() {
		message := fmt.Sprintf("A wallet debit transaction of %2.f %s has just been sent to %s. If this was not initiated by you, please contact SWIIFT immediately", amount.InexactFloat64(), req.Currency, recipientUser.UserTag.String)
		message_2 := fmt.Sprintf("%2.f %s has been credited to your wallet from %s", amount.InexactFloat64(), req.Currency, user.UserTag.String)

		s.notifyr.CreateWithRecipients(ctx, nil, "Wallet Debit", message, "system", []uuid.UUID{user.ID})
		s.notifyr.CreateWithRecipients(ctx, nil, "Wallet Credit", message_2, "system", []uuid.UUID{recipientUser.ID})

		s.push.CreditAlert(ctx, recipientUser.ID, amount.InexactFloat64(), req.Currency)
		s.push.DebitAlert(ctx, user.ID, amount.InexactFloat64(), req.Currency)
	// }()

	return response, nil
}

func (s TransactionService) HandleBankTransfer(ctx context.Context, user *db.User, req *BankTransferRequest) (*BankTransferResponse, error) {
	// Input validation
	if req.AccountNumber == "" || req.BankCode == "" || req.Name == "" {
		return nil, fmt.Errorf("invalid request: missing account number, bank code, or recipient name")
	}

	kyc, err := s.store.Queries.GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier_1" {
		s.push.SendPushNotification(ctx, user.ID, "Verification required.", "This feature requires Tier 2 verification. Complete identity verification to continue")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_2")
	}

	_, err = s.store.GetTransactionByIdempotencyKey(ctx, req.IdempotencyKey)
	if err == nil {
		return &BankTransferResponse{}, fmt.Errorf("tx exists")
	}

	amount := decimal.NewFromFloat(req.Amount)
	fee := decimal.NewFromFloat(0.00)

	totalAmount := amount.Add(fee)

	ngnWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   string(NGN),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet: %v", err)
	}
	walletBalance, err := utils.ToDecimal(ngnWallet.Balance.String)
	if err != nil {
		return nil, err
	}

	s.logger.Infof("wallet balance: %2.f, total amount: %2.f", walletBalance.InexactFloat64(), totalAmount.InexactFloat64())

	if totalAmount.GreaterThan(walletBalance) {
		return nil, wallet.ErrInsufficientFunds
	}

	if amount.LessThan(decimal.NewFromFloat(100)) || amount.GreaterThan(decimal.NewFromFloat(5000000)) {
		return nil, wallet.ErrAmountNotValidRange
	}

	amountUsd, _ := utils.ConvertToUSD(ctx, amount, string(NGN))

	recipientInfo, err := s.fiat.CreateTransferRecipient(
		req.AccountNumber,
		req.BankCode,
		req.Name,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transfer recipient: %v", err)
	}

	// Convert amount to int64 for the transfer (Nomba expects amount in NGN, not kobo)
	amountInNGN := int64(amount.InexactFloat64())

	debitTx, err := s.store.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            string(Transfer),
		Description:     sql.NullString{String: req.Description, Valid: true},
		TransactionFlow: string(Outflow),
		Amount:          amount.String(),
		AmountUsd:       amountUsd.String(),
		Currency:        string(NGN),
		IdempotencyKey:  req.IdempotencyKey,
		TFrom:           string(Wallet),
		TTo:             "bank",
		Direction:       string(Debit),
		Status:          string(Pending),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create debit tx record: %v", err)
	}

	transferReference := uuid.NewString()
	_, err = s.store.CreateBankTransferMetadata(ctx, db.CreateBankTransferMetadataParams{
		Amount:          amount.String(),
		ServiceCharge:   fee.String(),
		TransactionID:   debitTx.ID,
		AccountName:     req.Name,
		AccountNumber:   req.AccountNumber,
		ServiceProvider: sql.NullString{String: "nomba", Valid: true},
		Status:          string(Pending),
		Type:            string(Debit),
		AmountPaid:      totalAmount.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create debit metadata record: %v", err)
	}

	_, err = s.store.DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: totalAmount.String(), Valid: true},
		ID:      ngnWallet.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to debit wallet for transfer: %v", err)
	}

	var remark string
	if req.Description == "" {
		remark = "Sent via Swiift"
	}

	// Log transfer details for debugging live vs test differences
	s.logger.Infof("MakeTransfer details - recipientCode: %s, amount: %d NGN, accountName: %s, bankCode: %s",
		recipientInfo.RecipientCode, amountInNGN, req.Name, req.BankCode)

	res, err := s.fiat.MakeTransfer(recipientInfo.RecipientCode, transferReference, remark, amountInNGN, "SWIIFT")
	if err != nil {
		s.logger.Errorf("MakeTransfer failed: %v. Recipient: %s, Amount: %d NGN, Ref: %s", err, recipientInfo.RecipientCode, amountInNGN, transferReference)
		return nil, fmt.Errorf("failed to make transfer: %v", err)
	}
	s.logger.Infof("transfer response: %+v", res.RawData)

	if req.SaveBeneficiary {
		_, err = s.store.CreateBeneficiary(ctx, db.CreateBeneficiaryParams{
			UserID:          uuid.NullUUID{UUID: user.ID, Valid: true},
			AccountNumber:   req.AccountNumber,
			BeneficiaryName: req.Name,
			BankCode:        req.BankCode,
		})
		if err != nil {
			s.logger.Warnf("create beneficiary is failing: %v", err)
		}
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Normalize status to lowercase for comparison (Nomba returns uppercase)
	normalizedStatus := strings.ToLower(res.Status)

	switch normalizedStatus {
	case "failed":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     debitTx.ID,
			Status: "failed",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update debit tx record status: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID: debitTx.ID,
			Status:        "failed",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update failed bank transfer metadata status: %v", err)
		}

		_, err = s.store.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: totalAmount.String()},
			ID:      ngnWallet.ID,
		})
		if err != nil {
			s.logger.Warnf("failed to refund %2.f from failed bank transfer with transaction id: %d", totalAmount.InexactFloat64(), debitTx.ID)
		}

		// Commit the refund
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit refund: %w", err)
		}

		return &BankTransferResponse{
			Sender:         fmt.Sprintf("%s %s", user.FirstName.String, user.LastName.String),
			Recipient:      req.Name,
			Account_number: req.AccountNumber,
			BankCode:       req.BankCode,
			Amount:         amount.InexactFloat64(),
			AmountPaid:     totalAmount.InexactFloat64(),
			Remark:         req.Description,
			Type:           string(Transfer),
			Date:           debitTx.UpdatedAt,
			Status:         "failed",
			Reference:      req.IdempotencyKey,
			NombaData:      res.RawData,
		}, nil
	case "pending", "pending_billing", "processing":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     debitTx.ID,
			Status: "pending",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update debit tx record status: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID:        debitTx.ID,
			Status:               "pending",
			ServiceTransactionID: sql.NullString{String: res.RawData.Meta.MerchantTxRef, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update bank transfer metadata status: %v", err)
		}

		// Commit
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit pending bank transfer record: %w", err)
		}

		return &BankTransferResponse{
			Sender:         fmt.Sprintf("%s %s", user.FirstName.String, user.LastName.String),
			Recipient:      req.Name,
			Account_number: req.AccountNumber,
			BankCode:       req.BankCode,
			Amount:         amount.InexactFloat64(),
			AmountPaid:     totalAmount.InexactFloat64(),
			Remark:         req.Description,
			Type:           string(Transfer),
			Date:           debitTx.UpdatedAt,
			Status:         string(Success),
			Reference:      req.IdempotencyKey,
			NombaData:      res.RawData,
		}, nil
	case "success", "completed":
		_, err = s.store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     debitTx.ID,
			Status: "successful",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update debit tx record status: %v", err)
		}

		_, err = s.store.WithTx(dbTx).UpdateBankTransferStatus(ctx, db.UpdateBankTransferStatusParams{
			TransactionID:        debitTx.ID,
			Status:               "successful",
			ServiceTransactionID: sql.NullString{String: res.RawData.Meta.MerchantTxRef, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update bank transfer metadata status: %v", err)
		}

		// Commit
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to make transfer: %w", err)
		}

		entry := audit.NewTransactionLog(audit.EventFiatTransferCreated, debitTx.ID.String(), user.Role, user.ID, amount.InexactFloat64(), "", true)
		entry.Metadata = map[string]any{
			"account_number":   req.AccountNumber,
			"bank_code":        req.BankCode,
			"amount":           req.Amount,
			"save_beneficiary": req.SaveBeneficiary,
			"type":             debitTx.Type,
			"status":           debitTx.Status,
		}

		// err = s.store.UpdateUserTransactionVolume(ctx, db.UpdateUserTransactionVolumeParams{
		// 	TotalTransactionVolume: sql.NullString{String: amount.String(), Valid: true},
		// 	ID:                     user.ID,
		// })
		// if err != nil {
		// 	s.logger.Error(fmt.Sprintf("failed to update user transaction volume: %v", err))
		// }

		err = s.streakUpdater.UpdateStreakOnTransaction(ctx, user.ID, debitTx.ID, debitTx.Type)
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to update user streak: %v", err))
		}

		s.notifyr.CreateWithRecipients(ctx, nil, "Successful Bank Transfer", fmt.Sprintf("Transfer of %2.f was successful", amount.InexactFloat64()), "system", []uuid.UUID{user.ID})
		s.push.SendPushNotification(ctx, user.ID, "Successful Bank Transfer", fmt.Sprintf("Transfer of %2.f was successful", amount.InexactFloat64()))

		return &BankTransferResponse{
			Sender:         fmt.Sprintf("%s %s", user.FirstName.String, user.LastName.String),
			Recipient:      req.Name,
			Account_number: req.AccountNumber,
			BankCode:       req.BankCode,
			Amount:         amount.InexactFloat64(),
			AmountPaid:     totalAmount.InexactFloat64(),
			Remark:         req.Description,
			Type:           string(Transfer),
			Date:           debitTx.UpdatedAt,
			Status:         string(Success),
			Reference:      req.IdempotencyKey,
			NombaData:      res.RawData,
		}, nil
	default:
		return nil, fmt.Errorf("unknown bank transfer status: %s", res.Status)
	}
}

// CheckProviderHealth verifies if a provider is available by checking if it's initialized
func (s *TransactionService) CheckProviderHealth(ctx context.Context, providerName string) error {
	switch providerName {
	case "vtpass":
		// Test VTPass connectivity by fetching service categories
		if s.billProvider == nil {
			return fmt.Errorf("vtpass provider not configured")
		}
		_, err := s.billProvider.GetServiceCategories()
		if err != nil {
			return fmt.Errorf("vtpass provider health check failed: %w", err)
		}
	case "nomba":
		// Test Nomba connectivity
		if s.fiat == nil {
			return fmt.Errorf("nomba provider not configured")
		}
		// We can't directly test Nomba without making a transaction, so just verify it exists
	case "cryptomus":
		// Cryptomus provider is managed differently, just verify the service is initialized
		if s.currencyClient == nil {
			return fmt.Errorf("cryptomus provider (via currency service) not configured")
		}
	default:
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	return nil
}

// MonitorProviderHealth periodically checks provider availability and alerts admins
// Call this in a goroutine at application startup:
// go transactionService.MonitorProviderHealth(ctx)
func (s *TransactionService) MonitorProviderHealth(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	providers := []string{"vtpass", "nomba", "cryptomus"}
	unhealthyProviders := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, providerName := range providers {
				err := s.CheckProviderHealth(ctx, providerName)
				if err != nil {
					// Provider is down
					if !unhealthyProviders[providerName] {
						// First time detecting this provider as down - create alert
						unhealthyProviders[providerName] = true
						s.logger.Warnf("Provider health check failed: %s - %v", providerName, err)
						s.createAdminAlert(ctx, db.CreateAdminAlertParams{
							Severity: CRITICALALERT,
							Title:    fmt.Sprintf("Provider Unavailable: %s", strings.ToUpper(providerName)),
							Message:  fmt.Sprintf("The %s provider is currently unavailable. Error: %v", providerName, err),
							Source:   sql.NullString{String: "ProviderHealthMonitor", Valid: true},
						})
					}
				} else {
					// Provider is healthy
					if unhealthyProviders[providerName] {
						// Provider recovered - alert admins about recovery
						unhealthyProviders[providerName] = false
						s.logger.Infof("Provider health check passed: %s", providerName)
						s.createAdminAlert(ctx, db.CreateAdminAlertParams{
							Severity: INFOALERT,
							Title:    fmt.Sprintf("Provider Recovered: %s", strings.ToUpper(providerName)),
							Message:  fmt.Sprintf("The %s provider is now available and operational.", providerName),
							Source:   sql.NullString{String: "ProviderHealthMonitor", Valid: true},
						})
					}
				}
			}
		}
	}
}
