package smartconversion

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	exchangerate "github.com/SwiftFiat/SwiftFiat-Backend/services/exchange_rate"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	ratemanager "github.com/SwiftFiat/SwiftFiat-Backend/services/rate_manager"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/streaks"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ConversionService struct {
	store               *db.Store
	logger              *logging.Logger
	rateManagerService  *ratemanager.Service
	exchangeRateService *exchangerate.ExchangeRateService
	transactionService  *transaction.TransactionService
	streakScheduler     *streaks.StreakScheduler
	notifyr             *service.Notification
	push                *service.PushNotificationService
}

func NewConversionService(
	store *db.Store,
	logger *logging.Logger,
	rateManagerService *ratemanager.Service,
	exchangeRateService *exchangerate.ExchangeRateService,
	transactionService *transaction.TransactionService,
	streakScheduler *streaks.StreakScheduler,
	notifyr *service.Notification,
	push *service.PushNotificationService,
) *ConversionService {
	return &ConversionService{
		store:               store,
		logger:              logger,
		rateManagerService:  rateManagerService,
		exchangeRateService: exchangeRateService,
		transactionService:  transactionService,
		streakScheduler:     streakScheduler,
		notifyr:             notifyr,
		push:                push,
	}
}

// CreateConversionRule creates a new conversion rule
func (s *ConversionService) CreateConversionRule(ctx context.Context, userID uuid.UUID, req *CreateConversionRuleRequest) (*ConversionRule, error) {
	s.logger.Info(fmt.Sprintf("Creating conversion rule for user %d", userID))

	// Validate currency pair
	if err := s.exchangeRateService.ValidateCurrencyPair(req.SourceCurrency, req.TargetCurrency); err != nil {
		return nil, exchangerate.ErrInvalidCurrencyPair
	}

	// Check if active rule already exists for this currency pair
	existingRule, err := s.store.GetActiveRuleByCurrencyPair(ctx, db.GetActiveRuleByCurrencyPairParams{
		UserID:         userID,
		SourceCurrency: req.SourceCurrency,
		TargetCurrency: req.TargetCurrency,
	})
	if err == nil && existingRule.ID != uuid.Nil {
		return nil, ErrDuplicateRule
	}

	// Validate wallets belong to user
	sourceWallet, err := s.store.GetWallet(ctx, req.SourceWalletID)
	if err != nil || sourceWallet.CustomerID != userID {
		return nil, fmt.Errorf("invalid source wallet")
	}

	targetWallet, err := s.store.GetWallet(ctx, req.TargetWalletID)
	if err != nil || targetWallet.CustomerID != userID {
		return nil, fmt.Errorf("invalid target wallet")
	}

	// Calculate next execution time for scheduled rules
	var nextExecutionAt *time.Time
	if req.TriggerType == "scheduled" {
		nextExec := s.calculateNextExecution(req.ScheduleFrequency, req.ScheduleDayOfWeek, req.ScheduleDayOfMonth, req.ScheduleTime, req.Timezone)
		nextExecutionAt = &nextExec
	}

	// Create the rule
	params := db.CreateConversionRuleParams{
		UserID:             userID,
		SourceCurrency:     req.SourceCurrency,
		TargetCurrency:     req.TargetCurrency,
		SourceWalletID:     uuid.NullUUID{UUID: req.SourceWalletID, Valid: true},
		TargetWalletID:     uuid.NullUUID{UUID: req.TargetWalletID, Valid: true},
		TriggerType:        req.TriggerType,
		TriggerRate:        s.decimalToNullString(req.TriggerRate),
		TriggerCondition:   s.stringToNullString(req.TriggerCondition),
		ConversionType:     req.ConversionType,
		FixedAmount:        s.decimalToNullString(req.FixedAmount),
		Percentage:         s.decimalToNullString(req.Percentage),
		ScheduleFrequency:  s.stringToNullString(req.ScheduleFrequency),
		ScheduleDayOfWeek:  s.intToNullInt32(req.ScheduleDayOfWeek),
		ScheduleDayOfMonth: s.intToNullInt32(req.ScheduleDayOfMonth),
		ScheduleTime:       s.parseScheduleTime(req.ScheduleTime),
		NextExecutionAt:    s.timeToNullTime(nextExecutionAt),
		Timezone:           s.stringToNullString(req.Timezone),
		Description:        s.stringToNullString(req.Description),
		Label:              s.stringToNullString(req.Label),
	}

	rule, err := s.store.CreateConversionRule(ctx, params)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create conversion rule: %v", err))
		return nil, fmt.Errorf("failed to create conversion rule: %w", err)
	}

	return s.dbRuleToModel(&rule), nil
}

