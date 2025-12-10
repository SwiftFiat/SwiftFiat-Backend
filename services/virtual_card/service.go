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

func (s *Service) CreateCardHolder(ctx context.Context, userID int32, req *bridgecards.CreateCardHolderRequest) (*bridgecards.CreateCardHolderResponse, error) {
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
	// params := &bridgecards.CreateCardHolderRequest{
	// FirstName: "kyc.FirstName",
	// LastName:  "kyc.Lastname",
	// Email:     "test@email.com",
	// Phone:     "+2348118",
	// Address: bridgecards.Address{
	// 	Address:     "man",
	// 	City:        "Bende",
	// 	State:       "Abia",
	// 	PostalCode:  "0044221",
	// 	Country:     "Nigeria",
	// 	HouseNumber: "67",
	// },
	// Identity: bridgecards.Identity{
	// 	IDType:      "NIGERIAN_BVN_VERIFICATION",
	// 	BVN:         "22222222222",
	// 	SelfieImage: "https://www.catster.com/wp-content/uploads/2024/08/tabby-cats-on-the-road_Ivanova-Ksenia_Shutterstock.jpg.webp",
	// },
	// Metadata: map[string]any{
	// 		"user_id": userID,
	// 	},
	// }
	req.Metadata = map[string]any{
		"user_id": userID,
	}
	response, err := s.bridgeCard.CreateCardHolder(ctx, req)
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
func (s *Service) CreateCard(ctx context.Context, params *bridgecards.CreateCardRequest) (*bridgecards.CreateCardResponse, error) {
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

	// 2. Check if user has reached plan card limit
	userCardsCount, err := qtx.GetUserActiveCardsCount(ctx, params.UserID)
	if err != nil {
		return nil, fmt.Errorf("count user cards error: %w", err)
	}

	s.logger.Infof("active cards for this user: %d", userCardsCount)

	s.logger.Infof("max card for this plan: %d", plan.MaxCardsPerUser)

	if userCardsCount >= int64(plan.MaxCardsPerUser) {
		return nil, ErrPlanLimitExceeded
	}

	creationFeeCents, err := utils.ToDecimal(plan.CreationFee)
	if err != nil {
		return nil, fmt.Errorf("failed to convert CreationFee to decimal")
	}
	creationFee := creationFeeCents.Div(decimal.NewFromInt(100))

	// 4. Verify user has sufficient wallet balance
	sourceWallet, err := s.walletService.GetWalletForUpdate(ctx, dbTx, params.SourceWalletID)
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

	// enforce minimum depending on card limit
	var minFundingCents decimal.Decimal
	switch plan.CardLimit.String {
	case "5000":
		minFundingCents = decimal.NewFromInt(300) // $3 → 300 cents
	case "10000":
		minFundingCents = decimal.NewFromInt(400) // $4 → 400 cents
	}

	fundingCentsDecimal := decimal.RequireFromString(fundingCentsStr)
	if fundingCentsDecimal.LessThan(minFundingCents) {
		return nil, fmt.Errorf("funding amount must be at least %s cents", minFundingCents.String())
	}

	totalCost := creationFee.Add(fundingAmountDecimal)

	s.logger.Infof("total cost is $%s", totalCost.String())

	if sourceWallet.Balance.LessThan(totalCost) {
		return nil, ErrInsufficientFunds
	}

	// 5. Create card in BridgeCard
	bridgeCardReq := &bridgecards.CreateCardRequest{
		CardHolderID:         "220cf84a33954f81a325f2d5108d8fed", // get from user data
		CardType:             "virtual",
		Brand:                "Mastercard", //Visa is not supported yet
		Currency:             "USD",
		CardLimit:            "500000",        // (can either be $5,000 i.e "500000" or $10,000 i.e "1000000")
		FundingAmount:        fundingCentsStr, // (a minimum of $3 i.e "300" for cards with a spending limit of $5,000 and $4 i.e "400" for a card with a spending limit of $10,000)
		TransactionReference: utils.NewTxRef("swiift_card"),
	}

	s.logger.Infof("bridgeCardReq is : %v", bridgeCardReq)

	bridgeCardDetails, err := s.bridgeCard.CreateCard(ctx, bridgeCardReq)
	if err != nil {
		return nil, fmt.Errorf("create card in bridgecard err: %v", err)
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
		return nil, fmt.Errorf("create card record in db error: %w", err)
	}

	// 7. Deduct funds from wallet for creation fee
	totalDeduction := creationFee.Add(fundingAmountDecimal)
	s.logger.Infof("total dudection is %s$", totalDeduction.String())
	newBalance := sourceWallet.Balance.Sub(totalDeduction)
	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		Amount: sql.NullString{String: newBalance.String(), Valid: true},
		ID:     params.SourceWalletID,
	})
	if err != nil {
		return nil, fmt.Errorf("deduct funds from wallet error: %w", err)
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
		Status:             "successful",
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("failed to log creation fee: %v", err))
	}

	// 9. Log funding record
	_, err = qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
		CardID:         dbCard.ID,
		UserID:         params.UserID,
		SourceWalletID: params.SourceWalletID,
		Amount:         fundingAmountDecimal.String(),
		Currency:       "USD",
		SourceCurrency: sourceWallet.Currency,
		FundingType:    "manual",
		InitiatedBy:    "user",
		Status:         "successful",
	})
	if err != nil {
		return nil, fmt.Errorf("create funding record: %w", err)
	}

	// create generic tx record
	_, err = qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:      sql.NullInt64{Int64: params.UserID, Valid: true},
		Type:        "card",
		Description: sql.NullString{String: "Card creation fee", Valid: true},
		Status:      "successful",
	})
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: Send notification to user about card creation

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

	// case strings.HasPrefix(event.Event, "card.transaction."):
	// 	return s.processTransactionWebhook(ctx, payload)

	// case strings.HasPrefix(event.Event, "card."):
	// return s.processCardWebhook(ctx, payload)

	default:
		s.logger.Warn(fmt.Sprintf("Unhandled webhook event type: %s", event.Event))
		return event.Event, nil // Return event type but don't error for unknown events
	}
}

