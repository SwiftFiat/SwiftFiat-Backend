package virtualcard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bridgecards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/streaks"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/subscriptions"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

// Service handles all virtual card business logic.
type Service struct {
	store           *db.Store
	bridgeCard      *bridgecards.BridgeCardProvider
	walletService   *wallet.WalletService
	logger          *logging.Logger
	streak          *streaks.StreakScheduler
	notifySvc       *service.Notification
	email           *service.Plunk
	pushSvc         *service.PushNotificationService
	subscriptionSvc *subscriptions.Service
	config          *utils.Config
}

func NewService(
	store *db.Store,
	logger *logging.Logger,
	bridgeCard *bridgecards.BridgeCardProvider,
	walletService *wallet.WalletService,
	streak *streaks.StreakScheduler,
	notifySvc *service.Notification,
	email *service.Plunk,
	pushSvc *service.PushNotificationService,
	subscriptionSvc *subscriptions.Service,
	config *utils.Config,
) *Service {
	return &Service{
		store:           store,
		logger:          logger,
		bridgeCard:      bridgeCard,
		walletService:   walletService,
		streak:          streak,
		notifySvc:       notifySvc,
		email:           email,
		pushSvc:         pushSvc,
		subscriptionSvc: subscriptionSvc,
		config:          config,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// decryptKycField decrypts a KYC string field, falling back to the raw value
// for legacy unencrypted rows.
func (s *Service) decryptKycField(value string) string {
	if value == "" {
		return ""
	}
	decrypted := utils.Decrypt(value, s.config.SigningKey)
	if decrypted == "" {
		return value
	}
	return decrypted
}

// notifyCard fires both in-app and push notifications for a card event.
// Always non-blocking; errors are logged and swallowed.
func (s *Service) notifyCard(userID int64, title, message string) {
	go func() {
		bgCtx := context.Background()
		if s.notifySvc != nil {
			if _, err := s.notifySvc.CreateWithRecipients(bgCtx, nil, title, message, "system", []int64{userID}); err != nil {
				s.logger.Errorf("notifyCard in-app error (user=%d title=%s): %v", userID, title, err)
			}
		}
		if s.pushSvc != nil {
			if err := s.pushSvc.SendPushNotification(bgCtx, userID, title, message); err != nil {
				s.logger.Errorf("notifyCard push error (user=%d title=%s): %v", userID, title, err)
			}
		}
	}()
}

// billingPeriod returns [start, end] for the current calendar month.
func billingPeriod(t time.Time) (time.Time, time.Time) {
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start, end
}

// ── Cardholder registration ───────────────────────────────────────────────────

// CreateCardHolder registers a user as a BridgeCard cardholder.
// Requires KYC tier_3. Persists cardholder ID + verification status atomically.
func (s *Service) CreateCardHolder(ctx context.Context, userID int32, phone string) (*bridgecards.CreateCardHolderResponse, error) {
	kyc, err := s.store.Queries.GetKYCByUserID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}
	if kyc.Tier != "tier_3" {
		s.notifyCard(int64(userID), "Verification required",
			"This feature requires Tier 3 verification. Complete identity verification to continue.")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_3")
	}

	user, err := s.store.GetUserByID(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	fullName := s.decryptKycField(kyc.FullName.String)
	bvn := s.decryptKycField(kyc.Bvn.String)
	houseNumber := s.decryptKycField(kyc.HouseNumber.String)
	streetName := s.decryptKycField(kyc.StreetName.String)
	city := s.decryptKycField(kyc.City.String)
	state := s.decryptKycField(kyc.State.String)
	postalCode := s.decryptKycField(kyc.PostalCode.String)
	selfieImage := s.decryptKycField(kyc.SelfieUrl.String)

	firstName, lastName := utils.SplitName(fullName)
	params := &bridgecards.CreateCardHolderRequest{
		FirstName: firstName, LastName: lastName,
		Email: user.Email, Phone: phone,
		Address: bridgecards.Address{
			Address: fmt.Sprintf("%s %s", houseNumber, streetName),
			City:    city, PostalCode: postalCode, State: state,
			Country: "Nigeria", HouseNumber: houseNumber,
		},
		Identity: bridgecards.Identity{
			IDType: "NIGERIAN_BVN_VERIFICATION", SelfieImage: selfieImage, BVN: bvn,
		},
	}

	response, err := s.bridgeCard.CreateCardHolder(ctx, params)
	if err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("failed to create cardholder: %s", response.Message)
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	if err := qtx.SetBridgeCardCardholderID(ctx, db.SetBridgeCardCardholderIDParams{
		BridgecardCardholderID: sql.NullString{String: response.Data.CardHolderID, Valid: true},
		UpdatedAt:              time.Now(), ID: int64(userID),
	}); err != nil {
		return nil, fmt.Errorf("persist cardholder ID: %w", err)
	}
	if err := qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
		ID:                           int64(userID),
		BridgecardVerificationStatus: sql.NullString{String: "verified", Valid: true},
		UpdatedAt:                    time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("update verification status: %w", err)
	}
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	s.notifyCard(int64(userID), "Identity verified",
		"Your identity has been verified. You can now create a virtual card.")
	return response, nil
}

// ── Card creation ─────────────────────────────────────────────────────────────

// CreateCard creates a virtual card, deducts fees + funding from the USD wallet,
// records all ledger entries, and commits atomically.
func (s *Service) CreateCard(ctx context.Context, params *bridgecards.CreateCardRequest) (*bridgecards.CreateCardResponse, error) {
	// 1. KYC gate
	kyc, err := s.store.Queries.GetKYCByUserID(ctx, int32(params.UserID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("fetch KYC: %w", err)
	}
	if kyc.Tier != "tier_3" {
		s.notifyCard(params.UserID, "Verification required",
			"This feature requires Tier 3 verification. Complete identity verification to continue.")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_3")
	}

	// 2. Open DB transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	// 3. Validate plan
	plan, err := qtx.GetCardPlan(ctx, params.CardPlanID)
	if err != nil {
		return nil, fmt.Errorf("get card plan: %w", err)
	}
	if !plan.IsActive || plan.DeletedAt.Valid {
		return nil, ErrInvalidCardPlan
	}
	if !plan.CardLimit.Valid || plan.CardLimit.String == "" {
		return nil, fmt.Errorf("card plan limit is not configured")
	}

	// 4. Enforce card count limits
	const GlobalMaxCards = 2
	activeCount, err := qtx.GetUserActiveCardsCount(ctx, params.UserID)
	if err != nil {
		return nil, fmt.Errorf("count active cards: %w", err)
	}
	if activeCount >= GlobalMaxCards {
		return nil, fmt.Errorf("maximum of %d active cards allowed per user", GlobalMaxCards)
	}
	planCount, err := qtx.GetUserCardsCountByPlan(ctx, db.GetUserCardsCountByPlanParams{
		UserID: params.UserID, CardPlanID: params.CardPlanID,
	})
	if err != nil {
		return nil, fmt.Errorf("count cards in plan: %w", err)
	}
	if planCount >= int64(plan.MaxCardsPerUser) {
		return nil, fmt.Errorf("maximum cards for the %s plan reached", plan.Name)
	}

	// 5. Parse amounts
	creationFee, err := utils.ToDecimal(plan.CreationFee)
	if err != nil {
		return nil, fmt.Errorf("parse creation fee: %w", err)
	}
	fundingAmount, err := utils.ToDecimal(params.FundingAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid funding amount: %w", err)
	}
	fundingCentsStr, err := utils.DollarStringToCentsString(params.FundingAmount)
	if err != nil {
		return nil, fmt.Errorf("dollar to cents: %w", err)
	}
	cardLimitCents, err := utils.DollarStringToCentsString(plan.CardLimit.String)
	if err != nil {
		return nil, fmt.Errorf("card limit to cents: %w", err)
	}

	// 6. Enforce minimum funding per card limit tier
	var minFundingCents decimal.Decimal
	switch cardLimitCents {
	case "500000":
		minFundingCents = decimal.NewFromInt(300) // $3.00
	case "1000000":
		minFundingCents = decimal.NewFromInt(400) // $4.00
	default:
		return nil, fmt.Errorf("unsupported card limit %s (must be $5,000 or $10,000)", plan.CardLimit.String)
	}
	if decimal.RequireFromString(fundingCentsStr).LessThan(minFundingCents) {
		return nil, fmt.Errorf("minimum funding is %s cents for the selected card limit", minFundingCents.String())
	}
	totalCost := creationFee.Add(fundingAmount)

	// 7. Lock USD wallet and check balance
	usdWallet, err := s.store.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
		CustomerID: params.UserID, Currency: "USD",
	})
	if err != nil {
		return nil, fmt.Errorf("get USD wallet: %w", err)
	}
	sourceWallet, err := s.walletService.GetWalletForUpdate(ctx, dbTx, usdWallet.ID)
	if err != nil {
		return nil, fmt.Errorf("lock wallet: %w", err)
	}
	if sourceWallet.Balance.LessThan(totalCost) {
		return nil, ErrInsufficientFunds
	}

	// 8. Create pending ledger entries
	now := time.Now()
	billingStart, billingEnd := billingPeriod(now)

	cardCreationTx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: params.UserID, Type: string(transaction.Card),
		Description: sql.NullString{String: "card-creation-fee", Valid: true},
		Amount:      creationFee.String(), Currency: "USD", AmountUsd: creationFee.String(),
		Status: string(transaction.Pending), TransactionFlow: string(transaction.Outflow),
		IdempotencyKey: params.IdempotencyKey, TFrom: string(transaction.Wallet),
		TTo: "card_provider", Direction: string(transaction.Debit),
	})
	if err != nil {
		return nil, fmt.Errorf("create creation-fee tx: %w", err)
	}

	fundcardTx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: params.UserID, Type: string(transaction.Card),
		Description: sql.NullString{String: "card-funding", Valid: true},
		Amount:      fundingAmount.String(), Currency: "USD", AmountUsd: fundingAmount.String(),
		Status: string(transaction.Pending), TransactionFlow: string(transaction.Outflow),
		IdempotencyKey: params.IdempotencyKey2, TFrom: string(transaction.Wallet),
		TTo: "card_provider", Direction: string(transaction.Debit),
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") && strings.Contains(err.Error(), "idempotency_key") {
			return nil, fmt.Errorf("duplicate card funding request — already in progress")
		}
		return nil, fmt.Errorf("create funding tx: %w", err)
	}

	// 9. Call BridgeCard
	bridgeCardReq := &bridgecards.CreateCardRequest{
		CardHolderID: params.CardHolderID, CardType: "virtual",
		Brand: "Mastercard", Currency: "USD",
		CardLimit: cardLimitCents, FundingAmount: fundingCentsStr,
		TransactionReference: utils.NewTxRef("c_"), SourceWalletID: usdWallet.ID,
	}
	bridgeCardDetails, err := s.bridgeCard.CreateCard(ctx, bridgeCardReq)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard create card: %w", err)
	}
	if bridgeCardDetails.Status != "success" {
		raw, _ := json.Marshal(bridgeCardDetails)
		return nil, fmt.Errorf("BridgeCard error: %s", string(raw))
	}

	// 10. Persist card record
	dbCard, err := qtx.CreateVirtualCard(ctx, db.CreateVirtualCardParams{
		UserID: params.UserID, CardPlanID: params.CardPlanID,
		BridgecardCardID: bridgeCardDetails.Data.CardID,
		CardName:         plan.Name,
		CardColor:        sql.NullString{String: params.CardColor, Valid: params.CardColor != ""},
		Currency:         "USD", Status: string(VirtualCardStatusActive),
		NextBillingDate: sql.NullTime{Time: now.AddDate(0, 1, 0), Valid: true},
		SpendingMonth:   sql.NullString{String: now.Format("2006-01"), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("persist virtual card: %w", err)
	}

	// 11. Card-level transaction records
	creationCardTxMeta, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID: dbCard.ID, UserID: params.UserID,
		Amount: creationFee.IntPart(), Currency: "USD",
		Status: string(transaction.Pending), TransactionType: string(transaction.Credit),
		TransactionDate: now, WebhookReceivedAt: sql.NullTime{Time: now, Valid: true},
		BalanceAfter: sql.NullString{String: fundingAmount.String(), Valid: true},
		Mode:         true, TransactionTimestamp: now,
		BridgecardTransactionID: "cc_" + uuid.New().String(), TransactionID: cardCreationTx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create creation card-tx: %w", err)
	}

	fundingCardTxMeta, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID: dbCard.ID, UserID: params.UserID,
		Amount: fundingAmount.IntPart(), Currency: "USD",
		Status: string(transaction.Pending), TransactionType: string(transaction.Credit),
		TransactionDate: now, WebhookReceivedAt: sql.NullTime{Time: now, Valid: true},
		BalanceAfter: sql.NullString{String: fundingAmount.String(), Valid: true},
		Mode:         true, TransactionTimestamp: now,
		BridgecardTransactionID: "cf_" + uuid.New().String(), TransactionID: fundcardTx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create funding card-tx: %w", err)
	}

	// 12. Billing record with correct calendar-month period
	billingTx, err := qtx.CreateCardBilling(ctx, db.CreateCardBillingParams{
		CardID: dbCard.ID, UserID: params.UserID, CardPlanID: params.CardPlanID,
		BillingType: "creation_fee", Amount: creationFee.String(), Currency: "USD",
		BillingPeriodStart: billingStart, BillingPeriodEnd: billingEnd,
		SourceWalletID: usdWallet.ID, Status: string(transaction.Pending),
		TransactionID: cardCreationTx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create billing record: %w", err)
	}

	// 13. Funding record
	fundingRecord, err := qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID: dbCard.ID, UserID: params.UserID, SourceWalletID: usdWallet.ID,
		Amount: fundingAmount.String(), Currency: "USD", SourceCurrency: usdWallet.Currency,
		FundingType: "initial", InitiatedBy: "user",
		Status: string(transaction.Pending), TransactionID: fundcardTx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create funding record: %w", err)
	}

	// 14. Deduct from wallet
	if _, err = qtx.DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: totalCost.String(), Valid: true}, ID: usdWallet.ID,
	}); err != nil {
		return nil, fmt.Errorf("deduct from wallet: %w", err)
	}

	// 15. Flip all pending → success in one pass
	type updFn struct {
		fn  func() error
		msg string
	}
	for _, u := range []updFn{
		{func() error {
			_, e := qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{ID: cardCreationTx.ID, Status: string(transaction.Success)})
			return e
		}, "creation tx"},
		{func() error {
			_, e := qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{ID: fundcardTx.ID, Status: string(transaction.Success)})
			return e
		}, "funding tx"},
		{func() error {
			_, e := qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{ID: creationCardTxMeta.ID, Status: string(transaction.Success)})
			return e
		}, "creation card-tx"},
		{func() error {
			_, e := qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{ID: fundingCardTxMeta.ID, Status: string(transaction.Success)})
			return e
		}, "funding card-tx"},
		{func() error {
			_, e := qtx.UpdateCardBillingStatus(ctx, db.UpdateCardBillingStatusParams{ID: billingTx.ID, Status: string(transaction.Success), FailureReason: sql.NullString{Valid: false}})
			return e
		}, "billing"},
		{func() error {
			_, e := qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{ID: fundingRecord.ID, Status: string(transaction.Success)})
			return e
		}, "funding record"},
	} {
		if err := u.fn(); err != nil {
			return nil, fmt.Errorf("update %s status: %w", u.msg, err)
		}
	}

	// 16. Commit
	if err := dbTx.Commit(); err != nil {
		s.logger.Errorf("CRITICAL: DB commit failed after BridgeCard card creation (card=%s): %v",
			bridgeCardDetails.Data.CardID, err)
		return nil, fmt.Errorf("commit: %w", err)
	}

	s.logger.Infof("Virtual card %s created for user %d (plan=%s, funding=$%s)",
		bridgeCardDetails.Data.CardID, params.UserID, plan.Name, params.FundingAmount)
	s.notifyCard(params.UserID, "Virtual card created",
		fmt.Sprintf("Your %s virtual card is ready and funded with $%s.", plan.Name, params.FundingAmount))

	return &bridgecards.CreateCardResponse{
		Status: bridgeCardDetails.Status, Message: bridgeCardDetails.Message, Data: bridgeCardDetails.Data,
	}, nil
}

