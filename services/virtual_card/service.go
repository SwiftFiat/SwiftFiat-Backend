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

// VirtualCardService handles all virtual card business logic
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
	}
}

// this is now used for app kyc, Todo: deprecate dojah
// i am setting aside async registration because i dont see a way to update user data from webhook
func (s *Service) CreateCardHolder(ctx context.Context, userID int32, req *bridgecards.CreateCardHolderRequest) (*bridgecards.CreateCardHolderResponse, error) {
	req.Metadata = map[string]any{
		"user_id": userID,
	}

	response, err := s.bridgeCard.CreateCardHolder(ctx, req)
	if err != nil {
		return nil, err
	}

	// Start db transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	s.logger.Infof("response status is: %s", response.Status)

	if response.Status == "success" {
		// TODO: use redis
		user, err := qtx.GetUserByID(ctx, int64(userID))
		if err != nil {
			s.logger.Errorf("error getting user [CreateCardHolder]: %v", err)
			return nil, err
		}

		// Always set the cardholder ID regardless of name match
		if setErr := qtx.SetBridgeCardCardholderID(ctx, db.SetBridgeCardCardholderIDParams{
			BridgecardCardholderID: sql.NullString{String: response.Data.CardHolderID, Valid: true},
			UpdatedAt:              time.Now(),
			ID:                     int64(userID),
		}); setErr != nil {
			s.logger.Error(fmt.Sprintf("Failed to persist cardholder mapping: %v", setErr))
			return nil, setErr
		}

		// Update names if they differ
		if user.FirstName.String != req.FirstName || user.LastName.String != req.LastName {
			_, err = qtx.UpdateUserFirstName(ctx, db.UpdateUserFirstNameParams{
				FirstName: sql.NullString{String: req.FirstName, Valid: true},
				UpdatedAt: time.Now(),
				ID:        user.ID,
			})
			if err != nil {
				s.logger.Errorf("error updating firstname from kyc: %v", err)
				return nil, err
			}

			_, err = qtx.UpdateUserLastName(ctx, db.UpdateUserLastNameParams{
				LastName:  sql.NullString{String: req.LastName, Valid: true},
				ID:        user.ID,
				UpdatedAt: time.Now(),
			})
			if err != nil {
				s.logger.Errorf("error updating lastname from kyc: %v", err)
				return nil, err
			}
		}

		// update kyc
		_, err = qtx.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
			IsKycVerified: true,
			UpdatedAt:     time.Now(),
			ID:            int64(userID),
		})
		if err != nil {
			return nil, fmt.Errorf("update user kyc status error: %v", err)
		}

		// Update cardholder verification status
		err = qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
			ID:                           int64(userID),
			BridgecardVerificationStatus: sql.NullString{String: "verified", Valid: true},
			UpdatedAt:                    time.Now(),
		})
		if err != nil {
			return nil, fmt.Errorf("update cardholder verification status error: %w", err)
		}

		// Commit transaction
		if err := dbTx.Commit(); err != nil {
			return nil, fmt.Errorf("commit transaction: %w", err)
		}

		err = s.email.KycVerified(ctx, req.FirstName, user.Email)
		if err != nil {
			s.logger.Errorf("failed to send kyc verified email: %v", err)
		}

		// s.notifySvc
		go s.pushSvc.SendKYCVerifiedPushNotification(ctx, int64(userID))
		go s.notifySvc.CreateWithRecipients(ctx, nil, "Kyc verified", "Your kyc is verified", "system", []int64{int64(userID)})

		return response, nil
	} else {
		err = s.email.KycFailed(ctx, req.FirstName, req.Email, response.Message)
		if err != nil {
			s.logger.Errorf("failed to send kyc failed email: %v", err)
		}
		go s.pushSvc.SendKYCRejectedPushNotification(ctx, int64(userID), response.Message)
		go s.notifySvc.CreateWithRecipients(ctx, nil, "Kyc failed", "Your kyc is failed", "system", []int64{int64(userID)})
	}

	return response, nil

}