// PauseConversionRule pauses a conversion rule
func (s *ConversionService) PauseConversionRule(ctx context.Context, ruleID uuid.UUID, userID uuid.UUID) error {
	rule, err := s.store.GetConversionRule(ctx, ruleID)
	if err != nil {
		return ErrRuleNotFound
	}

	if rule.UserID != userID {
		return fmt.Errorf("unauthorized")
	}

	_, err = s.store.UpdateRuleStatus(ctx, db.UpdateRuleStatusParams{
		ID:       ruleID,
		Status:   "paused",
		IsActive: false,
	})

	return err
}

// ResumeConversionRule resumes a paused conversion rule
func (s *ConversionService) ResumeConversionRule(ctx context.Context, ruleID uuid.UUID, userID uuid.UUID) error {
	rule, err := s.store.GetConversionRule(ctx, ruleID)
	if err != nil {
		return ErrRuleNotFound
	}

	if rule.UserID != userID {
		return fmt.Errorf("unauthorized")
	}

	_, err = s.store.UpdateRuleStatus(ctx, db.UpdateRuleStatusParams{
		ID:       ruleID,
		Status:   "active",
		IsActive: true,
	})

	return err
}

// DeleteConversionRule soft deletes a conversion rule
func (s *ConversionService) DeleteConversionRule(ctx context.Context, ruleID uuid.UUID, userID uuid.UUID) error {
	_, err := s.store.DeleteConversionRule(ctx, db.DeleteConversionRuleParams{
		ID:     ruleID,
		UserID: userID,
	})

	if err == sql.ErrNoRows {
		return ErrRuleNotFound
	}

	return err
}

// ============================================================
// CONVERSION EXECUTION
// ============================================================

// ExecuteManualConversion executes a manual conversion with vip rate
func (s *ConversionService) ExecuteManualConversion(ctx context.Context, req *ManualConversionRequest, user *db.User) (*ManualConversionResponse, error) {
	s.logger.Info(fmt.Sprintf("Executing manual conversion for user %d", user.ID))

	_, err := s.store.GetTransactionByIdempotencyKey(ctx, req.Reference)
	if err == nil {
		return nil, fmt.Errorf("tx exists")
	}

	// Validate wallets
	sourceWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   req.SourceCurrency,
	})
	if err != nil {
		return nil, ErrWalletNotFound
	}

	targetWallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   req.TargetCurrency,
	})
	if err != nil {
		return nil, ErrWalletNotFound
	}

	if sourceWallet.CustomerID != user.ID || targetWallet.CustomerID != user.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	// Validate currency pair
	if err := s.exchangeRateService.ValidateCurrencyPair(req.SourceCurrency, req.TargetCurrency); err != nil {
		return nil, err
	}

	s.logger.Infof("converting Amount %s from %s to %s", req.Amount, req.SourceCurrency, req.TargetCurrency)

	amount, err := utils.ToDecimal(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to decimal: %w", err)
	}

	kyc, err := s.store.Queries.GetKYCByUserID(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier_1" {
		go s.push.SendPushNotification(ctx, user.ID, "Verification required.", "This feature requires Tier 2 verification. Complete identity verification to continue")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_2")
	}

	// Get vip adjusted rate
	rate, err := s.rateManagerService.GetAdjustedRateForUser(ctx, user.ID, req.SourceCurrency, req.TargetCurrency, req.Amount)
	if err != nil {
		s.logger.Warnf("%s", fmt.Sprintf("Failed to get VIP-adjusted rate: %v", err))
		// fallback to base rate if vip rate fails
		s.logger.Warnf("%s", fmt.Sprintf("Falling back to base rate for user %d", user.ID))
		return s.executeWithBaseRate(ctx, user.ID, req, targetWallet.ID, sourceWallet.ID)
	}

	if rate.AdjustedRate != "" {
		s.logger.Infof("VIP adjusted rate for %s to %s is %s", req.SourceCurrency, req.TargetCurrency, rate.AdjustedRate)
	}

	// Calculate amounts
	fees, err := utils.ToDecimal(rate.Fees)
	if err != nil {
		return nil, fmt.Errorf("failed to convert fees to decimal: %w", err)
	}

	adjustedRate, err := utils.ToDecimal(rate.AdjustedRate)
	if err != nil {
		return nil, fmt.Errorf("failed to convert adjusted rate to decimal: %w", err)
	}
	s.logger.Infof("adjusted rate for %s to %s is %s", req.SourceCurrency, req.TargetCurrency, adjustedRate)

	var sourceAmount, targetAmount, netAmount decimal.Decimal

	sourceAmount = amount
	targetAmount, fees, netAmount = s.exchangeRateService.CalculateConversionAmount(sourceAmount, adjustedRate, fees)

	s.logger.Infof("source amount is %s", sourceAmount)
	s.logger.Infof("target amount is %s", targetAmount)
	s.logger.Infof("fees is %s", rate.Fees)
	s.logger.Infof("net amount is %s", netAmount)

	// Execute the conversion in a transaction
	history, err := s.executeConversion(ctx, &conversionExecutionParams{
		userID:         user.ID,
		ruleID:         nil,
		sourceWalletID: sourceWallet.ID,
		targetWalletID: targetWallet.ID,
		sourceCurrency: req.SourceCurrency,
		targetCurrency: req.TargetCurrency,
		sourceAmount:   sourceAmount,
		targetAmount:   targetAmount,
		fees:           fees,
		netAmount:      netAmount,
		executedRate:   adjustedRate,
		triggerRate:    nil,
		executionType:  "manual",
		triggerType:    nil,
		rateProvider:   rate.RateProvider,
	})
	if err != nil {
		return nil, err
	}

	return history, nil
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

