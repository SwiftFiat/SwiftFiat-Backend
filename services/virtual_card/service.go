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
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/shopspring/decimal"
)

// VirtualCardService handles all virtual card business logic
type Service struct {
	store         *db.Store
	bridgeCard    *bridgecards.BridgeCardProvider
	walletService *wallet.WalletService
	logger        *logging.Logger
}

func NewService(
	store *db.Store,
	logger *logging.Logger,
	bridgeCard *bridgecards.BridgeCardProvider,
	walletService *wallet.WalletService,
) *Service {
	return &Service{
		store:         store,
		logger:        logger,
		bridgeCard:    bridgeCard,
		walletService: walletService,
	}
}

func (s *Service) CreateCardHolder(ctx context.Context, userID int32) (*bridgecards.CreateCardHolderResponse, error) {
	// get user
	user, err := s.store.GetUserByID(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to get user")
	}
	if !user.IsKycVerified {
		return nil, fmt.Errorf("user kyc not done")
	}

	_, err = s.store.GetKYCByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user kyc data")
	}
	params := &bridgecards.CreateCardHolderRequest{
		FirstName: "kyc.FirstName",
		LastName:  "kyc.Lastname",
		Email:     "test@email.com",
		Phone:     "+2348118",
		Address: bridgecards.Address{
			Address:     "man",
			City:        "Bende",
			State:       "Abia",
			PostalCode:  "0044221",
			Country:     "Nigeria",
			HouseNumber: "67",
		},
		Identity: bridgecards.Identity{
			IDType:      "NIGERIAN_BVN_VERIFICATION",
			BVN:         "22222222222",
			SelfieImage: "https://www.catster.com/wp-content/uploads/2024/08/tabby-cats-on-the-road_Ivanova-Ksenia_Shutterstock.jpg.webp",
		},
		Metadata: map[string]any{
			"user_id": userID,
		},
	}
	response, err := s.bridgeCard.CreateCardHolder(ctx, params)
	if err != nil {
		return nil, err
	}

	r := bridgecards.CreateCardHolderResponse{
		Status:  response.Status,
		Message: response.Message,
		Data:    response.Data,
	}
	return &r, nil
}

// CreateCard creates a new virtual card with BridgeCard and handles all setup
func (s *Service) CreateCard(ctx context.Context, params *bridgecards.CreateCardRequest) (*CreateCardResult, error) {
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
		return nil, fmt.Errorf("get card plan: %w", err)
	}

	if !plan.IsActive || plan.DeletedAt.Valid {
		return nil, ErrInvalidCardPlan
	}

	// 2. Check if user has reached plan card limit
	userCardsCount, err := qtx.GetUserActiveCardsCount(ctx, params.UserID)
	if err != nil {
		return nil, fmt.Errorf("count user cards: %w", err)
	}

	if userCardsCount >= int64(plan.MaxCardsPerUser) {
		return nil, ErrPlanLimitExceeded
	}

	// 3. Calculate total initial cost (creation fee + initial funding)
	initalFundingAmount, err := utils.ToDecimal(params.FundingAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to convert InitialFundingAmount to decimal")
	}

	creationFee, err := utils.ToDecimal(plan.CreationFee)
	if err != nil {
		return nil, fmt.Errorf("failed to convert CreationFee to decimal")
	}

	totalCost := creationFee.Add(initalFundingAmount)

	// 4. Verify user has sufficient wallet balance
	sourceWallet, err := s.walletService.GetWalletForUpdate(ctx, dbTx, params.SourceWalletID)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}

	if sourceWallet.Balance.LessThan(totalCost) {
		return nil, ErrInsufficientFunds
	}

	// 5. Create card in BridgeCard
	bridgeCardReq := &bridgecards.CreateCardRequest{
		CardHolderID:         "",
		CardType:             "virtual",
		Brand:                params.Brand,
		Currency:             "USD",
		CardLimit:            plan.CardLimit.String,
		FundingAmount:        params.FundingAmount,
		TransactionReference: utils.NewTxRef("Swiift_card"),
	}

	bridgeCardDetails, err := s.bridgeCard.CreateCard(ctx, bridgeCardReq)
	if err != nil {
		return nil, fmt.Errorf("create card in bridgecard: %w", err)
	}

	// 6. Create card record in our database
	now := time.Now()
	nextBillingDate := now.AddDate(0, 1, 0) // One month from now

	dbCard, err := qtx.CreateVirtualCard(ctx, db.CreateVirtualCardParams{
		UserID:           params.UserID,
		CardPlanID:       params.CardPlanID,
		BridgecardCardID: bridgeCardDetails.Data.CardID,
		CardName:         params.CardName,
		CardColor:        sql.NullString{String: params.CardColor, Valid: params.CardColor != ""},
		Currency:         "USD",
		Status:           "active",
		NextBillingDate:  sql.NullTime{Time: nextBillingDate, Valid: true},
		SpendingMonth:    sql.NullString{String: now.Format("2006-01"), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("create card record: %w", err)
	}

	// 7. Deduct funds from wallet for creation fee
	if creationFee.GreaterThan(decimal.Zero) {
		newBalance := sourceWallet.Balance.Sub(creationFee)
		_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
			Amount: sql.NullString{String: newBalance.String(), Valid: true},
			ID:     params.SourceWalletID,
		})
		if err != nil {
			return nil, fmt.Errorf("deduct creation fee error: %w", err)
		}

		// Log creation fee billing
		_, err = qtx.CreateCardBilling(ctx, db.CreateCardBillingParams{
			CardID:             dbCard.ID,
			UserID:             params.UserID,
			CardPlanID:         params.CardPlanID,
			BillingType:        "card_creation_fee",
			Amount:             plan.CreationFee,
			Currency:           "USD",
			BillingPeriodStart: now,
			BillingPeriodEnd:   now,
			SourceWalletID:     params.SourceWalletID,
			Status:             "completed",
		})
		if err != nil {
			s.logger.Error(fmt.Sprintf("failed to log creation fee: %v", err))
		}
	}

	// 8. Deduct funds from wallet for initial funding
	newBalance := sourceWallet.Balance.Sub(initalFundingAmount)
	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		Amount: sql.NullString{String: newBalance.String(), Valid: true},
		ID:     params.SourceWalletID,
	})
	if err != nil {
		return nil, fmt.Errorf("deduct initial funding error: %w", err)
	}

	// 9. Log funding record
	fundingRecord, err := qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID:         dbCard.ID,
		UserID:         params.UserID,
		SourceWalletID: params.SourceWalletID,
		Amount:         initalFundingAmount.String(),
		Currency:       "USD",
		SourceCurrency: sourceWallet.Currency,
		FundingType:    "manual",
		InitiatedBy:    "user",
		Status:         "completed",
	})
	if err != nil {
		return nil, fmt.Errorf("create funding record: %w", err)
	}

	// create generic tx record
	_, err = qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:      sql.NullInt64{Int64: params.UserID, Valid: true},
		Type:        "card_creation",
		Description: sql.NullString{String: "Card creation fee", Valid: true},
		Status:      "completed",
	})
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: Send notification to user about card creation

	s.logger.Info(fmt.Sprintf("Created virtual card %s for user %d", dbCard.ID, params.UserID))

	return &CreateCardResult{
		Card:              &dbCard,
		FundingRecord:     &fundingRecord,
		BridgeCardDetails: bridgeCardDetails,
	}, nil

}