// ── Balance / wallet ──────────────────────────────────────────────────────────

func (s *Service) GetCardBalance(ctx context.Context, cardID string) (*bridgecards.GetCardBalanceResponse, error) {
	return s.bridgeCard.GetCardBalance(ctx, cardID)
}

func (s *Service) FundIssuingWallet(ctx context.Context, req bridgecards.FundIssuingWalletRequest) string {
	msg, err := s.bridgeCard.FundIssuingWallet(ctx, req)
	if err != nil {
		return err.Error()
	}
	return *msg
}

func (s *Service) VerifyWebhookSignature(payload []byte, signature string) (bool, error) {
	return s.bridgeCard.VerifyWebhookSignature(payload, signature)
}

// ── Card funding ──────────────────────────────────────────────────────────────

func (s *Service) FundCard(ctx context.Context, req bridgecards.FundCardRequest, userID int64) (*bridgecards.FundCardResponse, error) {
	wallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: userID, Currency: "USD",
	})
	if err != nil {
		return nil, fmt.Errorf("get USD wallet: %w", err)
	}
	walletBalance, err := utils.ToDecimal(wallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("parse wallet balance: %w", err)
	}
	fundingAmount, err := utils.ToDecimal(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("parse funding amount: %w", err)
	}
	if walletBalance.LessThan(fundingAmount) {
		return nil, ErrInsufficientFunds
	}
	fundingCents, err := utils.DollarStringToCentsString(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("convert to cents: %w", err)
	}
	req.Amount = fundingCents

	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, req.CardID)
	if err != nil {
		return nil, ErrCardNotFound
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	tx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID, Type: string(transaction.Card),
		Description: sql.NullString{String: fmt.Sprintf("Fund card %s", req.CardID), Valid: true},
		Amount:      fundingAmount.String(), Currency: "USD", AmountUsd: fundingAmount.String(),
		Status: string(transaction.Pending), TransactionFlow: string(transaction.Outflow),
		IdempotencyKey: req.TransactionReference,
		TFrom:          string(transaction.Wallet), TTo: "card_provider", Direction: string(transaction.Debit),
	})
	if err != nil {
		return nil, fmt.Errorf("create funding tx: %w", err)
	}

	fundingRecord, err := qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID: card.ID, UserID: userID, SourceWalletID: wallet.ID,
		Amount: fundingAmount.String(), Currency: "USD", SourceCurrency: wallet.Currency,
		FundingType: "manual", InitiatedBy: "user",
		Status: string(CardFundingStatusPending), TransactionID: tx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create funding record: %w", err)
	}

	cardTxMeta, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID: card.ID, UserID: userID,
		Amount: fundingAmount.IntPart(), Currency: "USD",
		Status: string(transaction.Pending), TransactionType: string(transaction.Credit),
		TransactionDate: time.Now(), WebhookReceivedAt: sql.NullTime{Time: time.Now(), Valid: true},
		BalanceAfter: sql.NullString{String: fundingAmount.String(), Valid: true},
		Mode:         true, TransactionTimestamp: time.Now(),
		BridgecardTransactionID: "cf_" + uuid.New().String(), TransactionID: tx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create card tx meta: %w", err)
	}

	if _, err = qtx.DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		ID: wallet.ID, Balance: sql.NullString{String: fundingAmount.String(), Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("decrement wallet: %w", err)
	}

	bridgeResp, err := s.bridgeCard.FundCard(ctx, req)
	if err != nil {
		// Refund wallet on BridgeCard failure
		s.logger.Errorf("BridgeCard FundCard failed (card=%s): %v — refunding", req.CardID, err)
		_, _ = qtx.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			ID: wallet.ID, Balance: sql.NullString{String: fundingAmount.String(), Valid: true},
		})
		_, _ = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{ID: tx.ID, Status: string(transaction.Failed)})
		_, _ = qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
			ID: fundingRecord.ID, Status: string(CardFundingStatusFailed),
			FailureReason: sql.NullString{String: err.Error(), Valid: true},
		})
		dbTx.Commit()
		return nil, fmt.Errorf("BridgeCard fund card: %w", err)
	}

	_, _ = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{ID: tx.ID, Status: string(transaction.Success)})
	_, _ = qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
		ID: fundingRecord.ID, Status: string(transaction.Success), FailureReason: sql.NullString{Valid: false},
	})
	if _, err = qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{
		ID: cardTxMeta.ID, Status: string(transaction.Success),
	}); err != nil {
		return nil, fmt.Errorf("update card tx: %w", err)
	}

	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	s.notifyCard(userID, "Card funded",
		fmt.Sprintf("$%s has been added to your virtual card.", fundingAmount.String()))
	return bridgeResp, nil
}