// executeWithBaseRate is a fallback when VIP rate calculation fails
func (s *ConversionService) executeWithBaseRate(
	ctx context.Context,
	userID uuid.UUID,
	req *ManualConversionRequest,
	targetWalletID uuid.UUID,
	sourceWalletID uuid.UUID,
) (*ManualConversionResponse, error) {
	s.logger.Info("Executing conversion with base rate (VIP adjustment unavailable)")

	// Get base rate
	baseRate, err := s.exchangeRateService.GetExchangeRate(ctx, req.SourceCurrency, req.TargetCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to get base rate 3: %w", err)
	}

	s.logger.Infof("Base rate for %s to %s is %s", req.SourceCurrency, req.TargetCurrency, baseRate.Rate)

	// Calculate amounts
	feePercentage := s.exchangeRateService.GetFeePercentage(req.SourceCurrency, req.TargetCurrency)
	s.logger.Infof("Fee percentage for %s to %s is %s", req.SourceCurrency, req.TargetCurrency, feePercentage)

	amount, err := utils.ToDecimal(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to decimal: %w", err)
	}

	var sourceAmount, targetAmount, fees, netAmount decimal.Decimal
	sourceAmount = amount
	targetAmount, fees, netAmount = s.exchangeRateService.CalculateConversionAmount(
		sourceAmount, baseRate.Rate, feePercentage,
	)

	s.logger.Infof("source amount is %s", sourceAmount)
	s.logger.Infof("target amount is %s", targetAmount)
	s.logger.Infof("fees is %s", fees)
	s.logger.Infof("net amount is %s", netAmount)

	// Continue with regular conversion logic...
	// (Similar to ExecuteManualConversionWithVIPRate but without VIP adjustment)
	history, err := s.executeConversion(ctx, &conversionExecutionParams{
		userID:         userID,
		ruleID:         nil,
		sourceWalletID: sourceWalletID,
		targetWalletID: targetWalletID,
		sourceCurrency: req.SourceCurrency,
		targetCurrency: req.TargetCurrency,
		sourceAmount:   sourceAmount,
		targetAmount:   targetAmount,
		fees:           fees,
		netAmount:      netAmount,
		executedRate:   baseRate.Rate,
		triggerRate:    nil,
		executionType:  "manual",
		triggerType:    nil,
		rateProvider:   baseRate.Provider,
	})
	if err != nil {
		return nil, err
	}

	return history, nil
}

type conversionExecutionParams struct {
	userID         uuid.UUID
	ruleID         *uuid.UUID
	sourceWalletID uuid.UUID
	targetWalletID uuid.UUID
	sourceCurrency string
	targetCurrency string
	sourceAmount   decimal.Decimal
	targetAmount   decimal.Decimal
	fees           decimal.Decimal
	netAmount      decimal.Decimal
	executedRate   decimal.Decimal
	triggerRate    *decimal.Decimal
	executionType  string
	triggerType    *string
	rateProvider   string
}