// CreateCard creates a new virtual card with BridgeCard and handles all setup
func (s *Service) CreateCard(ctx context.Context, params *bridgecards.CreateCardRequest) (*bridgecards.CreateCardResponse, error) {
	s.logger.Infof("IdempotencyKey 1: '%s'", params.IdempotencyKey)
	s.logger.Infof("IdempotencyKey 2: '%s'", params.IdempotencyKey2)
	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	// 1. Validate card plan
	plan, err := qtx.GetCardPlan(ctx, params.CardPlanID)
	if err != nil {
		return nil, fmt.Errorf("get card plan error: %w", err)
	}

	if !plan.IsActive || plan.DeletedAt.Valid {
		return nil, ErrInvalidCardPlan
	}

	if !plan.CardLimit.Valid || plan.CardLimit.String == "" {
		return nil, fmt.Errorf("card plan limit is not configured")
	}

	// 2. Check if user has reached plan card limit
	userActiveCardsCount, err := qtx.GetUserActiveCardsCount(ctx, params.UserID)
	if err != nil {
		return nil, fmt.Errorf("count user cards error: %w", err)
	}

	// 2. Global Constraint: Total cards cannot exceed 2
	const GlobalMaxCards = 2
	if userActiveCardsCount >= GlobalMaxCards {
		return nil, fmt.Errorf("user has reached the maximum global limit of %d cards", GlobalMaxCards)
	}

	s.logger.Infof("active cards for this user: %d", userActiveCardsCount)

	s.logger.Infof("max card for this plan: %d", plan.MaxCardsPerUser)

	// 3. Plan-specific Constraint: Check if they already have a card under THIS specific plan
	// Note: This assumes GetUserActiveCardsCountByPlan is available or logic to filter by plan
	cardsInThisPlan, err := qtx.GetUserCardsCountByPlan(ctx, db.GetUserCardsCountByPlanParams{
		UserID:     params.UserID,
		CardPlanID: params.CardPlanID,
	})
	if err != nil {
		return nil, fmt.Errorf("count user cards for plan error: %w", err)
	}

	if cardsInThisPlan >= int64(plan.MaxCardsPerUser) {
		return nil, fmt.Errorf("user has reached the limit for the %s plan", plan.Name)
	}

	creationFee, err := utils.ToDecimal(plan.CreationFee)
	if err != nil {
		return nil, fmt.Errorf("failed to convert CreationFee to decimal")
	}

	// createFeeCents := creationFee.Mul(decimal.NewFromInt(100))

	usdWallet, err := s.store.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
		CustomerID: params.UserID,
		Currency:   "USD",
	})
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}

	// 4. Verify user has sufficient wallet balance
	sourceWallet, err := s.walletService.GetWalletForUpdate(ctx, dbTx, usdWallet.ID)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}

	// Convert FundingAmount (dollars string) → cents string
	fundingAmountDecimal, err := utils.ToDecimal(params.FundingAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid funding amount: %w", err)
	}

	fundingCentsStr, err := utils.DollarStringToCentsString(params.FundingAmount)
	if err != nil {
		return nil, fmt.Errorf("dollar to cent error: %v", err)
	}

	s.logger.Infof("funding amount in request %s", params.FundingAmount)
	s.logger.Infof("creation fee $%s", creationFee.String())

	cardLimitCents, err := utils.DollarStringToCentsString(plan.CardLimit.String)
	if err != nil {
		return nil, fmt.Errorf("card limit dollar to cent error: %v", err)
	}

	// enforce minimum depending on card limit
	var minFundingCents decimal.Decimal
	switch cardLimitCents {
	case "500000":
		minFundingCents = decimal.NewFromInt(300) // $3 → 300 cents
	case "1000000":
		minFundingCents = decimal.NewFromInt(400) // $4 → 400 cents
	default:
		return nil, fmt.Errorf("invalid card limit: %s, card limit must be '50000' or '100000'", plan.CardLimit.String)
	}

	fundingCentsDecimal := decimal.RequireFromString(fundingCentsStr)
	if fundingCentsDecimal.LessThan(minFundingCents) {
		return nil, fmt.Errorf("funding amount must be at least %s cents for card limit %s", minFundingCents.String(), plan.CardLimit.String)
	}

	totalCost := creationFee.Add(fundingAmountDecimal)

	s.logger.Infof("total cost is $%s", totalCost.String())

	if sourceWallet.Balance.LessThan(totalCost) {
		return nil, ErrInsufficientFunds
	}

	// creation fee tx record
	cardCreationTx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          params.UserID,
		Type:            string(transaction.Card),
		Description:     sql.NullString{String: "card-creation-fee", Valid: true},
		Amount:          creationFee.String(),
		Currency:        "USD",
		AmountUsd:       creationFee.String(),
		Status:          string(transaction.Pending),
		TransactionFlow: string(transaction.Outflow),
		IdempotencyKey:  params.IdempotencyKey,
		TFrom:           string(transaction.Wallet),
		TTo:             "card_provider",
		Direction:       string(transaction.Debit),
	})
	if err != nil {
		return nil, fmt.Errorf("create card transaction failed: %w", err)
	}

	// 5. Create card in BridgeCard
	bridgeCardReq := &bridgecards.CreateCardRequest{
		CardHolderID:         params.CardHolderID, // get from user data
		CardType:             "virtual",
		Brand:                "Mastercard", //Visa is not supported yet
		Currency:             "USD",
		CardLimit:            cardLimitCents,  // (can either be $5,000 i.e "500000" or $10,000 i.e "1000000")
		FundingAmount:        fundingCentsStr, // (a minimum of $3 i.e "300" for cards with a spending limit of $5,000 and $4 i.e "400" for a card with a spending limit of $10,000)
		TransactionReference: utils.NewTxRef("c_"),
		SourceWalletID:       usdWallet.ID,
	}

	s.logger.Infof("bridgeCardReq is : %v", bridgeCardReq)

	// Attempt to create card in BridgeCard
	bridgeCardDetails, err := s.bridgeCard.CreateCard(ctx, bridgeCardReq)
	if err != nil {
		return nil, fmt.Errorf("create card in bridgecard err: %v", err)
	}

	// Check BridgeCard response status.
	// If not "success", return error
	if bridgeCardDetails.Status != "success" {
		errBytes, _ := json.Marshal(bridgeCardDetails)
		return nil, fmt.Errorf("bridgecard create card failed: %s", string(errBytes))
	}

	// 6. Create card record in our database
	now := time.Now()
	nextBillingDate := now.AddDate(0, 1, 0) // One month from now

	// initial funding tx record
	fundcardTx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          params.UserID,
		Type:            string(transaction.Card),
		Description:     sql.NullString{String: "card-funding", Valid: true},
		Amount:          fundingAmountDecimal.String(),
		Currency:        "USD",
		AmountUsd:       fundingAmountDecimal.String(),
		Status:          string(transaction.Pending),
		TransactionFlow: string(transaction.Outflow),
		IdempotencyKey:  params.IdempotencyKey2,
		TFrom:           string(transaction.Wallet),
		TTo:             "card_provider",
		Direction:       string(transaction.Debit),
	})
	if err != nil {
		// Handle duplicate key error gracefully
		if strings.Contains(err.Error(), "duplicate key") && strings.Contains(err.Error(), "idempotency_key") {
			s.logger.Warn(fmt.Sprintf("Duplicate card funding request for idempotency key: %s", params.IdempotencyKey2))
			return nil, fmt.Errorf("this card funding request is already in progress or was completed")
		}
		return nil, fmt.Errorf("card funding transaction failed: %w", err)
	}

	dbCard, err := qtx.CreateVirtualCard(ctx, db.CreateVirtualCardParams{
		UserID:           params.UserID,
		CardPlanID:       params.CardPlanID,
		BridgecardCardID: bridgeCardDetails.Data.CardID,
		CardName:         plan.Name,
		CardColor:        sql.NullString{String: params.CardColor, Valid: params.CardColor != ""},
		Currency:         "USD",
		Status:           string(VirtualCardStatusActive),
		NextBillingDate:  sql.NullTime{Time: nextBillingDate, Valid: true},
		SpendingMonth:    sql.NullString{String: now.Format("2006-01"), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("create card record in db error: %w", err)
	}

	// create card transaction for creation fee
	s.logger.Infof("About to create card creation fee transaction with bridgecard_id: cc_%s", uuid.New().String())
	cardCreationtxmeta, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID:                  dbCard.ID,
		UserID:                  params.UserID,
		Amount:                  creationFee.IntPart(),
		Currency:                "USD",
		Status:                  string(transaction.Pending),
		TransactionType:         string(transaction.Credit),
		TransactionDate:         now,
		WebhookReceivedAt:       sql.NullTime{Time: now, Valid: true},
		BalanceAfter:            sql.NullString{String: fundingAmountDecimal.String(), Valid: true},
		Mode:                    true,
		TransactionTimestamp:    now,
		BridgecardTransactionID: "cc_" + uuid.New().String(),
		TransactionID:           cardCreationTx.ID,
	})
	if err != nil {
		s.logger.Errorf("failed to create card creation transaction: %v", err)
		return nil, fmt.Errorf("create card creation transaction error: %w", err)
	}
	s.logger.Infof("Successfully created card creation fee transaction with ID: %s", cardCreationtxmeta.ID)

	// create card transaction for funding
	s.logger.Infof("About to create card funding transaction with bridgecard_id: cf_%s", uuid.New().String())
	cardFundingtxmeta, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID:                  dbCard.ID,
		UserID:                  params.UserID,
		Amount:                  fundingAmountDecimal.IntPart(),
		Currency:                "USD",
		Status:                  string(transaction.Pending),
		TransactionType:         string(transaction.Credit),
		TransactionDate:         now,
		WebhookReceivedAt:       sql.NullTime{Time: now, Valid: true},
		BalanceAfter:            sql.NullString{String: fundingAmountDecimal.String(), Valid: true},
		Mode:                    true,
		TransactionTimestamp:    now,
		BridgecardTransactionID: "cf_" + uuid.New().String(),
		TransactionID:           fundcardTx.ID,
	})
	if err != nil {
		s.logger.Errorf("failed to create card funding transaction: %v", err)
		return nil, fmt.Errorf("create card funding transaction error: %w", err)
	}
	s.logger.Infof("Successfully created card funding transaction with ID: %s", cardFundingtxmeta.ID)

	// Log creation fee billing
	// TODO: card billing should point to a transaction record
	billingTx, err := qtx.CreateCardBilling(ctx, db.CreateCardBillingParams{
		CardID:             dbCard.ID,
		UserID:             params.UserID,
		CardPlanID:         params.CardPlanID,
		BillingType:        "creation_fee",
		Amount:             creationFee.String(),
		Currency:           "USD",
		BillingPeriodStart: now, // TODO: change this to the start of the billing period
		BillingPeriodEnd:   now, // TODO: change this to the end of the billing period
		SourceWalletID:     usdWallet.ID,
		Status:             string(transaction.Pending),
		TransactionID:      cardCreationTx.ID,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to log creation fee: %v", err))
	}

	// 9. Log funding record
	fundingTx, err := qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID:         dbCard.ID,
		UserID:         params.UserID,
		SourceWalletID: usdWallet.ID,
		Amount:         fundingAmountDecimal.String(),
		Currency:       "USD",
		SourceCurrency: usdWallet.Currency,
		FundingType:    "manual",
		InitiatedBy:    "user",
		Status:         string(transaction.Pending),
		TransactionID:  fundcardTx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create funding record: %w", err)
	}

	_, err = qtx.DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		Balance: sql.NullString{String: totalCost.String(), Valid: true},
		ID:      usdWallet.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("deduct funds from wallet error: %w", err)
	}

	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     cardCreationTx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("update card creation transaction status error: %w", err)
	}

	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     fundcardTx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("update card funding transaction status error: %w", err)
	}

	_, err = qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{
		ID:     cardCreationtxmeta.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("update card creation meta transaction status error: %w", err)
	}

	_, err = qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{
		ID:     cardFundingtxmeta.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("update card funding meta transaction status error: %w", err)
	}

	_, err = qtx.UpdateCardBillingStatus(ctx, db.UpdateCardBillingStatusParams{
		ID:            billingTx.ID,
		Status:        string(transaction.Success),
		FailureReason: sql.NullString{String: "", Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("update card billing status error: %w", err)
	}

	_, err = qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
		ID:     fundingTx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("update card funding status error: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     cardCreationTx.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("update card creation transaction status error: %w", err)
		}

		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     fundcardTx.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("update card funding transaction status error: %w", err)
		}

		_, err = qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{
			ID:     cardCreationtxmeta.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("update card creation meta transaction status error: %w", err)
		}

		_, err = qtx.UpdateCardTransactionStatus(ctx, db.UpdateCardTransactionStatusParams{
			ID:     cardFundingtxmeta.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("update card funding meta transaction status error: %w", err)
		}

		_, err = qtx.UpdateCardBillingStatus(ctx, db.UpdateCardBillingStatusParams{
			ID:     billingTx.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("update card billing status error: %w", err)
		}

		_, err = qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
			ID:     fundingTx.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("update card funding status error: %w", err)
		}
		return nil, fmt.Errorf("commit transaction error: %w", err)
	}

	// TODO: Send notification to user about card creation
	go func() {
		if s.notifySvc != nil {
			if _, err := s.notifySvc.CreateWithRecipients(ctx, nil, "Virtual card created", "Your virtual card has been created successfully", "system", []int64{params.UserID}); err != nil {
				s.logger.Errorf("failed to create notification: %v", err)
			}
		}
	}()

	s.logger.Info(fmt.Sprintf("Created virtual card %s for user %d", bridgeCardDetails.Data.CardID, params.UserID))

	s.logger.Infof("create card result in service is ====: %v", bridgeCardDetails)
	return &bridgecards.CreateCardResponse{
		Status:  bridgeCardDetails.Status,
		Message: bridgeCardDetails.Message,
		Data:    bridgeCardDetails.Data,
	}, nil

}