// ── Freeze / Unfreeze ─────────────────────────────────────────────────────────

func (s *Service) FreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	if VirtualCardStatus(card.Status) == VirtualCardStatusTerminated {
		return nil, ErrCardAlreadyTerminated
	}
	resp, err := s.bridgeCard.FreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard freeze: %w", err)
	}
	if _, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID: card.ID, Status: string(VirtualCardStatusFrozen),
	}); err != nil {
		return nil, fmt.Errorf("update card status: %w", err)
	}
	s.notifyCard(userID, "Card frozen",
		"Your virtual card has been frozen. No transactions will be processed until you unfreeze it.")
	return resp, nil
}

func (s *Service) AdminFreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	resp, err := s.bridgeCard.FreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard freeze: %w", err)
	}
	if _, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID: card.ID, Status: string(VirtualCardStatusFrozen),
	}); err != nil {
		return nil, fmt.Errorf("update card status: %w", err)
	}
	go func() {
		bgCtx := context.Background()
		s.pushSvc.AdminFreezeCardNotification(bgCtx, card.UserID, card.CardName)
		s.notifySvc.CreateWithRecipients(bgCtx, nil, "Card frozen by admin",
			"An administrator has frozen your virtual card. Contact support if you believe this is an error.",
			"system", []int64{card.UserID})
	}()
	return resp, nil
}