// executeConversion performs the actual conversion in a database transaction
func (s *ConversionService) executeConversion(ctx context.Context, params *conversionExecutionParams) (*ManualConversionResponse, error) {

	sourceWallet, err := s.store.GetWalletForUpdate(ctx, params.sourceWalletID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source wallet: %w", err)
	}

	targetWallet, err := s.store.GetWalletForUpdate(ctx, params.targetWalletID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target wallet: %w", err)
	}

	sourceBalance, _ := decimal.NewFromString(sourceWallet.Balance.String)
	if params.sourceAmount.GreaterThan(sourceBalance) {
		return nil, ErrInsufficientBalance
	}

	// calculate new balances
	newSourceBalance := sourceBalance.Sub(params.sourceAmount)
	targetBalance, _ := decimal.NewFromString(targetWallet.Balance.String)
	newTargetBalance := targetBalance.Add(params.netAmount)

	dbTx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Commit at the end before returning
	defer func() {
		if err == nil {
			err = dbTx.Commit()
		}
	}()

	qtx := s.store.WithTx(dbTx)

	amountUsd, err := utils.ConvertToUSD(ctx, params.sourceAmount, params.sourceCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	mainTx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          params.userID,
		Type:            string(transaction.Swap),
		Description:     sql.NullString{String: "Conversion from " + params.sourceCurrency + " to " + params.targetCurrency, Valid: true},
		TransactionFlow: string(transaction.InPlatform),
		Amount:          params.sourceAmount.String(),
		Currency:        params.sourceCurrency,
		AmountUsd:       amountUsd.String(),
		Status:          string(transaction.Pending),
		IdempotencyKey:  utils.WatRequestID(),
		TFrom:           params.sourceCurrency,
		TTo:             params.targetCurrency,
		Direction:       "conversion",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Create conversion history
	history, err := qtx.CreateConversionHistory(ctx, db.CreateConversionHistoryParams{
		ConversionRuleID:    s.uuidToNullUUID(params.ruleID),
		UserID:              params.userID,
		SourceCurrency:      params.sourceCurrency,
		TargetCurrency:      params.targetCurrency,
		SourceWalletID:      uuid.NullUUID{UUID: params.sourceWalletID, Valid: true},
		TargetWalletID:      uuid.NullUUID{UUID: params.targetWalletID, Valid: true},
		TriggerRate:         s.decimalToNullString(params.triggerRate),
		ExecutedRate:        params.executedRate.String(),
		RateProvider:        sql.NullString{String: params.rateProvider, Valid: true},
		SourceAmount:        params.sourceAmount.String(),
		TargetAmount:        params.targetAmount.String(),
		Fees:                sql.NullString{String: params.fees.String(), Valid: true},
		NetAmount:           params.netAmount.String(),
		SourceBalanceBefore: sql.NullString{String: sourceBalance.String(), Valid: true},
		SourceBalanceAfter:  sql.NullString{String: newSourceBalance.String(), Valid: true},
		TargetBalanceBefore: sql.NullString{String: targetBalance.String(), Valid: true},
		TargetBalanceAfter:  sql.NullString{String: newTargetBalance.String(), Valid: true},
		ExecutionType:       params.executionType,
		TriggerType:         s.stringToNullString(params.triggerType),
		Status:              string(transaction.Pending),
		TransactionID:       uuid.NullUUID{UUID: mainTx.ID, Valid: true},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create conversion history: %w", err)
	}

	// update source wallet
	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		Amount: sql.NullString{String: newSourceBalance.String(), Valid: true},
		ID:     params.sourceWalletID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update source wallet: %w", err)
	}

	// update target wallet
	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID:     params.targetWalletID,
		Amount: sql.NullString{String: newTargetBalance.String(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update target wallet: %w", err)
	}

	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     mainTx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update tx record: %v", err)
	}

	_, err = qtx.UpdateConversionHistoryStatus(ctx, db.UpdateConversionHistoryStatusParams{
		ID:            history.ID,
		Status:        "success",
		FailureReason: sql.NullString{String: "", Valid: false},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update conversion history: %v", err)
	}

	// Update user streak
	if err := s.streakScheduler.UpdateStreakOnTransaction(ctx, params.userID, history.ID, "conversion"); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to update user streak: %v", err))
		// Don't fail the conversion for this
	}

	// Increment user's conversion volume for VIP tracking
	if err := s.rateManagerService.IncrementUserConversionVolume(ctx, params.userID, params.targetAmount); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to increment conversion volume for user %d: %v", params.userID, err))
		// Don't fail the conversion for this
	}

	user, userErr := s.store.GetUserByID(ctx, params.userID)
	if userErr != nil {
		s.logger.Error("failed to get user")
	}

	if userErr == nil && !user.HasCompletedFirstConversion.Bool {
		referrerID, referralBonus, refErr := transaction.CheckFirstConersionAndDisburseReferralBonus(ctx, s.store, dbTx, params.userID, mainTx.ID)
		if refErr != nil {
			s.logger.Errorf("Failed to disburse referral bonus: %v", refErr)
		}
		if referrerID != nil && referralBonus != nil {
			go func() {
				bgCtx := context.Background()
				s.notifyr.CreateWithRecipients(bgCtx, nil, "Referral Bonus Credit",
					fmt.Sprintf("You have received a referral bonus of %s", referralBonus.String()),
					"system", []uuid.UUID{*referrerID})
				s.push.ReferralBonusEarned(bgCtx, *referrerID, referralBonus.String())
			}()
		}
	}

	referrerID, amountEarned, creditErr := transaction.CreditReferrerForConversion(ctx, s.store, dbTx, params.userID, params.targetAmount)
	if creditErr != nil {
		s.logger.Errorf("failed to credit referrer for conversion: %v", creditErr)
	} else {
		go func() {
			bgCtx := context.Background()
			s.notifyr.CreateWithRecipients(bgCtx, nil, "Referral conversion Bonus Earned",
				fmt.Sprintf("You have earned %s from a referral conversion", amountEarned.String()),
				"system", []uuid.UUID{*referrerID})
			s.push.SendPushNotification(bgCtx, *referrerID, "Referral conversion Bonus Earned",
				fmt.Sprintf("You have earned %s from a referral conversion", amountEarned.String()))
		}()
	}

	// send notification
	go func() {
		bgCtx := context.Background()
		s.notifyr.CreateWithRecipients(bgCtx, nil, "Conversion Completed",
			fmt.Sprintf("You have converted %s %s to %s %s", params.sourceAmount.String(), params.sourceCurrency, params.targetAmount.String(), params.targetCurrency),
			"system", []uuid.UUID{params.userID})
		s.push.SendPushNotification(bgCtx, params.userID, "Conversion Completed",
			fmt.Sprintf("You have converted %s %s to %s %s", params.sourceAmount.String(), params.sourceCurrency, params.targetAmount.String(), params.targetCurrency))
	}()

	return &ManualConversionResponse{
		SourceAmount: params.sourceAmount.InexactFloat64(),
		TargetAmount: params.targetAmount.InexactFloat64(),
		ExecutedRate: params.executedRate.InexactFloat64(),
		Reference:    mainTx.IdempotencyKey,
		Fees:         params.fees.InexactFloat64(),
		NetAmount:    params.netAmount.InexactFloat64(),
		Status:       string(transaction.Success),
	}, nil
}

