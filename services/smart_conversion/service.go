package smartconversion

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ConversionService struct {
	store               *db.Store
	logger              *logging.Logger
	exchangeRateService *ExchangeRateService
	transactionService  *transaction.TransactionService
}

func NewConversionService(
	store *db.Store,
	logger *logging.Logger,
	exchangeRateService *ExchangeRateService,
	transactionService *transaction.TransactionService,
) *ConversionService {
	return &ConversionService{
		store:               store,
		logger:              logger,
		exchangeRateService: exchangeRateService,
		transactionService:  transactionService,
	}
}

// CreateConversionRule creates a new conversion rule
func (s *ConversionService) CreateConversionRule(ctx context.Context, userID int64, req *CreateConversionRuleRequest) (*ConversionRule, error) {
	s.logger.Info(fmt.Sprintf("Creating conversion rule for user %d", userID))

	// Validate currency pair
	if err := s.exchangeRateService.ValidateCurrencyPair(req.SourceCurrency, req.TargetCurrency); err != nil {
		return nil, ErrInvalidCurrencyPair
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
func (s *ConversionService) PauseConversionRule(ctx context.Context, ruleID uuid.UUID, userID int64) error {
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
func (s *ConversionService) ResumeConversionRule(ctx context.Context, ruleID uuid.UUID, userID int64) error {
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
func (s *ConversionService) DeleteConversionRule(ctx context.Context, ruleID uuid.UUID, userID int64) error {
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

// ExecuteManualConversion executes a manual conversion
func (s *ConversionService) ExecuteManualConversion(ctx context.Context, userID int64, req *ManualConversionRequest) (*ManualConversionResponse, error) {
	s.logger.Info(fmt.Sprintf("Executing manual conversion for user %d", userID))

	// Validate currency pair
	if err := s.exchangeRateService.ValidateCurrencyPair(req.SourceCurrency, req.TargetCurrency); err != nil {
		return nil, ErrInvalidCurrencyPair
	}

	// Get current exchange rate
	rate, err := s.exchangeRateService.GetExchangeRate(ctx, req.SourceCurrency, req.TargetCurrency)
	if err != nil {
		return nil, ErrRateNotAvailable
	}

	// Calculate amounts
	feePercentage := s.exchangeRateService.GetFeePercentage(req.SourceCurrency, req.TargetCurrency)

	var sourceAmount, targetAmount, fees, netAmount decimal.Decimal

	if req.AmountType == "source" {
		sourceAmount = req.Amount
		targetAmount, fees, netAmount = s.exchangeRateService.CalculateConversionAmount(sourceAmount, rate.Rate, feePercentage)
	} else {
		targetAmount = req.Amount
		sourceAmount, fees, netAmount = s.exchangeRateService.CalculateInverseAmount(targetAmount, rate.Rate, feePercentage)
	}

	// Execute the conversion in a transaction
	conversionID, transactionID, err := s.executeConversion(ctx, &conversionExecutionParams{
		userID:         userID,
		ruleID:         nil,
		sourceWalletID: req.SourceWalletID,
		targetWalletID: req.TargetWalletID,
		sourceCurrency: req.SourceCurrency,
		targetCurrency: req.TargetCurrency,
		sourceAmount:   sourceAmount,
		targetAmount:   targetAmount,
		fees:           fees,
		netAmount:      netAmount,
		executedRate:   rate.Rate,
		triggerRate:    nil,
		executionType:  "manual",
		triggerType:    nil,
		rateProvider:   rate.Provider,
	})
	if err != nil {
		return nil, err
	}

	return &ManualConversionResponse{
		ConversionID:  *conversionID,
		TransactionID: *transactionID,
		SourceAmount:  sourceAmount,
		TargetAmount:  targetAmount,
		ExecutedRate:  rate.Rate,
		Fees:          fees,
		NetAmount:     netAmount,
		Status:        "success",
	}, nil
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

type conversionExecutionParams struct {
	userID         int64
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
func (s *ConversionService) executeConversion(ctx context.Context, params *conversionExecutionParams) (*uuid.UUID, *uuid.UUID, error) {
	var conversionID, transactionID *uuid.UUID
	// var historyErr error

	err := s.store.ExecTx(ctx, func(q *db.Queries) error {
		// get wallet for update
		sourceWallet, err := q.GetWalletForUpdate(ctx, params.sourceWalletID)
		if err != nil {
			return fmt.Errorf("failed to get source wallet: %w", err)
		}

		targetWallet, err := q.GetWalletForUpdate(ctx, params.targetWalletID)
		if err != nil {
			return fmt.Errorf("failed to get target wallet: %w", err)
		}

		sourceBalance, _ := decimal.NewFromString(sourceWallet.Balance.String)
		if params.sourceAmount.GreaterThan(sourceBalance) {
			return ErrInsufficientBalance
		}

		// calculate new balances
		newSourceBalance := sourceBalance.Sub(params.sourceAmount)
		targetBalance, _ := decimal.NewFromString(targetWallet.Balance.String)
		newTargetBalance := targetBalance.Add(params.netAmount)

		// update source wallet
		_, err = q.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
			Amount: sql.NullString{String: newSourceBalance.String(), Valid: true},
			ID:     params.sourceWalletID,
		})
		if err != nil {
			return fmt.Errorf("failed to update source wallet: %w", err)
		}

		// update target wallet
		_, err = q.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
			ID:     params.targetWalletID,
			Amount: sql.NullString{String: newTargetBalance.String(), Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to update target wallet: %w", err)
		}

		// Create main transaction record (swap type)
		txnID := uuid.New()
		transactionID = &txnID

		// TODO: Create transaction using your transaction service
		// This would involve calling s.transactionService.CreateSwapTransaction(...)

		// Create conversion history
		history, err := q.CreateConversionHistory(ctx, db.CreateConversionHistoryParams{
			ConversionRuleID:    s.uuidToNullUUID(params.ruleID),
			UserID:              params.userID,
			TransactionID:       s.uuidToNullUUID(transactionID),
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
			Status:              "success",
		})

		if err != nil {
			// historyErr = err
			return fmt.Errorf("failed to create history: %w", err)
		}

		conversionID = &history.ID
		return nil

	})

	if err != nil {
		s.logger.Error(fmt.Sprintf("Conversion failed: %v", err))
		return nil, nil, ErrConversionFailed
	}

	return conversionID, transactionID, nil
}

// Helper conversion functions
func (s *ConversionService) dbRuleToModel(rule *db.ConversionRule) *ConversionRule {
	// Convert db.ConversionRule to ConversionRule model
	// Implementation details...
	return &ConversionRule{}
}

func (s *ConversionService) calculateNextExecution(frequency *string, dayOfWeek, dayOfMonth *int, scheduleTime *string, timezone *string) time.Time {
	// Calculate next execution time based on frequency
	now := time.Now()
	// Implementation details...
	return now.Add(24 * time.Hour)
}

func (s *ConversionService) calculateNextExecutionForRule(rule *db.ConversionRule) time.Time {
	// Calculate next execution based on rule settings
	return time.Now().Add(24 * time.Hour)
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