func (s *Service) UnfreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	if VirtualCardStatus(card.Status) == VirtualCardStatusTerminated {
		return nil, ErrCardAlreadyTerminated
	}
	resp, err := s.bridgeCard.UnfreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard unfreeze: %w", err)
	}
	if _, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID: card.ID, Status: string(VirtualCardStatusActive),
	}); err != nil {
		return nil, fmt.Errorf("update card status: %w", err)
	}
	s.notifyCard(userID, "Card unfrozen", "Your virtual card is now active again.")
	return resp, nil
}

func (s *Service) AdminUnfreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	resp, err := s.bridgeCard.UnfreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard unfreeze: %w", err)
	}
	if _, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID: card.ID, Status: string(VirtualCardStatusActive),
	}); err != nil {
		return nil, fmt.Errorf("update card status: %w", err)
	}
	go func() {
		bgCtx := context.Background()
		s.pushSvc.AdminUnfreezeCardNotification(bgCtx, card.UserID, card.CardName)
		s.notifySvc.CreateWithRecipients(bgCtx, nil, "Card unfrozen by admin",
			"An administrator has unfrozen your virtual card. It is now active.",
			"system", []int64{card.UserID})
	}()
	return resp, nil
}

// ── PIN ───────────────────────────────────────────────────────────────────────

func (s *Service) UpdateCardPin(ctx context.Context, req bridgecards.UpdateCardPinRequest, userID int64) (*bridgecards.CardResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, req.CardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	resp, err := s.bridgeCard.UpdateCardPin(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard update PIN: %w", err)
	}
	s.notifyCard(userID, "Card PIN updated", "Your virtual card PIN has been updated successfully.")
	return resp, nil
}

// ── Termination ───────────────────────────────────────────────────────────────

func (s *Service) DeleteCard(ctx context.Context, cardID uuid.UUID, userID int64) (*bridgecards.CardResponse, error) {
	card, err := s.store.GetVirtualCard(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	if VirtualCardStatus(card.Status) == VirtualCardStatusTerminated {
		return nil, ErrCardAlreadyTerminated
	}
	resp, err := s.bridgeCard.DeleteCard(ctx, card.BridgecardCardID)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard delete: %w", err)
	}
	if _, err = s.store.TerminateCard(ctx, db.TerminateCardParams{ID: card.ID, UserID: userID}); err != nil {
		return nil, fmt.Errorf("terminate card in DB: %w", err)
	}
	s.notifyCard(userID, "Card terminated",
		"Your virtual card has been terminated and can no longer be used.")
	return resp, nil
}

func (s *Service) AdminDeleteCard(ctx context.Context, cardID uuid.UUID, userID int64) (*bridgecards.CardResponse, error) {
	card, err := s.store.GetVirtualCard(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	resp, err := s.bridgeCard.DeleteCard(ctx, card.BridgecardCardID)
	if err != nil {
		return nil, fmt.Errorf("BridgeCard delete: %w", err)
	}
	if _, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID: card.ID, Status: string(VirtualCardStatusTerminated),
	}); err != nil {
		return nil, fmt.Errorf("update card status: %w", err)
	}
	go func() {
		bgCtx := context.Background()
		s.pushSvc.AdminTerminateCardNotification(bgCtx, card.UserID, card.CardName)
		s.notifySvc.CreateWithRecipients(bgCtx, nil, "Virtual Card Terminated",
			"Your virtual card has been terminated by an administrator. Contact support for more information.",
			"system", []int64{card.UserID})
	}()
	return resp, nil
}