// CheckAndExecuteRateBasedRules checks rate-based rules and executes if triggered
func (s *ConversionService) CheckAndExecuteRateBasedRules(ctx context.Context) error {
	s.logger.Info("Checking rate-based conversion rules")

	// Get all active rate-based rules
	rules, err := s.store.Queries.GetActiveRateBasedRules(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to fetch rate-based rules: %v", err))
		return fmt.Errorf("failed to fetch rate-based rules: %w", err)
	}
	for _, rule := range rules {
		// Get current exchange rate
		rate, err := s.exchangeRateService.GetExchangeRate(ctx, rule.SourceCurrency, rule.TargetCurrency)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to get exchange rate for rule %s: %v", rule.ID, err))
			continue
		}

		// Check if trigger condition is met
		triggerRate := s.nullStringToDecimal(rule.TriggerRate)
		if triggerRate == nil {
			s.logger.Warn(fmt.Sprintf("Rule %s has no trigger rate set", rule.ID))
			continue
		}

		condition := s.stringOrDefault(&rule.TriggerCondition.String, "gte")
		isTriggered := false

		if condition == "gte" && rate.Rate.GreaterThanOrEqual(*triggerRate) {
			isTriggered = true
		} else if condition == "lte" && rate.Rate.LessThanOrEqual(*triggerRate) {
			isTriggered = true
		} else if condition == "eq" && rate.Rate.Equal(*triggerRate) {
			isTriggered = true
		}

		if !isTriggered {
			s.logger.Debug(fmt.Sprintf("Rule %s not triggered. Current rate: %s, Trigger: %s (%s)",
				rule.ID, rate.Rate.String(), triggerRate.String(), condition))
			continue
		}

		s.logger.Info(fmt.Sprintf("Rule %s triggered! Executing conversion", rule.ID))

		// Execute the conversion
		err = s.executeRuleConversion(ctx, &rule)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to execute rule %s: %v", rule.ID, err))

			// Update failure
			s.store.UpdateRuleFailure(ctx, db.UpdateRuleFailureParams{
				ID:                rule.ID,
				LastFailureReason: sql.NullString{String: err.Error(), Valid: true},
			})
			continue
		}

		// Update successful execution
		s.store.UpdateRuleExecution(ctx, db.UpdateRuleExecutionParams{
			ID:              rule.ID,
			LastTriggerRate: sql.NullString{String: rate.Rate.String(), Valid: true},
		})
	}

	return nil
}