func (s *Service) GetCardBalance(ctx context.Context, cardID string) (*bridgecards.GetCardBalanceResponse, error) {
	return s.bridgeCard.GetCardBalance(ctx, cardID)
}

func (s *Service) FundIssuingWallet(ctx context.Context, req bridgecards.FundIssuingWalletRequest) string {
	message, err := s.bridgeCard.FundIssuingWallet(ctx, req)
	if err != nil {
		return err.Error()
	}
	return *message
}

// VerifyWebhookSignature verifies BridgeCard webhook signatures
func (s *Service) VerifyWebhookSignature(payload []byte, signature string) (bool, error) {
	return s.bridgeCard.VerifyWebhookSignature(payload, signature)
}

func (s *Service) FundCard(ctx context.Context, req bridgecards.FundCardRequest, userID int64) (*bridgecards.FundCardResponse, error) {
	// get user usd wallet
	wallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: userID,
		Currency:   "USD",
	})
	if err != nil {
		return nil, fmt.Errorf("get user usd wallet error: %w", err)
	}

	walletBalance, err := utils.ToDecimal(wallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("convert wallet balance to decimal error: %w", err)
	}

	fundingAmount, err := utils.ToDecimal(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("convert funding amount to decimal error: %w", err)
	}

	fundingAmountCents, err := utils.DollarStringToCentsString(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("convert funding amount to cents error: %w", err)
	}

	// check insufficient fund
	if walletBalance.LessThan(fundingAmount) {
		return nil, fmt.Errorf("insufficient funds")
	}

	req.Amount = fundingAmountCents

	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, req.CardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}

	tx, err := s.store.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: userID,
		Type:   string(transaction.Card),
		Description: sql.NullString{
			String: fmt.Sprintf("Funding card %s", req.CardID),
			Valid:  true,
		},
		Amount:          fundingAmount.String(),
		Currency:        "USD",
		AmountUsd:       fundingAmount.String(),
		Status:          string(transaction.Pending),
		TransactionFlow: string(transaction.Outflow),
		IdempotencyKey:  req.TransactionReference,
		TFrom:           string(transaction.Wallet),
		TTo:             "card_provider",
		Direction:       string(transaction.Debit),
	})
	if err != nil {
		return nil, fmt.Errorf("create funding transaction error: %w", err)
	}

	// Create funding record first
	fundingRecord, err := s.store.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID:         card.ID,
		UserID:         userID,
		SourceWalletID: wallet.ID,
		Amount:         fundingAmount.String(),
		Currency:       "USD",
		SourceCurrency: wallet.Currency,
		FundingType:    "manual",
		InitiatedBy:    "user",
		Status:         string(CardFundingStatusPending),
		TransactionID:  tx.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("create card funding record error: %w", err)
	}

	_, err = s.store.DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		ID: wallet.ID,
		Balance: sql.NullString{
			String: fundingAmount.String(),
			Valid:  true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("update wallet balance error: %w", err)
	}

	bridgeResponse, err := s.bridgeCard.FundCard(ctx, req)
	if err != nil {
		_, updateErr := s.store.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     tx.ID,
			Status: string(transaction.Failed),
		})
		if updateErr != nil {
			s.logger.Error(fmt.Sprintf("failed to update transaction status: %v", updateErr))
		}
		// Update funding status to failed if BridgeCard call fails
		_, updateErr = s.store.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
			ID:            fundingRecord.ID,
			Status:        string(CardFundingStatusFailed),
			FailureReason: sql.NullString{String: err.Error(), Valid: true},
		})
		if updateErr != nil {
			s.logger.Error(fmt.Sprintf("failed to update funding status: %v", updateErr))
		}

		// refund wallet
		_, err = s.store.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			ID: wallet.ID,
			Balance: sql.NullString{
				String: fundingAmount.String(),
				Valid:  true,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("update wallet balance error: %w", err)
		}
		return nil, fmt.Errorf("fund card via bridgecard error: %w", err)
	}

	// remove if not work
	_, err = s.store.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
		ID:            fundingRecord.ID,
		Status:        string(transaction.Success),
		FailureReason: sql.NullString{String: "", Valid: true},
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update funding status: %v", err))
	}

	_, err = s.store.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     tx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to update transaction status: %v", err))
	}

	return bridgeResponse, nil
}