// ── Card info ─────────────────────────────────────────────────────────────────

func (s *Service) ListCardsFromProvider(ctx context.Context, cardholderID string, userID int64) (*bridgecards.ListCardsResponse, error) {
	return s.bridgeCard.ListCards(ctx, cardholderID)
}

func (s *Service) ListCardsFromDB(ctx context.Context, userID int64) ([]db.GetUserCardsRow, error) {
	return s.store.GetUserCards(ctx, userID)
}

// GetCardDetails returns card details from BridgeCard after validating ownership.
func (s *Service) GetCardDetails(ctx context.Context, cardID string, userID int64) (*bridgecards.GetCardDetailsResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	return s.bridgeCard.GetCardDetails(ctx, cardID)
}

// DebitCard validates ownership before calling BridgeCard.
func (s *Service) DebitCard(ctx context.Context, cardID string, userID int64) (*bridgecards.DebitCardResponse, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, ErrCardNotFound
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	return s.bridgeCard.DebitCard(ctx, bridgecards.DebitCardRequest{CardID: cardID})
}

func (s *Service) GetCardTransaction(ctx context.Context, cardID string, userID int64) (*bridgecards.GetCardTransactionResponse, error) {
	return s.bridgeCard.GetCardTransaction(ctx, cardID)
}

func (s *Service) ListCardTransactions(ctx context.Context, req bridgecards.ListCardTransactionsRequest) (*bridgecards.ListCardTransactionsResponse, error) {
	return s.bridgeCard.ListCardTransactions(ctx, req)
}

func (s *Service) GetCardTransactionStatus(ctx context.Context, cardID, clientRef string, userID int64) (*bridgecards.GetCardTransactionStatusResponse, error) {
	return s.bridgeCard.GetCardTransactionStatus(ctx, cardID, clientRef)
}

func (s *Service) WithdrawCard(ctx context.Context, req bridgecards.WithdrawCardRequest) (*bridgecards.WithdrawCardResponse, error) {
	return s.bridgeCard.WithdrawCard(ctx, req)
}

func (s *Service) GetIssuingWalletBalance(ctx context.Context) (*bridgecards.IssuingWalletBalanceResponse, error) {
	return s.bridgeCard.GetIssuingWalletBalance(ctx)
}

func (s *Service) GetAllIssuedCards(ctx context.Context) (*bridgecards.GetAllIssuedcardResponse, error) {
	return s.bridgeCard.GetAllIssuedCards(ctx)
}

// ── Webhook dispatcher ────────────────────────────────────────────────────────

// ProcessWebhook routes incoming BridgeCard webhook payloads to the correct handler.
func (s *Service) ProcessWebhook(ctx context.Context, payload []byte) (string, error) {
	var event struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", fmt.Errorf("parse webhook event: %w", err)
	}
	s.logger.Infof("webhook event: %s", event.Event)

	switch {
	case strings.HasPrefix(event.Event, "cardholder_verification."):
		return s.processCardholderVerification(ctx, payload)
	case strings.HasPrefix(event.Event, "card_credit."):
		return s.processCardCredit(ctx, payload)
	case strings.HasPrefix(event.Event, "card_unload"):
		return s.processCardUnloadEvent(ctx, payload)
	case strings.HasPrefix(event.Event, "card_creation"):
		return s.processCardCreationEvent(ctx, payload)
	case strings.HasPrefix(event.Event, "card_debit"):
		return s.processCardDebitEvent(ctx, payload)
	default:
		s.logger.Warnf("unhandled webhook event: %s", event.Event)
		return event.Event, nil
	}
}

func (s *Service) processCardDebitEvent(ctx context.Context, payload []byte) (string, error) {
	parsed, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card debit event: %w", err)
	}
	switch d := parsed.(type) {
	case *bridgecards.CardDebitEventSuccessful:
		return s.handleCardDebitEventSuccess(ctx, d)
	case *bridgecards.CardDebitEventDeclined:
		return s.handleCardDebitEventDeclined(ctx, d)
	default:
		return "", fmt.Errorf("unknown debit event type")
	}
}

func (s *Service) processCardCredit(ctx context.Context, payload []byte) (string, error) {
	parsed, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card credit: %w", err)
	}
	switch c := parsed.(type) {
	case *bridgecards.CardCreditSuccess:
		return s.handleCardCreditSuccess(ctx, c)
	case *bridgecards.CardCreditFailed:
		return s.handleCardCreditFailed(ctx, c)
	default:
		return "", fmt.Errorf("unknown credit type")
	}
}

func (s *Service) processCardUnloadEvent(ctx context.Context, payload []byte) (string, error) {
	parsed, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card unload event: %w", err)
	}
	switch u := parsed.(type) {
	case *bridgecards.CardWithDrawEventSuccessful:
		return s.handleCardUnloadEventSuccess(ctx, u)
	case *bridgecards.CardWithDrawEventFailed:
		return s.handleCardUnloadEventFailed(ctx, u)
	default:
		return "", fmt.Errorf("unknown unload event type")
	}
}

func (s *Service) processCardholderVerification(ctx context.Context, payload []byte) (string, error) {
	parsed, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse cardholder verification: %w", err)
	}
	switch v := parsed.(type) {
	case *bridgecards.CardholderVerificationSuccess:
		return s.handleCardholderVerificationSuccess(ctx, v)
	case *bridgecards.CardholderVerificationFailed:
		return s.handleCardholderVerificationFailed(ctx, v)
	default:
		return "", fmt.Errorf("unknown verification type")
	}
}

func (s *Service) processCardCreationEvent(ctx context.Context, payload []byte) (string, error) {
	parsed, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card creation event: %w", err)
	}
	switch c := parsed.(type) {
	case *bridgecards.CardCreationEventSuccessful:
		return s.handleCardCreationEventSuccess(ctx, c)
	case *bridgecards.CardCreationEventFailed:
		return s.handleCardCreationEventFailed(ctx, c)
	default:
		return "", fmt.Errorf("unknown creation event type")
	}
}

// ── Webhook event handlers ────────────────────────────────────────────────────