// ExecuteScheduledConversions executes scheduled conversions that are due
func (s *ConversionService) ExecuteScheduledConversions(ctx context.Context) error {
	s.logger.Info("Executing scheduled smart conversions")

	rules, err := s.store.Queries.GetScheduledRulesDue(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch scheduled rules: %w", err)
	}

	for _, rule := range rules {
		s.logger.Info(fmt.Sprintf("Executing scheduled rule %s", rule.ID))

		err := s.executeRuleConversion(ctx, &rule)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to execute rule %s: %v", rule.ID, err))

			// Update failure
			s.store.UpdateRuleFailure(ctx, db.UpdateRuleFailureParams{
				ID:                rule.ID,
				LastFailureReason: sql.NullString{String: err.Error(), Valid: true},
			})

			continue
		}

		// Calculate next execution time
		nextExec := s.calculateNextExecutionForRule(&rule)

		// Update rule execution
		s.store.UpdateRuleExecution(ctx, db.UpdateRuleExecutionParams{
			ID:              rule.ID,
			LastTriggerRate: sql.NullString{Valid: false},
			NextExecutionAt: sql.NullTime{Time: nextExec, Valid: true},
		})
	}

	return nil
}

// executeRuleConversion executes a conversion based on a rule
func (s *ConversionService) executeRuleConversion(ctx context.Context, rule *db.ConversionRule) error {
	rate, err := s.exchangeRateService.GetExchangeRate(ctx, rule.SourceCurrency, rule.TargetCurrency)
	if err != nil {
		return fmt.Errorf("failed to get exchange rate: %w", err)
	}

	// Get source wallet to check balance
	sourceWallet, err := s.store.GetWallet(ctx, rule.SourceWalletID.UUID)
	if err != nil {
		return fmt.Errorf("failed to get source wallet: %w", err)
	}

	sourceBalance, _ := decimal.NewFromString(sourceWallet.Balance.String)

	// Calculate source amount based on conversion type
	var sourceAmount decimal.Decimal
	switch rule.ConversionType {
	case "fixed_amount":
		sourceAmount, _ = decimal.NewFromString(rule.FixedAmount.String)
	case "percentage":
		percentage, _ := decimal.NewFromString(rule.Percentage.String)
		sourceAmount = sourceBalance.Mul(percentage).Div(decimal.NewFromInt(100))
	case "full_balance":
		sourceAmount = sourceBalance
	}

	// Check if sufficient balance
	if sourceAmount.GreaterThan(sourceBalance) {
		return ErrInsufficientBalance
	}

	// Calculate target amounts
	feePercentage := s.exchangeRateService.GetFeePercentage(rule.SourceCurrency, rule.TargetCurrency)
	targetAmount, fees, netAmount := s.exchangeRateService.CalculateConversionAmount(sourceAmount, rate.Rate, feePercentage)

	// Execute the conversion
	triggerType := rule.TriggerType
	_, err = s.executeConversion(ctx, &conversionExecutionParams{
		userID:         rule.UserID,
		ruleID:         &rule.ID,
		sourceWalletID: rule.SourceWalletID.UUID,
		targetWalletID: rule.TargetWalletID.UUID,
		sourceCurrency: rule.SourceCurrency,
		targetCurrency: rule.TargetCurrency,
		sourceAmount:   sourceAmount,
		targetAmount:   targetAmount,
		fees:           fees,
		netAmount:      netAmount,
		executedRate:   rate.Rate,
		triggerRate:    s.nullStringToDecimal(rule.TriggerRate),
		executionType:  "automatic",
		triggerType:    &triggerType,
		rateProvider:   rate.Provider,
	})
	if err != nil {
		return err
	}
	return nil
}

// ============================================================
// CONVERSION HISTORY
// ============================================================