func (s *Service) AdminFreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}

	cardDetails, err := s.bridgeCard.FreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("freeze card via bridgecard error: %w", err)
	}

	// update virtual card status
	_, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID:     card.ID,
		Status: string(VirtualCardStatusFrozen),
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

	// TODO: send notifications

	return cardDetails, nil
}

func (s *Service) FreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}

	cardDetails, err := s.bridgeCard.FreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("freeze card via bridgecard error: %w", err)
	}

	// update virtual card status
	_, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID:     card.ID,
		Status: string(VirtualCardStatusFrozen),
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

	// TODO: send notifications

	return cardDetails, nil
}

func (s *Service) AdminUnfreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}

	cardDetails, err := s.bridgeCard.UnfreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("unfreeze card via bridgecard error: %w", err)
	}

	// update virtual card status
	_, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID:     card.ID,
		Status: string(VirtualCardStatusActive),
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

	// TODO: send notifications

	return cardDetails, nil
}

func (s *Service) UnfreezeCard(ctx context.Context, cardID string, userID int64) (*bridgecards.FreezeCardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}

	cardDetails, err := s.bridgeCard.UnfreezeCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("unfreeze card via bridgecard error: %w", err)
	}

	// update virtual card status
	_, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID:     card.ID,
		Status: string(VirtualCardStatusActive),
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

	// TODO: send notifications

	return cardDetails, nil
}

func (s *Service) UpdateCardPin(ctx context.Context, req bridgecards.UpdateCardPinRequest, userID int64) (*bridgecards.CardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, req.CardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}
	// TODO: send notifications
	return s.bridgeCard.UpdateCardPin(ctx, req)
}

func (s *Service) AdminDeleteCard(ctx context.Context, cardID uuid.UUID, userID int64) (*bridgecards.CardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}

	cardDetails, err := s.bridgeCard.DeleteCard(ctx, card.BridgecardCardID)
	if err != nil {
		return nil, fmt.Errorf("delete card via bridgecard error: %w", err)
	}

	// update virtual card status
	_, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
		ID:     card.ID,
		Status: string(VirtualCardStatusTerminated),
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

	go func() {
		err := s.pushSvc.AdminTerminateCardNotification(ctx, card.UserID, card.CardName)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Error sending admin terminate card push notification: %v", err))
		}

		_, err = s.notifySvc.CreateWithRecipients(ctx, nil, "Virtual Card Terminated", "Your virtual card has been terminated by an administrator.", "system", []int64{card.UserID})
		if err != nil {
			s.logger.Error(fmt.Sprintf("Error creating admin terminate card inapp notification: %v", err))
		}
	}()

	return cardDetails, nil
}