// handleCardCreationEventSuccess activates a card that was provisioned asynchronously.
func (s *Service) handleCardCreationEventSuccess(ctx context.Context, e *bridgecards.CardCreationEventSuccessful) (string, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, e.Data.CardID)
	if err != nil {
		s.logger.Warnf("card_creation.successful: card %s not in DB (async path)", e.Data.CardID)
		return "card_creation_event.successful", nil
	}
	if VirtualCardStatus(card.Status) == VirtualCardStatusInactive {
		if _, err := s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
			ID: card.ID, Status: string(VirtualCardStatusActive),
		}); err != nil {
			s.logger.Errorf("activate card %s from creation webhook: %v", e.Data.CardID, err)
		}
	}
	s.notifyCard(card.UserID, "Virtual card ready",
		"Your virtual card has been activated and is ready to use.")
	s.logger.Infof("card_creation.successful: card %s activated (user=%d)", e.Data.CardID, card.UserID)
	return "card_creation_event.successful", nil
}

// handleCardCreationEventFailed marks a card as terminated when provisioning fails.
func (s *Service) handleCardCreationEventFailed(ctx context.Context, e *bridgecards.CardCreationEventFailed) (string, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, e.Data.CardID)
	if err != nil {
		s.logger.Warnf("card_creation.failed: card %s not in DB, reason=%s", e.Data.CardID, e.Data.Reason)
		return "card_creation_event.failed", nil
	}
	if _, err := s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID: card.ID, Status: string(VirtualCardStatusTerminated),
	}); err != nil {
		s.logger.Errorf("terminate card %s after creation failure: %v", e.Data.CardID, err)
	}
	s.notifyCard(card.UserID, "Card creation failed",
		fmt.Sprintf("We could not create your virtual card: %s. Please try again or contact support.", e.Data.Reason))
	s.logger.Warnf("card_creation.failed: card %s (user=%d) reason=%s", e.Data.CardID, card.UserID, e.Data.Reason)
	return "card_creation_event.failed", nil
}

// handleCardDebitEventSuccess records a successful card spend.
func (s *Service) handleCardDebitEventSuccess(ctx context.Context, success *bridgecards.CardDebitEventSuccessful) (string, error) {
	now := time.Now()
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: success.Data.CardholderID, Valid: true})
	if err != nil {
		return "", fmt.Errorf("get user by cardholder_id: %w", err)
	}

	amountStr, err := utils.CentsStringToDollarString(success.Data.Amount)
	if err != nil {
		return "", fmt.Errorf("cents to dollars: %w", err)
	}
	amount, err := utils.ToDecimal(amountStr)
	if err != nil {
		return "", fmt.Errorf("parse amount: %w", err)
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	card, err := qtx.GetVirtualCardByBridgeCardID(ctx, success.Data.CardID)
	if err != nil {
		return "", fmt.Errorf("get card %s: %w", success.Data.CardID, err)
	}

	rawWebhook, _ := json.Marshal(success)
	amountUsd, err := utils.ConvertToUSD(ctx, amount, success.Data.Currency)
	if err != nil {
		return "", fmt.Errorf("convert to USD: %w", err)
	}

	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		Type:            string(transaction.Card),
		Description:     sql.NullString{String: "Card spend — " + success.Data.Description, Valid: true},
		TransactionFlow: string(transaction.Outflow),
		Status:          string(transaction.Success),
		Amount:          amount.String(), AmountUsd: amountUsd.String(),
		UserID: card.UserID,
	})
	if err != nil {
		return "", fmt.Errorf("create transaction: %w", err)
	}

	merchantName := success.Data.Description
	if merchantName == "" {
		merchantName = "Unknown Merchant"
	}

	txDate, _ := time.Parse("2006-01-02 15:04:05", success.Data.TransactionDate)
	if txDate.IsZero() {
		txDate = now
	}
	var txTimestamp time.Time
	if ts, err := time.Parse("2006-01-02 15:04:05", success.Data.TransactionTimestamp); err == nil {
		txTimestamp = ts
	} else if n, err := strconv.ParseInt(success.Data.TransactionTimestamp, 10, 64); err == nil {
		txTimestamp = time.Unix(n, 0)
	} else {
		txTimestamp = now
	}

	cardTx, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID: card.ID, UserID: user.ID,
		BridgecardTransactionID: success.Data.TransactionReference,
		TransactionType:         success.Data.CardTransactionType,
		MerchantName:            sql.NullString{String: merchantName, Valid: true},
		MerchantCategoryCode:    sql.NullString{String: success.Data.MerchantCategoryCode, Valid: success.Data.MerchantCategoryCode != ""},
		Amount:                  amount.IntPart(), Fee: 0, Currency: success.Data.Currency,
		Status:               string(transaction.Success),
		BalanceAfter:         sql.NullString{String: success.Data.SettledBookBalance, Valid: success.Data.SettledBookBalance != ""},
		TransactionDate:      txDate,
		WebhookReceivedAt:    sql.NullTime{Time: now, Valid: true},
		RawWebhookData:       pqtype.NullRawMessage{RawMessage: rawWebhook, Valid: true},
		TransactionID:        txx.ID,
		Mode:                 success.Data.Livemode,
		TransactionTimestamp: txTimestamp,
	})
	if err != nil {
		return "", fmt.Errorf("create card transaction: %w", err)
	}

	if _, err = qtx.UpdateCardSpending(ctx, db.UpdateCardSpendingParams{
		CurrentMonthSpend: sql.NullInt64{Int64: amount.IntPart(), Valid: true},
		ID:                card.ID,
		SpendingMonth:     sql.NullString{String: now.Format("2006-01"), Valid: true},
		SpendingDay:       sql.NullString{String: now.Format("2006-01-02"), Valid: true},
	}); err != nil {
		return "", fmt.Errorf("update card spending: %w", err)
	}

	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	// Post-commit async work
	if s.subscriptionSvc != nil {
		subTx := db.CardTransaction{
			ID: cardTx.ID, CardID: cardTx.CardID, UserID: cardTx.UserID,
			BridgecardTransactionID: cardTx.BridgecardTransactionID,
			Amount:                  cardTx.Amount, Currency: cardTx.Currency,
			Status: cardTx.Status, TransactionType: cardTx.TransactionType,
			TransactionDate: cardTx.TransactionDate, MerchantName: cardTx.MerchantName,
			MerchantCategoryCode: cardTx.MerchantCategoryCode,
			TransactionTimestamp: cardTx.TransactionTimestamp,
		}
		go func() {
			if err := s.subscriptionSvc.DetectAndLogSubscription(context.Background(), &subTx); err != nil {
				s.logger.Errorf("subscription detection failed for tx %s: %v", cardTx.BridgecardTransactionID, err)
			}
		}()
	}

	if err := s.streak.UpdateStreakOnTransaction(ctx, user.ID, txx.ID, "card"); err != nil {
		s.logger.Warnf("streak update failed for user %d: %v", user.ID, err)
	}

	s.notifyCard(user.ID, "Card transaction",
		fmt.Sprintf("$%s spent at %s.", amount.String(), merchantName))
	return "card_debit_event.successful", nil
}