// VerifyWebhookSignature verifies BridgeCard webhook signatures
func (s *Service) VerifyWebhookSignature(payload []byte, signature string) (bool, error) {
	return s.bridgeCard.VerifyWebhookSignature(payload, signature)
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

	// case strings.HasPrefix(event.Event, "card.transaction."):
	// 	return s.processTransactionWebhook(ctx, payload)

	// case strings.HasPrefix(event.Event, "card."):
	// return s.processCardWebhook(ctx, payload)

	default:
		s.logger.Warn(fmt.Sprintf("Unhandled webhook event type: %s", event.Event))
		return event.Event, nil // Return event type but don't error for unknown events
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

// handleCardholderVerificationSuccess handles successful cardholder verification
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

	// 4) Persist mapping cardholder_id -> user_id
	if setErr := s.store.SetBridgeCardCardholderID(ctx, db.SetBridgeCardCardholderIDParams{
		BridgecardCardholderID: sql.NullString{String: success.CardholderID, Valid: true},
		UpdatedAt:              time.Now(),
		ID:                     foundUID,
	}); setErr != nil {
		s.logger.Error(fmt.Sprintf("Failed to persist cardholder mapping: %v", setErr))
	}

	s.logger.Info(fmt.Sprintf("Found user %d for cardholder %s",foundUID, success.CardholderID))

	// Start transaction
	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	// Update cardholder verification status
	err = qtx.UpdateCardholderVerificationStatus(ctx, db.UpdateCardholderVerificationStatusParams{
		BridgecardCardholderID:       sql.NullString{String: success.CardholderID, Valid: true},
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
	// TODO: Update user's KYC status if needed

	s.logger.Info(fmt.Sprintf("Cardholder %s (user_id: %d) successfully verified",
		success.CardholderID, foundUID))

	return "cardholder_verification.successful", nil
}

// handleCardholderVerificationFailed handles failed cardholder verification
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
		BridgecardCardholderID:       sql.NullString{String: failed.CardholderID, Valid: true},
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

// processCardWebhook handles card-related webhooks (status changes, etc.)
// func (s *Service) processCardWebhook(ctx context.Context, payload []byte) (string, error) {
// 	// Parse card webhook
// 	var webhook struct {
// 		Event string `json:"event"`
// 		Data  struct {
// 			Card bridgecards.Card `json:"card"`
// 		} `json:"data"`
// 	}

// 	if err := json.Unmarshal(payload, &webhook); err != nil {
// 		return "", fmt.Errorf("parse card webhook: %w", err)
// 	}

// 	// Start transaction
// 	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
// 	if err != nil {
// 		return "", fmt.Errorf("begin transaction: %w", err)
// 	}
// 	defer dbTx.Rollback()

// 	qtx := s.store.WithTx(dbTx)

// 	// Get card by bridgecard ID
// 	card, err := qtx.GetVirtualCardByBridgecardID(ctx, webhook.Data.Card.ID)
// 	if err != nil {
// 		return "", fmt.Errorf("get card by bridgecard ID: %w", err)
// 	}

// 	// Update card status
// 	_, err = qtx.UpdateVirtualCardStatus(ctx, db.UpdateVirtualCardStatusParams{
// 		ID:     card.ID,
// 		Status: webhook.Data.Card.Status,
// 	})
// 	if err != nil {
// 		return "", fmt.Errorf("update card status: %w", err)
// 	}

// 	// TODO: Send notification to user about card status change

// 	s.logger.Info(fmt.Sprintf("Updated card %s status to %s", card.ID, webhook.Data.Card.Status))

// 	if err := dbTx.Commit(); err != nil {
// 		return "", fmt.Errorf("commit transaction: %w", err)
// 	}

// 	return webhook.Event, nil
// }