func (s *Service) DeleteCard(ctx context.Context, cardID uuid.UUID, userID int64) (*bridgecards.CardResponse, error) {
	// check if card belongs to user
	card, err := s.store.GetVirtualCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("get virtual card error: %w", err)
	}
	if card.UserID != userID {
		return nil, fmt.Errorf("card does not belong to user")
	}

	cardDetails, err := s.bridgeCard.DeleteCard(ctx, card.BridgecardCardID)
	if err != nil {
		return nil, fmt.Errorf("delete card via bridgecard error: %w", err)
	}

	// // update virtual card status
	// _, err = s.store.UpdateCardStatus(ctx, db.UpdateCardStatusParams{
	// 	ID:     card.ID,
	// 	Status: string(VirtualCardStatusTerminated),
	// })
	// if err != nil {
	// 	return nil, fmt.Errorf("update virtual card status error: %w", err)
	// }

	// update virtual card status
	_, err = s.store.TerminateCard(ctx, db.TerminateCardParams{
		ID:     card.ID,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("terminate card error: %w", err)
	}

	// TODO: send notifications

	return cardDetails, nil
}

func (s *Service) ListCardsFromProvider(ctx context.Context, cardholderID string, userID int64) (*bridgecards.ListCardsResponse, error) {
	return s.bridgeCard.ListCards(ctx, cardholderID)
}

func (s *Service) ListCardsFromDB(ctx context.Context, userID int64) ([]db.GetUserCardsRow, error) {
	return s.store.GetUserCards(ctx, userID)
}

func (s *Service) GetCardDetails(ctx context.Context, cardID string, userID int64) (*bridgecards.GetCardDetailsResponse, error) {
	// check if card belongs to user
	// card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	// if err != nil {
	// 	return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	// }
	// if card.UserID != userID {
	// 	return nil, fmt.Errorf("card does not belong to user")
	// }
	return s.bridgeCard.GetCardDetails(ctx, cardID)
}

func (s *Service) DebitCard(ctx context.Context, cardID string, userID int64) (*bridgecards.DebitCardResponse, error) {
	// check if card belongs to user
	// card, err := s.store.GetVirtualCardByBridgeCardID(ctx, cardID)
	// if err != nil {
	// 	return nil, fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	// }
	// if card.UserID != userID {
	// 	return nil, fmt.Errorf("card does not belong to user")
	// }
	return s.bridgeCard.DebitCard(ctx, bridgecards.DebitCardRequest{CardID: cardID})
}

func (s *Service) GetCardTransaction(ctx context.Context, cardID string, userID int64) (*bridgecards.GetCardTransactionResponse, error) {
	return s.bridgeCard.GetCardTransaction(ctx, cardID)
}

func (s *Service) ListCardTransactions(ctx context.Context, req bridgecards.ListCardTransactionsRequest) (*bridgecards.ListCardTransactionsResponse, error) {
	return s.bridgeCard.ListCardTransactions(ctx, req)
}