// handleCardDebitEventDeclined notifies the user of a declined transaction.
func (s *Service) handleCardDebitEventDeclined(ctx context.Context, declined *bridgecards.CardDebitEventDeclined) (string, error) {
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, declined.Data.CardID)
	if err != nil {
		s.logger.Warnf("card_debit.declined: unknown card %s", declined.Data.CardID)
		return "card_debit_event.declined", nil
	}
	amountStr, _ := utils.CentsStringToDollarString(declined.Data.Amount)
	merchant := declined.Data.Description
	if merchant == "" {
		merchant = "Unknown Merchant"
	}
	s.notifyCard(card.UserID, "Card transaction declined",
		fmt.Sprintf("A $%s charge at %s was declined. Check your card balance or limits.", amountStr, merchant))
	s.logger.Warnf("card_debit.declined: card %s (user=%d) amount=%s merchant=%s",
		declined.Data.CardID, card.UserID, amountStr, merchant)
	return "card_debit_event.declined", nil
}

// handleCardCreditSuccess handles wallet→card credits via webhook.
func (s *Service) handleCardCreditSuccess(ctx context.Context, success *bridgecards.CardCreditSuccess) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: success.CardholderID, Valid: true})
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	usdWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "USD",
	})
	if err != nil {
		return "", fmt.Errorf("get USD wallet: %w", err)
	}

	walletBalance, _ := utils.ToDecimal(usdWallet.Balance.String)
	amount, err := utils.ToDecimal(success.Amount)
	if err != nil {
		return "", fmt.Errorf("parse amount: %w", err)
	}
	if walletBalance.LessThan(amount) {
		return "", fmt.Errorf("insufficient wallet balance for card credit")
	}
	newBalance := walletBalance.Sub(amount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	if _, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: usdWallet.ID, Amount: sql.NullString{String: newBalance.String(), Valid: true},
	}); err != nil {
		return "", fmt.Errorf("update wallet: %w", err)
	}

	card, err := qtx.GetVirtualCardByBridgeCardID(ctx, success.CardID)
	if err != nil {
		return "", fmt.Errorf("get card: %w", err)
	}

	if _, err = qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID: card.ID, Amount: success.Amount, Currency: success.Currency,
		SourceWalletID: usdWallet.ID, UserID: user.ID, SourceCurrency: usdWallet.Currency,
		FundingType: "webhook", InitiatedBy: "system",
		Status: string(CardFundingStatusSuccessful),
	}); err != nil {
		return "", fmt.Errorf("create funding record: %w", err)
	}

	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: user.ID, Type: string(transaction.Card),
		TransactionFlow: string(transaction.Inflow),
		Description:     sql.NullString{String: "Card credit via webhook", Valid: true},
		Amount:          amount.String(), Currency: "USD", AmountUsd: amount.String(),
		Status: string(transaction.Success),
	})
	if err != nil {
		return "", fmt.Errorf("create transaction: %w", err)
	}

	if _, err = qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID: card.ID, UserID: user.ID,
		BridgecardTransactionID: success.TransactionReference,
		Amount:                  amount.IntPart(), Currency: success.Currency,
		TransactionType: success.CardTransactionType,
		Fee:             0, Status: string(transaction.Success), Mode: success.Livemode,
		TransactionDate:      success.TransactionDate,
		TransactionTimestamp: success.TransactionTimestamp,
		BalanceAfter:         sql.NullString{String: success.SettledBookBalance, Valid: true},
		TransactionID:        txx.ID,
	}); err != nil {
		return "", fmt.Errorf("create card transaction: %w", err)
	}

	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	if err := s.streak.UpdateStreakOnTransaction(ctx, user.ID, txx.ID, "card"); err != nil {
		s.logger.Warnf("streak update failed for user %d: %v", user.ID, err)
	}

	s.notifyCard(user.ID, "Card credited",
		fmt.Sprintf("$%s has been added to your virtual card.", amount.String()))
	return "card_credit.success", nil
}

// handleCardCreditFailed refunds the wallet when a card credit event fails.
func (s *Service) handleCardCreditFailed(ctx context.Context, failed *bridgecards.CardCreditFailed) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: failed.CardholderID, Valid: true})
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	usdWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "USD",
	})
	if err != nil {
		return "", fmt.Errorf("get USD wallet: %w", err)
	}

	failedAmountStr, err := utils.CentsStringToDollarString(failed.Amount)
	if err != nil {
		return "", fmt.Errorf("cents to dollars: %w", err)
	}
	failedAmount, err := utils.ToDecimal(failedAmountStr)
	if err != nil {
		return "", fmt.Errorf("parse amount: %w", err)
	}

	walletBalance, _ := utils.ToDecimal(usdWallet.Balance.String)
	newBalance := walletBalance.Add(failedAmount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	if _, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: usdWallet.ID, Amount: sql.NullString{String: newBalance.String(), Valid: true},
	}); err != nil {
		return "", fmt.Errorf("refund wallet: %w", err)
	}

	card, _ := s.store.GetVirtualCardByBridgeCardID(ctx, failed.CardID)
	if card.ID != uuid.Nil {
		_, _ = qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
			ID: card.ID, Status: string(CardFundingStatusFailed),
			FailureReason: sql.NullString{String: "card_credit.failed webhook", Valid: true},
		})
	}

	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	s.notifyCard(user.ID, "Card credit failed",
		fmt.Sprintf("A $%s credit to your virtual card failed. The amount has been returned to your wallet.", failedAmount.String()))
	s.logger.Warnf("card_credit.failed: card %s (user=%d) refunded $%s", failed.CardID, user.ID, failedAmount.String())
	return "card_credit.failed", nil
}