// GetConversionHistory retrieves conversion history for a user
func (s *ConversionService) GetConversionHistory(ctx context.Context, userID uuid.UUID, limit, offset int32) ([]*ConversionHistoryResponse, error) {
	histories, err := s.store.GetConversionHistoryByUser(ctx, db.GetConversionHistoryByUserParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}

	var responses []*ConversionHistoryResponse

	for _, h := range histories {
		// Get rule label if exists
		var ruleLabel *string
		if h.ConversionRuleID.Valid {
			rule, err := s.store.GetConversionRule(ctx, h.ConversionRuleID.UUID)
			if err == nil {
				ruleLabel = s.nullStringToString(rule.Label)
			}
		}

		responses = append(responses, &ConversionHistoryResponse{
			ID:             h.ID,
			RuleLabel:      ruleLabel,
			SourceCurrency: h.SourceCurrency,
			TargetCurrency: h.TargetCurrency,
			SourceAmount:   s.stringToDecimal(h.SourceAmount),
			TargetAmount:   s.stringToDecimal(h.TargetAmount),
			TriggerRate:    s.nullStringToDecimal(h.TriggerRate),
			ExecutedRate:   s.stringToDecimal(h.ExecutedRate),
			Fees:           s.stringToDecimal(h.Fees.String),
			NetAmount:      s.stringToDecimal(h.NetAmount),
			ExecutionType:  h.ExecutionType,
			Status:         h.Status,
			ExecutedAt:     h.ExecutedAt,
		})

	}
	return responses, nil
}

// GetConversionStats retrieves conversion statistics for a user
func (s *ConversionService) GetConversionStats(ctx context.Context, userID uuid.UUID, since time.Time) (*ConversionStats, error) {
	stats, err := s.store.GetConversionHistoryStats(ctx, db.GetConversionHistoryStatsParams{
		UserID:     userID,
		ExecutedAt: since,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to fetch stats: %w", err)
	}

	return &ConversionStats{
		TotalConversions:      int(stats.TotalConversions),
		SuccessfulConversions: int(stats.SuccessfulConversions),
		FailedConversions:     int(stats.FailedConversions),
		TotalConverted:        s.stringToDecimal(stats.TotalConverted),
		TotalFees:             s.stringToDecimal(stats.TotalFees),
	}, nil
}

// Helper conversion functions
func (s *ConversionService) dbRuleToModel(rule *db.ConversionRule) *ConversionRule {
	return &ConversionRule{
		ID:                 rule.ID,
		UserID:             rule.UserID,
		SourceCurrency:     rule.SourceCurrency,
		TargetCurrency:     rule.TargetCurrency,
		SourceWalletID:     rule.SourceWalletID.UUID,
		TargetWalletID:     rule.TargetWalletID.UUID,
		TriggerType:        rule.TriggerType,
		TriggerRate:        s.nullStringToDecimal(rule.TriggerRate),
		TriggerCondition:   s.nullStringToString(rule.TriggerCondition),
		ConversionType:     rule.ConversionType,
		FixedAmount:        s.nullStringToDecimal(rule.FixedAmount),
		Percentage:         s.nullStringToDecimal(rule.Percentage),
		ScheduleFrequency:  s.nullStringToString(rule.ScheduleFrequency),
		ScheduleDayOfWeek:  &rule.ScheduleDayOfWeek.Int32,
		ScheduleDayOfMonth: &rule.ScheduleDayOfMonth.Int32,
		ScheduleTime:       s.nullTimeToTime(rule.ScheduleTime),
		NextExecutionAt:    s.nullTimeToTime(rule.NextExecutionAt),
		Timezone:           rule.Timezone.String,
		Description:        s.nullStringToString(rule.Description),
		Label:              s.nullStringToString(rule.Label),
		Status:             rule.Status,
		IsActive:           rule.IsActive,
		CreatedAt:          rule.CreatedAt,
		UpdatedAt:          rule.UpdatedAt,
	}
}

// ...existing code...
func (s *ConversionService) calculateNextExecution(frequency *string, dayOfWeek, dayOfMonth *int, scheduleTime *string, timezone *string) time.Time {
	// Determine location
	loc := time.UTC
	if timezone != nil && *timezone != "" {
		if l, err := time.LoadLocation(*timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)

	// parse scheduled time (HH:MM) if provided
	hour, min := 0, 0
	if scheduleTime != nil && *scheduleTime != "" {
		if t, err := time.ParseInLocation("15:04", *scheduleTime, loc); err == nil {
			hour, min = t.Hour(), t.Minute()
		}
	}

	// helper to create a time at given date with scheduled hour/min
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, hour, min, 0, 0, loc)
	}

	// default to next day same time if no frequency
	if frequency == nil || *frequency == "" {
		cand := at(now.Year(), now.Month(), now.Day()).Add(24 * time.Hour)
		if cand.After(now) {
			return cand
		}
		return cand.Add(24 * time.Hour)
	}

	switch *frequency {
	case "daily":
		today := at(now.Year(), now.Month(), now.Day())
		if today.After(now) {
			return today
		}
		return today.Add(24 * time.Hour)

	case "weekly":
		// Expect dayOfWeek: 0 = Sunday ... 6 = Saturday (fallback to next day)
		var targetWeekday time.Weekday
		if dayOfWeek != nil {
			targetWeekday = time.Weekday(*dayOfWeek % 7)
		} else {
			// default to same weekday next week
			targetWeekday = now.Weekday()
		}
		daysUntil := (int(targetWeekday) - int(now.Weekday()) + 7) % 7
		candidate := at(now.Year(), now.Month(), now.Day()).AddDate(0, 0, daysUntil)
		// if same day but time already passed, schedule for next week
		if daysUntil == 0 && !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		if candidate.After(now) {
			return candidate
		}
		return candidate.AddDate(0, 0, 7)

	case "monthly":
		// Expect dayOfMonth: 1..31
		var dom int
		if dayOfMonth != nil && *dayOfMonth > 0 {
			dom = *dayOfMonth
		} else {
			dom = now.Day()
		}

		// try this month
		maxDay := daysInMonth(now.Year(), now.Month())
		day := dom
		if day > maxDay {
			day = maxDay
		}
		candidate := at(now.Year(), now.Month(), day)
		if candidate.After(now) {
			return candidate
		}
		// next month
		nextMonth := now.AddDate(0, 1, 0)
		maxDayNext := daysInMonth(nextMonth.Year(), nextMonth.Month())
		day = dom
		if day > maxDayNext {
			day = maxDayNext
		}
		return at(nextMonth.Year(), nextMonth.Month(), day)

	default:
		// custom or unknown -> fallback to next day at scheduled time
		cand := at(now.Year(), now.Month(), now.Day()).Add(24 * time.Hour)
		if cand.After(now) {
			return cand
		}
		return cand.Add(24 * time.Hour)
	}
}