func (s *Service) GetCardTransactionStatus(ctx context.Context, cardID string, clientTransactionReference string, userID int64) (*bridgecards.GetCardTransactionStatusResponse, error) {
	return s.bridgeCard.GetCardTransactionStatus(ctx, cardID, clientTransactionReference)
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

func (s *Service) handleCardDebitEventSuccess(ctx context.Context, success *bridgecards.CardDebitEventSuccessful) (string, error) {
	now := time.Now()
	spendingMonth := now.Format("2006-01")
	spendingDay := now.Format("2006-01-02")
	// Get user by cardholderID
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: success.Data.CardholderID, Valid: true})
	if err != nil {
		return "", fmt.Errorf("failed to get user from cardholderID: %w", err)
	}

	amountString, err := utils.CentsStringToDollarString(success.Data.Amount)
	if err != nil {
		return "", fmt.Errorf("failed to convert amount from cent to dollars: %w", err)
	}

	amount, err := utils.ToDecimal(amountString)
	if err != nil {
		return "", fmt.Errorf("failed to convert amount from string to decimal: %w", err)
	}

	// start db transaction
	tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.store.WithTx(tx)

	card, err := qtx.GetVirtualCardByBridgeCardID(ctx, success.Data.CardID)
	if err != nil {
		return "", fmt.Errorf("failed to get virtual card by bridge card ID: %w", err)
	}

	// marshal success to json
	rawWebhookData, err := json.Marshal(success)
	if err != nil {
		return "", fmt.Errorf("failed to marshal success data: %w", err)
	}

	amountUsd, err := utils.ConvertToUSD(ctx, amount, success.Data.Currency)
	if err != nil {
		return "", fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	// Create transaction
	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		Type:            string(transaction.Card),
		Description:     sql.NullString{String: "Card Debit", Valid: true},
		TransactionFlow: string(transaction.Outflow),
		Status:          string(transaction.Success),
		Amount:          amount.String(), // store in dollars
		AmountUsd:       amountUsd.String(),
		UserID:          card.UserID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}

	// Extract merchant name from webhook's "description" field
	merchantName := success.Data.Description
	if merchantName == "" {
		merchantName = "Unknown Merchant"
	}

	// Parse transaction date from webhook format "2025-12-22 12:04:21"
	transactionDate, err := time.Parse("2006-01-02 15:04:05", success.Data.TransactionDate)
	if err != nil {
		return "", fmt.Errorf("failed to parse transaction date: %w", err)
	}

	// Parse transaction timestamp from string (Unix timestamp)
	transactionTimestamp, err := time.Parse("2006-01-02 15:04:05", success.Data.TransactionTimestamp)
	if err != nil {
		// If parsing as datetime fails, try parsing as Unix timestamp
		if timestampInt, parseErr := strconv.ParseInt(success.Data.TransactionTimestamp, 10, 64); parseErr == nil {
			transactionTimestamp = time.Unix(timestampInt, 0)
		} else {
			return "", fmt.Errorf("failed to parse transaction timestamp: %w", err)
		}
	}

	// create card transaction
	cardTx, err := qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID:                  card.ID,
		UserID:                  user.ID,
		BridgecardTransactionID: success.Data.TransactionReference,
		TransactionType:         success.Data.CardTransactionType,
		MerchantName:            sql.NullString{String: merchantName, Valid: true},
		MerchantCategory:        sql.NullString{Valid: false}, // Not provided in webhook
		MerchantCategoryCode:    sql.NullString{String: success.Data.MerchantCategoryCode, Valid: success.Data.MerchantCategoryCode != ""},
		Amount:                  amount.IntPart(),
		Fee:                     0, // Fee not provided in debit webhook
		Currency:                success.Data.Currency,
		BillingAmount:           sql.NullInt64{Valid: false},  // Not provided in webhook
		BillingCurrency:         sql.NullString{Valid: false}, // Not provided in webhook
		Status:                  string(transaction.Success),
		BalanceAfter:            sql.NullString{String: success.Data.SettledBookBalance, Valid: success.Data.SettledBookBalance != ""},
		TransactionDate:         transactionDate,
		WebhookReceivedAt:       sql.NullTime{Time: now, Valid: true},
		RawWebhookData:          pqtype.NullRawMessage{RawMessage: rawWebhookData, Valid: true},
		TransactionID:           txx.ID,
		Mode:                    success.Data.Livemode,
		TransactionTimestamp:    transactionTimestamp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create card transaction: %w", err)
	}

	// update card spending
	_, err = qtx.UpdateCardSpending(ctx, db.UpdateCardSpendingParams{
		CurrentMonthSpend: sql.NullInt64{Int64: amount.IntPart(), Valid: true},
		ID:                card.ID,
		SpendingMonth:     sql.NullString{String: spendingMonth, Valid: true},
		SpendingDay:       sql.NullString{String: spendingDay, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("failed to update card spending: %w", err)
	}

	// commit transaction
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Detect and log subscription after transaction is committed
	if s.subscriptionSvc != nil {
		// Convert to proper CardTransaction struct for subscription detection
		subscriptionCardTx := db.CardTransaction{
			ID:                      cardTx.ID,
			CardID:                  cardTx.CardID,
			UserID:                  cardTx.UserID,
			BridgecardTransactionID: cardTx.BridgecardTransactionID,
			Amount:                  cardTx.Amount,
			Currency:                cardTx.Currency,
			Status:                  cardTx.Status,
			TransactionType:         cardTx.TransactionType,
			TransactionDate:         cardTx.TransactionDate,
			MerchantName:            cardTx.MerchantName,
			MerchantCategoryCode:    cardTx.MerchantCategoryCode,
			TransactionTimestamp:    cardTx.TransactionTimestamp,
		}
		// Run subscription detection asynchronously to avoid blocking webhook response
		go func() {
			detectionCtx := context.Background()
			if err := s.subscriptionSvc.DetectAndLogSubscription(detectionCtx, &subscriptionCardTx); err != nil {
				s.logger.Error(fmt.Sprintf("Subscription detection failed for transaction %s: %v",
					cardTx.BridgecardTransactionID, err))
			} else {
				s.logger.Info(fmt.Sprintf("Subscription detection completed for transaction %s",
					cardTx.BridgecardTransactionID))
			}
		}()
	}

	// update streak
	if err := s.streak.UpdateStreakOnTransaction(ctx, user.ID, txx.ID, "card"); err != nil {
		return "", err
	}

	return "card_debit_event.successful", nil
}

func (s *Service) handleCardDebitEventFailed(ctx context.Context, failed *bridgecards.CardDebitEventDeclined) (string, error) {
	return "card_debit_event.failed", nil
}

func (s *Service) processCardCreationEvent(ctx context.Context, payload []byte) (string, error) {
	creationEvent, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card creation event: %w", err)
	}

	switch c := creationEvent.(type) {
	case *bridgecards.CardCreationEventSuccessful:
		return s.handleCardCreationEventSuccess(ctx, c)

	case *bridgecards.CardCreationEventFailed:
		return s.handleCardCreationEventFailed(ctx, c)

	default:
		return "", fmt.Errorf("unknown creation event type")
	}
}

func (s *Service) handleCardCreationEventSuccess(ctx context.Context, success *bridgecards.CardCreationEventSuccessful) (string, error) {
	// TODO: create virtual card
	// TODO: send notifications

	return "card_creation_event.successful", nil
}

func (s *Service) handleCardCreationEventFailed(ctx context.Context, failed *bridgecards.CardCreationEventFailed) (string, error) {
	// TODO: send notifications
	return "card_creation_event.failed", nil
}

// handleCardholderVerificationSuccess handles successful cardholder verification [deprecated]
func (s *Service) handleCardholderVerificationSuccess(ctx context.Context, success *bridgecards.CardholderVerificationSuccess) (string, error) {
	s.logger.Info(fmt.Sprintf("Looking up user for cardholder_id: %s", success.CardholderID))

	ch, chErr := s.bridgeCard.GetCardHolder(ctx, success.CardholderID)
	if chErr != nil {
		return "", fmt.Errorf("user not found for cardholder_id %s: %w", success.CardholderID, chErr)
	}

	// Attempt to extract user_id from cardholder metadata
	var foundUID int64
	if ch != nil && ch.Metadata != nil {
		if v, ok := ch.Metadata["user_id"]; ok && v != nil {
			switch t := v.(type) {
			case float64:
				foundUID = int64(t)
			case int:
				foundUID = int64(t)
			case int64:
				foundUID = t
			case string:
				if parsed, perr := strconv.ParseInt(t, 10, 64); perr == nil {
					foundUID = parsed
				}
			case json.Number:
				if parsed, perr := t.Int64(); perr == nil {
					foundUID = parsed
				}
			default:
				// try to marshal/unmarshal as fallback
				b, _ := json.Marshal(v)
				var tmp int64
				if uerr := json.Unmarshal(b, &tmp); uerr == nil {
					foundUID = tmp
				}
			}
		}
	}

	if foundUID == 0 {
		return "", fmt.Errorf("user not found for cardholder_id %s after fetching cardholder details", success.CardholderID)
	}
	s.logger.Info(fmt.Sprintf("Found user %d for cardholder %s", foundUID, success.CardholderID))

	// Start db transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	// set SetBridgeCardCardholderID to user
	if setErr := s.store.SetBridgeCardCardholderID(ctx, db.SetBridgeCardCardholderIDParams{
		BridgecardCardholderID: sql.NullString{String: success.CardholderID, Valid: true},
		UpdatedAt:              time.Now(),
		ID:                     foundUID,
	}); setErr != nil {
		s.logger.Error(fmt.Sprintf("Failed to persist cardholder mapping: %v", setErr))
	}

	// update kyc
	_, err = s.store.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
		IsKycVerified: true,
		UpdatedAt:     time.Now(),
		ID:            foundUID,
	})

	if err != nil {
		return "", fmt.Errorf("updated user kyc error: %v", err)
	}

	// Update cardholder verification status
	err = qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
		ID:                           foundUID,
		BridgecardVerificationStatus: sql.NullString{String: "verified", Valid: true},
		UpdatedAt:                    time.Now(),
	})
	if err != nil {
		return "", fmt.Errorf("update cardholder verification status error: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: Send notification to user about successful verification

	s.logger.Info(fmt.Sprintf("Cardholder %s (user_id: %d) successfully verified",
		success.CardholderID, foundUID))

	return "cardholder_verification.successful", nil
}

// handleCardholderVerificationFailed handles failed cardholder verification [deprecated]
func (s *Service) handleCardholderVerificationFailed(ctx context.Context, failed *bridgecards.CardholderVerificationFailed) (string, error) {
	// Look up user by cardholder_id
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{
		String: failed.CardholderID,
		Valid:  true,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("User not found for failed verification cardholder_id %s: %v",
			failed.CardholderID, err))
		// Don't return error - still acknowledge webhook receipt
		return "cardholder_verification.failed", nil
	}

	// Update status to failed
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	err = qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
		ID:                           1, // Todo:
		BridgecardVerificationStatus: sql.NullString{String: "failed", Valid: true},
		UpdatedAt:                    time.Now(),
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to update verification status: %v", err))
	}

	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: Send notification to user about failed verification
	s.logger.Warn(fmt.Sprintf("Cardholder %s (user_id: %d) verification failed: %s",
		failed.CardholderID, user.ID, failed.ErrorDescription))

	return "cardholder_verification.failed", nil
}