// handleCardUnloadEventSuccess credits the wallet when funds are withdrawn from a card.
func (s *Service) handleCardUnloadEventSuccess(ctx context.Context, success *bridgecards.CardWithDrawEventSuccessful) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: success.Data.CardholderID, Valid: true})
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	usdWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID, Currency: "USD",
	})
	if err != nil {
		return "", fmt.Errorf("get USD wallet: %w", err)
	}

	amountStr, err := utils.CentsStringToDollarString(success.Data.Amount)
	if err != nil {
		return "", fmt.Errorf("cents to dollars: %w", err)
	}
	amount, err := utils.ToDecimal(amountStr)
	if err != nil {
		return "", fmt.Errorf("parse amount: %w", err)
	}

	walletBalance, _ := utils.ToDecimal(usdWallet.Balance.String)
	newBalance := walletBalance.Add(amount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	if _, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: usdWallet.ID, Amount: sql.NullString{String: newBalance.String(), Valid: true},
	}); err != nil {
		return "", fmt.Errorf("credit wallet: %w", err)
	}

	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: user.ID, Type: "card",
		TransactionFlow: string(transaction.InPlatform),
		Description:     sql.NullString{String: "Card withdrawal to wallet", Valid: true},
		Amount:          amount.String(), Currency: "USD", AmountUsd: amount.String(),
		Status: string(transaction.Success),
	})
	if err != nil {
		return "", fmt.Errorf("create transaction: %w", err)
	}

	card, err := qtx.GetVirtualCardByBridgeCardID(ctx, success.Data.CardID)
	if err != nil {
		return "", fmt.Errorf("get card: %w", err)
	}

	if _, err = qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID: card.ID, UserID: user.ID,
		BridgecardTransactionID: success.Data.TransactionReference,
		Amount:                  amount.IntPart(), Currency: success.Data.Currency,
		TransactionType: success.Data.CardTransactionType,
		Fee:             0, Status: string(transaction.Success), Mode: success.Data.Livemode,
		TransactionTimestamp: success.Data.TransactionTimestamp,
		TransactionDate:      time.Now(), TransactionID: txx.ID,
	}); err != nil {
		return "", fmt.Errorf("create card transaction: %w", err)
	}

	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	s.notifyCard(user.ID, "Card withdrawal successful",
		fmt.Sprintf("$%s has been transferred from your virtual card to your USD wallet.", amount.String()))
	return "card_withdraw.success", nil
}

// handleCardUnloadEventFailed notifies the user when a card withdrawal fails.
func (s *Service) handleCardUnloadEventFailed(ctx context.Context, failed *bridgecards.CardWithDrawEventFailed) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: failed.Data.CardholderID, Valid: true})
	if err != nil {
		s.logger.Warnf("card_unload.failed: unknown cardholder %s", failed.Data.CardholderID)
		return "card_withdraw.failed", nil
	}
	amountStr, _ := utils.CentsStringToDollarString(failed.Data.Amount)
	s.notifyCard(user.ID, "Card withdrawal failed",
		fmt.Sprintf("A $%s withdrawal from your virtual card failed. Please try again or contact support.", amountStr))
	s.logger.Warnf("card_unload.failed: card %s (user=%d) amount=%s reason=%s",
		failed.Data.CardID, user.ID, amountStr, failed.Data.Description)
	return "card_withdraw.failed", nil
}

// handleCardholderVerificationSuccess persists a successful BVN verification.
func (s *Service) handleCardholderVerificationSuccess(ctx context.Context, success *bridgecards.CardholderVerificationSuccess) (string, error) {
	s.logger.Infof("cardholder_verification.successful: %s", success.CardholderID)

	ch, err := s.bridgeCard.GetCardHolder(ctx, success.CardholderID)
	if err != nil {
		return "", fmt.Errorf("fetch cardholder %s: %w", success.CardholderID, err)
	}

	var userID int64
	if ch != nil && ch.Metadata != nil {
		userID = extractInt64Metadata(ch.Metadata, "user_id")
	}
	if userID == 0 {
		user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: success.CardholderID, Valid: true})
		if err != nil {
			return "", fmt.Errorf("resolve user for cardholder %s: %w", success.CardholderID, err)
		}
		userID = user.ID
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	_ = s.store.SetBridgeCardCardholderID(ctx, db.SetBridgeCardCardholderIDParams{
		BridgecardCardholderID: sql.NullString{String: success.CardholderID, Valid: true},
		UpdatedAt:              time.Now(), ID: userID,
	})

	if _, err = s.store.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
		IsKycVerified: true, UpdatedAt: time.Now(), ID: userID,
	}); err != nil {
		return "", fmt.Errorf("update KYC: %w", err)
	}

	if err := qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
		ID:                           userID,
		BridgecardVerificationStatus: sql.NullString{String: "verified", Valid: true},
		UpdatedAt:                    time.Now(),
	}); err != nil {
		return "", fmt.Errorf("update verification status: %w", err)
	}

	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	s.notifyCard(userID, "Identity verified",
		"Your identity has been verified. You can now create a virtual card.")
	s.logger.Infof("cardholder %s (user=%d) verified", success.CardholderID, userID)
	return "cardholder_verification.successful", nil
}

// handleCardholderVerificationFailed marks the verification as failed and notifies the user.
func (s *Service) handleCardholderVerificationFailed(ctx context.Context, failed *bridgecards.CardholderVerificationFailed) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: failed.CardholderID, Valid: true})
	if err != nil {
		s.logger.Warnf("cardholder_verification.failed: unknown cardholder %s", failed.CardholderID)
		return "cardholder_verification.failed", nil
	}

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()
	qtx := s.store.WithTx(dbTx)

	_ = qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
		ID:                           user.ID,
		BridgecardVerificationStatus: sql.NullString{String: "failed", Valid: true},
		UpdatedAt:                    time.Now(),
	})
	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	s.notifyCard(user.ID, "Identity verification failed",
		fmt.Sprintf("Your identity verification failed: %s. Please retry or contact support.", failed.ErrorDescription))
	s.logger.Warnf("cardholder %s (user=%d) verification failed: %s",
		failed.CardholderID, user.ID, failed.ErrorDescription)
	return "cardholder_verification.failed", nil
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// extractInt64Metadata safely reads an int64 from a map[string]any.
func extractInt64Metadata(meta map[string]interface{}, key string) int64 {
	v, ok := meta[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n
		}
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
	}
	raw, _ := json.Marshal(v)
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	return 0
}