func (s *ConversionService) calculateNextExecutionForRule(rule *db.ConversionRule) time.Time {
	// build parameters from db rule
	var freqPtr *string
	if rule.ScheduleFrequency.Valid && rule.ScheduleFrequency.String != "" {
		f := rule.ScheduleFrequency.String
		freqPtr = &f
	}

	var dowPtr *int
	if rule.ScheduleDayOfWeek.Valid {
		d := int(rule.ScheduleDayOfWeek.Int32)
		dowPtr = &d
	}

	var domPtr *int
	if rule.ScheduleDayOfMonth.Valid {
		d := int(rule.ScheduleDayOfMonth.Int32)
		domPtr = &d
	}

	var scheduleTimePtr *string
	if rule.ScheduleTime.Valid {
		t := rule.ScheduleTime.Time.Format("15:04")
		scheduleTimePtr = &t
	}

	var tzPtr *string
	if rule.Timezone.Valid && rule.Timezone.String != "" {
		tz := rule.Timezone.String
		tzPtr = &tz
	}

	return s.calculateNextExecution(freqPtr, dowPtr, domPtr, scheduleTimePtr, tzPtr)
}

func daysInMonth(year int, month time.Month) int {
	// day 0 of next month is last day of current month
	t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	return t.Day()
}

func (s *ConversionService) parseScheduleTime(timeStr *string) sql.NullTime {
	if timeStr == nil {
		return sql.NullTime{Valid: false}
	}
	// Parse HH:MM format
	return sql.NullTime{Valid: false}
}

// Conversion helper functions
func (s *ConversionService) decimalToNullString(d *decimal.Decimal) sql.NullString {
	if d == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: d.String(), Valid: true}
}

func (s *ConversionService) stringToNullString(str *string) sql.NullString {
	if str == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *str, Valid: true}
}

func (s *ConversionService) intToNullInt32(i *int) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

func (s *ConversionService) timeToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func (s *ConversionService) stringOrDefault(str *string, defaultVal string) string {
	if str == nil {
		return defaultVal
	}
	return *str
}

func (s *ConversionService) nullStringToDecimal(ns sql.NullString) *decimal.Decimal {
	if !ns.Valid {
		return nil
	}
	d, _ := decimal.NewFromString(ns.String)
	return &d
}

func (s *ConversionService) nullStringToString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func (s *ConversionService) nullTimeToTime(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}

func (s *ConversionService) stringToDecimal(st string) decimal.Decimal {
	d, _ := decimal.NewFromString(st)
	return d
}

func (s *ConversionService) uuidToNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{Valid: false}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}