func (s Service) handleCardCreditFailed(ctx context.Context, failed *bridgecards.CardCreditFailed) (string, error) {
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	// refund user
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{String: failed.CardholderID, Valid: true})
	if err != nil {
		return "", err
	}

	// get wallet for refund
	usdWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   "USD",
	})
	if err != nil {
		return "", err
	}

	card, err := s.store.GetVirtualCardByBridgeCardID(ctx, failed.CardID)
	if err != nil {
		return "", err
	}

	// refund
	walletBalance, err := utils.ToDecimal(usdWallet.Balance.String)
	if err != nil {
		return "", err
	}

	// convert cent to dollar
	failedAmount, err := utils.CentsStringToDollarString(failed.Amount)
	if err != nil {
		return "", fmt.Errorf("failed to convert failed amount cent to dollar:  %v", err)
	}

	failedAmountDecimal, err := utils.ToDecimal(failedAmount)
	if err != nil {
		return "", fmt.Errorf("failed to convert failed amount dollar to decimal:  %v", err)
	}

	newbalance := walletBalance.Add(failedAmountDecimal)

	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		Amount: sql.NullString{String: newbalance.String(), Valid: true},
		ID:     usdWallet.ID,
	})
	if err != nil {
		// return "", fmt.Errorf("failed to refund user balance: %v", err)
		// save error before returning
		walletErr := err

		// update card status with failure reason
		_, _ = s.store.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
			ID:            card.ID,
			Status:        string(CardFundingStatusFailed),
			FailureReason: sql.NullString{String: walletErr.Error(), Valid: true},
		})

		return "", fmt.Errorf("failed to refund user balance: %v", walletErr)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: send notifications
	return "card_credit.failed", nil
}