func (s *Service) FundCard(ctx context.Context, req bridgecards.FundCardRequest, userID int64) (*bridgecards.FundCardResponse, error) {
	// get user usd wallet
	wallet, err := s.store.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
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
		Status:         "pending",
	})
	if err != nil {
		return nil, fmt.Errorf("create card funding record error: %w", err)
	}

	// Debit user and refund if card funding fails
	newBalance := walletBalance.Sub(fundingAmount)

	_, err = s.store.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: wallet.ID,
		Amount: sql.NullString{
			String: newBalance.String(),
			Valid:  true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("update wallet balance error: %w", err)
	}

	bridgeResponse, err := s.bridgeCard.FundCard(ctx, req)
	if err != nil {
		// Update funding status to failed if BridgeCard call fails
		_, updateErr := s.store.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
			ID:            fundingRecord.ID,
			Status:        "failed",
			FailureReason: sql.NullString{String: err.Error(), Valid: true},
		})
		if updateErr != nil {
			s.logger.Error(fmt.Sprintf("failed to update funding status: %v", updateErr))
		}
		return nil, fmt.Errorf("fund card via bridgecard error: %w", err)
	}

	return bridgeResponse, nil
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
		Status: "frozen",
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

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
		Status: "active",
	})
	if err != nil {
		return nil, fmt.Errorf("update virtual card status error: %w", err)
	}

	return cardDetails, nil
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

	s.logger.Info(fmt.Sprintf("Found user %d for cardholder %s", foundUID, success.CardholderID))

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
			Status:        "failed",
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

	// walletBalance, err := utils.ToDecimal(usdWallet.Balance.String)
	// if err != nil {
	// 	return "", fmt.Errorf("convert wallet balance to decimal error: %w", err)
	// }

	// amount, err := utils.ToDecimal(success.Amount)
	// if err != nil {
	// 	return "", fmt.Errorf("convert amount to decimal error: %w", err)
	// }

	// if walletBalance.LessThan(amount) {
	// 	return "", fmt.Errorf("insufficient balance")
	// }

	// newBalance := walletBalance.Sub(amount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	qtx := s.store.WithTx(dbTx)

	// _, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
	// 	ID: usdWallet.ID,
	// 	Amount: sql.NullString{
	// 		String: newBalance.String(),
	// 		Valid:  true,
	// 	},
	// })
	// if err != nil {
	// 	return "", fmt.Errorf("update wallet balance error: %w", err)
	// }

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
		Status:         "successful",
	})
	if err != nil {
		return "", fmt.Errorf("create card funding error: %w", err)
	}

	_, err = qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID: sql.NullInt64{
			Int64: user.ID,
			Valid: true,
		},
		Type:            "card",
		TransactionFlow: sql.NullString{String: "card_funding", Valid: true},
		Description:     sql.NullString{String: "Card credit", Valid: true},
		Status:          "successful",
	})
	if err != nil {
		return "", fmt.Errorf("create transaction error: %w", err)
	}

	_, err = qtx.UpdateCardFundingStatus(ctx, db.UpdateCardFundingStatusParams{
		ID:     card.ID,
		Status: "successful",
	})
	if err != nil {
		return "", fmt.Errorf("update card funding status error: %w", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	// TODO: notification
	return "card_credit.success", nil
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