func (s Service) handleCardCreditSuccess(ctx context.Context, success *bridgecards.CardCreditSuccess) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{
		String: success.CardholderID,
		Valid:  true,
	})
	if err != nil {
		return "", fmt.Errorf("get user by bridgecard cardholder id error: %w", err)
	}

	usdWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   "USD",
	})
	if err != nil {
		return "", fmt.Errorf("get wallet by user id and currency error: %w", err)
	}

	walletBalance, err := utils.ToDecimal(usdWallet.Balance.String)
	if err != nil {
		return "", fmt.Errorf("convert wallet balance to decimal error: %w", err)
	}

	amount, err := utils.ToDecimal(success.Amount)
	if err != nil {
		return "", fmt.Errorf("convert amount to decimal error: %w", err)
	}

	if walletBalance.LessThan(amount) {
		return "", fmt.Errorf("insufficient balance")
	}

	newBalance := walletBalance.Sub(amount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: usdWallet.ID,
		Amount: sql.NullString{
			String: newBalance.String(),
			Valid:  true,
		},
	})
	if err != nil {
		return "", fmt.Errorf("update wallet balance error: %w", err)
	}

	card, err := qtx.GetVirtualCardByBridgeCardID(ctx, success.CardID)
	if err != nil {
		return "", fmt.Errorf("get virtual card by bridgecard id error: %w", err)
	}

	_, err = qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID:         card.ID,
		Amount:         success.Amount,
		Currency:       success.Currency,
		SourceWalletID: usdWallet.ID,
		UserID:         user.ID,
		SourceCurrency: usdWallet.Currency,
		FundingType:    "manual",
		InitiatedBy:    "user",
		Status:         string(CardFundingStatusSuccessful),
	})
	if err != nil {
		return "", fmt.Errorf("create card funding error: %w", err)
	}

	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            string(transaction.Card),
		TransactionFlow: string(transaction.Inflow),
		Description:     sql.NullString{String: "Card funding", Valid: true},
		Amount:          amount.String(),
		Currency:        "USD",
		AmountUsd:       amount.String(),
		Status:          string(transaction.Success),
	})
	if err != nil {
		return "", fmt.Errorf("create transaction error: %w", err)
	}

	_, err = qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID:                  card.ID,
		UserID:                  user.ID,
		BridgecardTransactionID: success.TransactionReference,
		Amount:                  amount.IntPart(),
		Currency:                success.Currency,
		TransactionType:         success.CardTransactionType,
		Fee:                     0,
		Status:                  string(transaction.Success),
		Mode:                    success.Livemode,
		TransactionDate:         success.TransactionDate,
		TransactionTimestamp:    success.TransactionTimestamp,
		BalanceAfter:            sql.NullString{String: success.SettledBookBalance, Valid: true},
		TransactionID:           txx.ID,
	})
	if err != nil {
		return "", fmt.Errorf("create card transaction error: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	// update streak
	if err := s.streak.UpdateStreakOnTransaction(ctx, user.ID, txx.ID, "card"); err != nil {
		return "", err
	}

	go func() {
		message := fmt.Sprintf("You have successfully credited your card with %s %s", success.Amount, success.Currency)
		if s.notifySvc != nil {
			if _, err := s.notifySvc.CreateWithRecipients(ctx, nil, "Virtual card credited", message, "system", []int64{user.ID}); err != nil {
				s.logger.Errorf("failed to create notification: %v", err)
			}
		}

		// TODO: email

		// TODO: push notification
	}()
	return "card_credit.success", nil
}

func (s *Service) handleCardUnloadEventSuccess(ctx context.Context, success *bridgecards.CardWithDrawEventSuccessful) (string, error) {
	user, err := s.store.GetUserByBridgeCardCardholderID(ctx, sql.NullString{
		String: success.Data.CardholderID,
		Valid:  true,
	})
	if err != nil {
		return "", fmt.Errorf("get user by bridgecard cardholder id error: %w", err)
	}

	usdWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   "USD",
	})
	if err != nil {
		return "", fmt.Errorf("get wallet by user id and currency error: %w", err)
	}

	withdrawAmountString, err := utils.CentsStringToDollarString(success.Data.Amount)
	if err != nil {
		return "", fmt.Errorf("convert cents to dollar error: %w", err)
	}

	withdrawAmount, err := utils.ToDecimal(withdrawAmountString)
	if err != nil {
		return "", fmt.Errorf("convert amount to decimal error: %w", err)
	}

	walletBalance, err := utils.ToDecimal(usdWallet.Balance.String)
	if err != nil {
		return "", fmt.Errorf("convert wallet balance to decimal error: %w", err)
	}

	newBalance := walletBalance.Add(withdrawAmount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: usdWallet.ID,
		Amount: sql.NullString{
			String: newBalance.String(),
			Valid:  true,
		},
	})
	if err != nil {
		return "", fmt.Errorf("update wallet balance error: %w", err)
	}

	txx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          user.ID,
		Type:            "card",
		TransactionFlow: string(transaction.InPlatform),
		Description:     sql.NullString{String: "Card withdrawal", Valid: true},
		Amount:          withdrawAmount.String(),
		Currency:        "USD",
		AmountUsd:       withdrawAmount.String(),
		Status:          string(transaction.Success),
	})
	if err != nil {
		return "", fmt.Errorf("create transaction error: %w", err)
	}

	card, err := qtx.GetVirtualCardByBridgeCardID(ctx, success.Data.CardID)
	if err != nil {
		return "", fmt.Errorf("get card by cardholder id error: %w", err)
	}

	_, err = qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
		CardID:                  card.ID,
		UserID:                  user.ID,
		BridgecardTransactionID: success.Data.TransactionReference,
		Amount:                  withdrawAmount.IntPart(),
		Currency:                success.Data.Currency,
		TransactionType:         success.Data.CardTransactionType,
		Fee:                     0,
		Status:                  string(transaction.Success),
		Mode:                    success.Data.Livemode,
		TransactionTimestamp:    success.Data.TransactionTimestamp,
		TransactionID:           txx.ID,
	})
	if err != nil {
		return "", fmt.Errorf("create card transaction error: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: notification
	return "card_withdraw.success", nil
}

func (s *Service) handleCardUnloadEventFailed(ctx context.Context, failed *bridgecards.CardWithDrawEventFailed) (string, error) {
	return "card_withdraw.failed", nil
}

func (s *Service) processCardCredit(ctx context.Context, payload []byte) (string, error) {
	credit, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card credit: %w", err)
	}

	switch c := credit.(type) {
	case *bridgecards.CardCreditSuccess:
		return s.handleCardCreditSuccess(ctx, c)

	case *bridgecards.CardCreditFailed:
		return s.handleCardCreditFailed(ctx, c)

	default:
		return "", fmt.Errorf("unknown credit type")
	}
}

func (s *Service) processCardUnloadEvent(ctx context.Context, payload []byte) (string, error) {
	unloadEvent, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card unload event: %w", err)
	}

	switch u := unloadEvent.(type) {
	case *bridgecards.CardWithDrawEventSuccessful:
		return s.handleCardUnloadEventSuccess(ctx, u)

	case *bridgecards.CardWithDrawEventFailed:
		return s.handleCardUnloadEventFailed(ctx, u)

	default:
		return "", fmt.Errorf("unknown unload event type")
	}
}

// processCardholderVerification handles cardholder verification webhooks
func (s *Service) processCardholderVerification(ctx context.Context, payload []byte) (string, error) {
	verification, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse cardholder verification: %w", err)
	}

	switch v := verification.(type) {
	case *bridgecards.CardholderVerificationSuccess:
		return s.handleCardholderVerificationSuccess(ctx, v)

	case *bridgecards.CardholderVerificationFailed:
		return s.handleCardholderVerificationFailed(ctx, v)

	default:
		return "", fmt.Errorf("unknown verification type")
	}
}

func (s *Service) processCardDebitEvent(ctx context.Context, payload []byte) (string, error) {
	debitEvent, err := s.bridgeCard.ParseCardholderVerification(payload)
	if err != nil {
		return "", fmt.Errorf("parse card debit event: %w", err)
	}

	switch d := debitEvent.(type) {
	case *bridgecards.CardDebitEventSuccessful:
		return s.handleCardDebitEventSuccess(ctx, d)

	case *bridgecards.CardDebitEventDeclined:
		return s.handleCardDebitEventFailed(ctx, d)

	default:
		return "", fmt.Errorf("unknown debit event type")
	}
}

// ProcessWebhook processes incoming webhook events from BridgeCard
func (s *Service) ProcessWebhook(ctx context.Context, payload []byte) (string, error) {
	// Parse the webhook event type
	var event struct {
		Event string `json:"event"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return "", fmt.Errorf("parse webhook event: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Processing webhook event: %s", event.Event))

	// Handle different event types
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
		s.logger.Warn(fmt.Sprintf("Unhandled webhook event type: %s", event.Event))
		return event.Event, nil // Return event type but don't error for unknown events
	}
}
