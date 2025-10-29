package vaultsavings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	activitylogs "github.com/SwiftFiat/SwiftFiat-Backend/services/activity_logs"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrVaultNotFound        = errors.New("vault not found")
	ErrInsufficientBalance  = errors.New("insufficient vault balance")
	ErrInvalidAmount        = errors.New("invalid amount")
	ErrInvalidCurrency      = errors.New("invalid currency")
	ErrWalletNotFound       = errors.New("wallet not found")
	ErrVaultAlreadyExists   = errors.New("vault already exists for this currency")
	ErrRecurringRuleInvalid = errors.New("invalid recurring rule configuration")
)

// ============================================================================
// RECURRING RULE MODELS
// ============================================================================

type Interval string

const (
	IntervalDaily   Interval = "daily"
	IntervalWeekly  Interval = "weekly"
	IntervalMonthly Interval = "monthly"
)

// RecurringRule represents the flexible JSONB recurring deposit configuration
type RecurringRule struct {
	Enabled           bool                   `json:"enabled"`
	Amount            string                 `json:"amount"`
	Interval          Interval               `json:"interval"`
	StartDate         time.Time              `json:"start_date"`
	EndDate           *time.Time             `json:"end_date,omitempty"`
	DayOfWeek         *time.Weekday          `json:"day_of_week,omitempty"`
	DayOfMonth        *int                   `json:"day_of_month,omitempty"`
	TimeOfDay         string                 `json:"time_of_day"`
	NextExecutionAt   time.Time              `json:"next_execution_at"`
	ExecutionCount    int                    `json:"execution_count"`
	MaxExecutions     *int                   `json:"max_executions,omitempty"`
	SkipWeekends      bool                   `json:"skip_weekends"`
	RetryOnFailure    bool                   `json:"retry_on_failure"`
	MaxRetries        int                    `json:"max_retries"`
	RetryCount        int                    `json:"retry_count"`
	NotifyOnSuccess   bool                   `json:"notify_on_success"`
	NotifyOnFailure   bool                   `json:"notify_on_failure"`
	PauseOnLowBalance bool                   `json:"pause_on_low_balance"`
	LastExecutionAt   *time.Time             `json:"last_execution_at,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// Scan implements sql.Scanner interface for RecurringRule
func (r *RecurringRule) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan RecurringRule: expected []byte, got %T", value)
	}

	return json.Unmarshal(bytes, r)
}

// Value implements driver.Valuer interface for RecurringRule
func (r RecurringRule) Value() (interface{}, error) {
	if r.Amount == "" {
		return nil, nil
	}
	return json.Marshal(r)
}

// IsActive checks if the recurring rule should be executed
func (r *RecurringRule) IsActive() bool {
	if !r.Enabled {
		return false
	}

	now := time.Now()

	// Check start date
	if now.Before(r.StartDate) {
		return false
	}

	// Check end date
	if r.EndDate != nil && now.After(*r.EndDate) {
		return false
	}

	// Check max executions
	if r.MaxExecutions != nil && r.ExecutionCount >= *r.MaxExecutions {
		return false
	}

	return true
}

// CalculateNextExecution calculates the next execution time based on interval
func (r *RecurringRule) CalculateNextExecution() time.Time {
	now := time.Now()
	var next time.Time

	switch r.Interval {
	case IntervalDaily:
		next = r.calculateNextDaily(now)
	case IntervalWeekly:
		next = r.calculateNextWeekly(now)
	case IntervalMonthly:
		next = r.calculateNextMonthly(now)
	default:
		next = now.Add(24 * time.Hour)
	}

	// Apply time of day
	next = r.applyTimeOfDay(next)

	// Skip weekends if configured
	if r.SkipWeekends {
		next = r.skipWeekends(next)
	}

	return next
}

func (r *RecurringRule) calculateNextDaily(from time.Time) time.Time {
	return from.Add(24 * time.Hour)
}

func (r *RecurringRule) calculateNextWeekly(from time.Time) time.Time {
	next := from.Add(7 * 24 * time.Hour)

	if r.DayOfWeek != nil {
		targetWeekday := *r.DayOfWeek
		currentWeekday := next.Weekday()

		daysUntilTarget := int(targetWeekday - currentWeekday)
		if daysUntilTarget < 0 {
			daysUntilTarget += 7
		}

		next = next.Add(time.Duration(daysUntilTarget) * 24 * time.Hour)
	}

	return next
}

func (r *RecurringRule) calculateNextMonthly(from time.Time) time.Time {
	year, month, _ := from.Date()

	// Move to next month
	month++
	if month > 12 {
		month = 1
		year++
	}

	// Determine day of month
	day := 1
	if r.DayOfMonth != nil {
		if *r.DayOfMonth == -1 {
			// Last day of month
			day = r.lastDayOfMonth(year, month)
		} else {
			day = *r.DayOfMonth
			// Ensure day is valid for the month
			lastDay := r.lastDayOfMonth(year, month)
			if day > lastDay {
				day = lastDay
			}
		}
	}

	return time.Date(year, month, day, 0, 0, 0, 0, from.Location())
}

func (r *RecurringRule) lastDayOfMonth(year int, month time.Month) int {
	// First day of next month minus one day
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func (r *RecurringRule) applyTimeOfDay(t time.Time) time.Time {
	if r.TimeOfDay == "" {
		return t
	}

	// Parse time (format: "HH:MM")
	var hour, minute int
	fmt.Sscanf(r.TimeOfDay, "%d:%d", &hour, &minute)

	return time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, t.Location())
}

func (r *RecurringRule) skipWeekends(t time.Time) time.Time {
	for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		t = t.Add(24 * time.Hour)
	}
	return t
}

// MarkExecuted marks the rule as executed and updates counters
func (r *RecurringRule) MarkExecuted() {
	now := time.Now()
	r.ExecutionCount++
	r.LastExecutionAt = &now
	r.NextExecutionAt = r.CalculateNextExecution()
	r.RetryCount = 0 // Reset retry count on success
}

// ============================================================================
// SERVICE INTERFACES
// ============================================================================

type VaultService struct {
	store              *db.Store
	logger             *logging.Logger
	walletService      *wallet.WalletService
	emailService       *service.Plunk
	pushService        *service.PushNotificationService
	activityLogService *activitylogs.ActivityLog
}

func NewVaultService(
	store *db.Store,
	logger *logging.Logger,
	walletService *wallet.WalletService,
	emailService *service.Plunk,
	pushService *service.PushNotificationService,
	activityLogService *activitylogs.ActivityLog,
) *VaultService {
	return &VaultService{
		store:              store,
		logger:             logger,
		walletService:      walletService,
		emailService:       emailService,
		pushService:        pushService,
		activityLogService: activityLogService,
	}
}


// Helper functions to create common recurring rules

func NewDailyRecurringRule(amount, timeOfDay string) *RecurringRule {
	now := time.Now()
	rule := &RecurringRule{
		Enabled:           true,
		Amount:            amount,
		Interval:          IntervalDaily,
		StartDate:         now,
		TimeOfDay:         timeOfDay,
		ExecutionCount:    0,
		SkipWeekends:      false,
		RetryOnFailure:    true,
		MaxRetries:        3,
		NotifyOnSuccess:   true,
		NotifyOnFailure:   true,
		PauseOnLowBalance: true,
	}
	rule.NextExecutionAt = rule.CalculateNextExecution()
	return rule
}

func NewWeeklyRecurringRule(amount string, dayOfWeek time.Weekday, timeOfDay string) *RecurringRule {
	now := time.Now()
	rule := &RecurringRule{
		Enabled:           true,
		Amount:            amount,
		Interval:          IntervalWeekly,
		StartDate:         now,
		DayOfWeek:         &dayOfWeek,
		TimeOfDay:         timeOfDay,
		ExecutionCount:    0,
		SkipWeekends:      false,
		RetryOnFailure:    true,
		MaxRetries:        3,
		NotifyOnSuccess:   true,
		NotifyOnFailure:   true,
		PauseOnLowBalance: true,
	}
	rule.NextExecutionAt = rule.CalculateNextExecution()
	return rule
}

func NewMonthlyRecurringRule(amount string, dayOfMonth int, timeOfDay string) *RecurringRule {
	now := time.Now()
	rule := &RecurringRule{
		Enabled:           true,
		Amount:            amount,
		Interval:          IntervalMonthly,
		StartDate:         now,
		DayOfMonth:        &dayOfMonth,
		TimeOfDay:         timeOfDay,
		ExecutionCount:    0,
		SkipWeekends:      false,
		RetryOnFailure:    true,
		MaxRetries:        3,
		NotifyOnSuccess:   true,
		NotifyOnFailure:   true,
		PauseOnLowBalance: true,
	}
	rule.NextExecutionAt = rule.CalculateNextExecution()
	return rule
}


// ============================================================================
// REQUEST/RESPONSE MODELS
// ============================================================================

type CreateVaultGoalRequest struct {
	UserID        int64          `json:"user_id"`
	Name          string         `json:"name" binding:"required"`
	Description   *string        `json:"description"`
	TargetAmount  string         `json:"target_amount"`
	Currency      string         `json:"currency" binding:"required"`
	VaultType     string         `json:"vault_type"`
	RecurringRule *RecurringRule `json:"recurring_rule,omitempty"`
}

type DepositRequest struct {
	UserID       int64     `json:"user_id"`
	VaultID      uuid.UUID `json:"vault_id"`
	FromWalletID uuid.UUID `json:"from_wallet_id" binding:"required"`
	Amount       string    `json:"amount" binding:"required"`
	Currency     string    `json:"currency"`
	Reference    string    `json:"reference"`
	Description  string    `json:"description"`
}

type WithdrawRequest struct {
	UserID      int64     `json:"user_id"`
	VaultID     uuid.UUID `json:"vault_id"`
	ToWalletID  uuid.UUID `json:"to_wallet_id" binding:"required"`
	Amount      string    `json:"amount" binding:"required"`
	Reference   string    `json:"reference"`
	Description string    `json:"description"`
}

type UpdateRecurringRuleRequest struct {
	Enabled       *bool         `json:"enabled,omitempty"`
	Amount        *string       `json:"amount,omitempty"`
	Interval      *Interval     `json:"interval,omitempty"`
	DayOfWeek     *time.Weekday `json:"day_of_week,omitempty"`
	DayOfMonth    *int          `json:"day_of_month,omitempty"`
	TimeOfDay     *string       `json:"time_of_day,omitempty"`
	SkipWeekends  *bool         `json:"skip_weekends,omitempty"`
	MaxExecutions *int          `json:"max_executions,omitempty"`
}

// ============================================================================
// SERVICE INTERFACES
// ============================================================================

type EmailService interface {
	SendGoalCreatedEmail(ctx context.Context, user *db.User, vaultName, currency, goalAmount string) error
	SendRecurringDepositSuccessEmail(ctx context.Context, user *db.User, vaultName, amount, currency string) error
	SendRecurringDepositFailedEmail(ctx context.Context, user *db.User, vaultName, reason string) error
	SendDepositSuccessEmail(ctx context.Context, user *db.User, vaultName, amount, currency string) error
	SendWithdrawalSuccessEmail(ctx context.Context, user *db.User, vaultName, amount, currency string) error
	SendWithdrawal2FARequiredEmail(ctx context.Context, user *db.User, transactionID string) error
	SendWithdrawalPendingApprovalEmail(ctx context.Context, user *db.User, transactionID string) error
	SendGoalCompletedEmail(ctx context.Context, user *db.User, vaultName, amount, currency string) error
}

type PushService interface {
	SendGoalCreatedPush(ctx context.Context, userID int64, vaultName string) error
	SendRecurringDepositSuccessPush(ctx context.Context, userID int64, vaultName, amount, currency string) error
	SendDepositSuccessPush(ctx context.Context, userID int64, vaultName, amount, currency string) error
	SendWithdrawalSuccessPush(ctx context.Context, userID int64, vaultName, amount, currency string) error
	SendGoalCompletedPush(ctx context.Context, userID int64, vaultName string) error
}

type ActivityLogService interface {
	LogActivity(ctx context.Context, userID int64, action string) error
}

// ============================================================================
// CREATE VAULT GOAL
// ============================================================================
func (s *VaultService) CreateVaultGoal(ctx context.Context, req CreateVaultGoalRequest, ip, ua string) (*db.VaultSaving, error) {
	// Validate request
	if err := s.validateCreateRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// If recurring rule is provided, validate and initialize it
	if req.RecurringRule != nil {
		if err := s.validateRecurringRule(req.RecurringRule); err != nil {
			return nil, fmt.Errorf("invalid recurring rule: %w", err)
		}

		// Calculate first execution time
		req.RecurringRule.NextExecutionAt = req.RecurringRule.CalculateNextExecution()
	}

	// Get user for notifications
	user, err := s.store.GetUserByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Set default vault type
	if req.VaultType == "" {
		req.VaultType = "flexible"
	}

	var recurring pqtype.NullRawMessage

	// Create vault goal
	params := db.CreateVaultGoalParams{
		UserID:            req.UserID,
		VaultName:         req.Name,
		Description:       nullString(stringOrEmpty(req.Description)),
		GoalAmount:        nullString(req.TargetAmount),
		CurrentBalance:    nullString("0.00"),
		Currency:          req.Currency,
		AutoSaveEnabled:   req.RecurringRule != nil && req.RecurringRule.Enabled,
		AutoSaveFrequency: nullString(string(req.RecurringRule.Interval)),
		AutoSaveAmount:    nullString(req.RecurringRule.Amount),
		NextAutoSave:      nullTime(req.RecurringRule.NextExecutionAt),
		RecurringRule:     recurring,
		Status:            "active",
		VaultType:         req.VaultType,
	}

	vault, err := s.store.CreateVaultGoal(ctx, params)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create vault goal: %v", err))
		return nil, fmt.Errorf("failed to create vault goal: %w", err)
	}

	action := fmt.Sprintf("Created vault savings goal: %s (%s %s)", req.Name, req.TargetAmount, req.Currency)
	auditParams := db.CreateAuditLogParams{
		UserID:    int32(user.ID),
		Action:    action,
		Ip:        nullString(ip),
		UserAgent: nullString(ua),
	}
	_ = s.activityLogService.Create(ctx, auditParams)

	// Send notifications (async, don't block on errors)
	go func() {
		bgCtx := context.Background()

		if s.emailService != nil {
			if err := s.emailService.SendGoalCreatedEmail(bgCtx, &user, req.Name, req.Currency, req.TargetAmount); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to send goal created email: %v", err))
			}
		}

		if s.pushService != nil {
			if err := s.pushService.SendVaultGoalCreatedPush(bgCtx, req.UserID, req.Name); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to send goal created push: %v", err))
			}
		}
	}()

	s.logger.Info(fmt.Sprintf("Successfully created vault goal %s for user %d", vault.ID, req.UserID))
	return &vault, nil
}

// ============================================================================
// GET VAULT GOALS
// ============================================================================

func (s *VaultService) GetVaultByID(ctx context.Context, vaultID uuid.UUID) (*db.VaultSaving, error) {
	vault, err := s.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVaultNotFound
		}
		return nil, err
	}
	return &vault, nil
}

func (s *VaultService) GetUserVaults(ctx context.Context, userID int64) ([]db.VaultSaving, error) {
	return s.store.GetVaultGoalsByUserID(ctx, userID)
}

func (s *VaultService) GetUserActiveVaults(ctx context.Context, userID int64) ([]db.VaultSaving, error) {
	return s.store.GetActiveVaultGoalsByUserID(ctx, userID)
}

func (s *VaultService) GetUserVaultSummary(ctx context.Context, userID int64) (*db.GetUserVaultsSummaryRow, error) {
	summary, err := s.store.GetUserVaultsSummary(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (s *VaultService) GetVaultProgress(ctx context.Context, vaultID uuid.UUID) (*db.GetVaultGoalProgressRow, error) {
	progress, err := s.store.GetVaultGoalProgress(ctx, vaultID)
	if err != nil {
		return nil, err
	}
	return &progress, nil
}

// ============================================================================
// DEPOSIT
// ============================================================================

func (s *VaultService) Deposit(ctx context.Context, req DepositRequest) (*db.VaultTransaction, error) {
	s.logger.Info(fmt.Sprintf("Processing deposit to vault %s: %s %s", req.VaultID, req.Amount, req.Currency))

	// Validate amount
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidAmount
	}

	// Start transaction
	tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.store.WithTx(tx)

	// Lock vault for update
	vault, err := qtx.LockVaultForUpdate(ctx, req.VaultID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVaultNotFound
		}
		return nil, fmt.Errorf("failed to lock vault: %w", err)
	}

	// Verify currency matches
	if vault.Currency != req.Currency {
		return nil, fmt.Errorf("currency mismatch: vault uses %s, provided %s", vault.Currency, req.Currency)
	}

	// Lock source wallet
	sourceWallet, err := qtx.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
		CustomerID: req.UserID,
		Currency:   req.Currency,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to lock source wallet: %w", err)
	}

	// Check wallet balance
	walletBalance, err := decimal.NewFromString(sourceWallet.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet balance: %w", err)
	}

	if walletBalance.LessThan(amount) {
		return nil, fmt.Errorf("insufficient wallet balance: have %s, need %s", walletBalance.String(), amount.String())
	}

	// Calculate new balances
	currentBalance, err := decimal.NewFromString(vault.CurrentBalance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid vault balance: %w", err)
	}

	newVaultBalance := currentBalance.Add(amount)
	newWalletBalance := walletBalance.Sub(amount)

	// Generate reference
	reference := req.Reference
	if reference == "" {
		reference = fmt.Sprintf("vault_deposit_%s_%d", uuid.New().String()[:8], time.Now().Unix())
	}

	// Create transaction record
	txParams := db.CreateVaultTransactionParams{
		UserID:                req.UserID,
		VaultID:               req.VaultID,
		TransactionType:       "deposit",
		Amount:                req.Amount,
		Currency:              req.Currency,
		SourceWallet:          uuid.NullUUID{UUID: req.FromWalletID, Valid: true},
		BalanceBefore:         vault.CurrentBalance.String,
		BalanceAfter:          newVaultBalance.String(),
		Reference:             sql.NullString{String: reference, Valid: true},
		Description:           sql.NullString{String: req.Description, Valid: req.Description != ""},
		Status:                nullString("completed"),
		Requires2fa:           sql.NullBool{Bool: false, Valid: true},
		RequiresAdminApproval: sql.NullBool{Bool: false, Valid: true},
	}

	transaction, err := qtx.CreateVaultTransaction(ctx, txParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Update vault balance
	if err := qtx.IncrementVaultBalance(ctx, db.IncrementVaultBalanceParams{
		ID:             req.VaultID,
		CurrentBalance: nullString(newVaultBalance.String()),
	}); err != nil {
		return nil, fmt.Errorf("failed to update vault balance: %w", err)
	}

	// Update wallet balance (deduct)
	_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID:     req.FromWalletID,
		Amount: sql.NullString{String: newWalletBalance.String(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update wallet balance: %w", err)
	}

	// Check if goal reached
	goalAmount, err := decimal.NewFromString(vault.GoalAmount.String)
	goalReached := false
	if err == nil && newVaultBalance.GreaterThanOrEqual(goalAmount) {
		goalReached = true
		if err := qtx.UpdateVaultStatus(ctx, db.UpdateVaultStatusParams{
			ID:     req.VaultID,
			Status: "completed",
		}); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to mark vault as completed: %v", err))
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log activity
	action := fmt.Sprintf("Deposited %s %s to vault: %s", req.Amount, req.Currency, vault.VaultName)
	auditParams := db.CreateAuditLogParams{
		UserID: int32(req.UserID),
		Action: action,
	}
	_ = s.activityLogService.Create(ctx, auditParams)

	// Get user for notifications
	user, err := s.store.GetUserByID(ctx, req.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get user for notifications: %v", err))
	} else {
		// Send notifications (async)
		go func() {
			bgCtx := context.Background()

			if goalReached {
				if s.emailService != nil {
					_ = s.emailService.SendGoalCompletedEmail(bgCtx, &user, vault.VaultName, goalAmount.String(), vault.Currency)
				}
				if s.pushService != nil {
					_ = s.pushService.SendGoalCompletedPush(bgCtx, req.UserID, vault.VaultName)
				}
			} else {
				if s.emailService != nil {
					_ = s.emailService.SendDepositSuccessEmail(bgCtx, &user, vault.VaultName, req.Amount, req.Currency)
				}
				if s.pushService != nil {
					_ = s.pushService.SendDepositSuccessPush(bgCtx, req.UserID, vault.VaultName, req.Amount, req.Currency)
				}
			}
		}()
	}
	s.logger.Info(fmt.Sprintf("Successfully processed deposit: %s", transaction.ID))
	return &transaction, nil
}

// ============================================================================
// WITHDRAWAL
// ============================================================================

func (s *VaultService) Withdraw(ctx context.Context, req WithdrawRequest) (*db.VaultTransaction, error) {
	s.logger.Info(fmt.Sprintf("Processing withdrawal from vault %s: %s", req.VaultID, req.Amount))

	// Validate amount
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidAmount
	}

	// Determine security requirements
	requires2FA := amount.GreaterThan(decimal.NewFromInt(1000))            // $1,000 threshold
	requiresAdminApproval := amount.GreaterThan(decimal.NewFromInt(10000)) // $10,000 threshold

	// Start transaction
	tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.store.WithTx(tx)

	// Lock vault
	vault, err := qtx.LockVaultForUpdate(ctx, req.VaultID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVaultNotFound
		}
		return nil, fmt.Errorf("failed to lock vault: %w", err)
	}

	// Check balance
	currentBalance, err := decimal.NewFromString(vault.CurrentBalance.String)
	if err != nil {
		return nil, fmt.Errorf("invalid vault balance: %w", err)
	}

	if currentBalance.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	newVaultBalance := currentBalance.Sub(amount)

	// Generate reference
	reference := req.Reference
	if reference == "" {
		reference = fmt.Sprintf("withdrawal_%s_%d", uuid.New().String()[:8], time.Now().Unix())
	}

	// Determine status
	status := "completed"
	if requires2FA || requiresAdminApproval {
		status = "pending"
	}

	// Create transaction record
	txParams := db.CreateVaultTransactionParams{
		UserID:                req.UserID,
		VaultID:               req.VaultID,
		TransactionType:       "withdrawal",
		Amount:                req.Amount,
		Currency:              vault.Currency,
		DestinationWallet:     uuid.NullUUID{UUID: req.ToWalletID, Valid: true},
		BalanceBefore:         vault.CurrentBalance.String,
		BalanceAfter:          newVaultBalance.String(),
		Reference:             sql.NullString{String: reference, Valid: true},
		Description:           sql.NullString{String: req.Description, Valid: req.Description != ""},
		Status:                nullString(status),
		Requires2fa:           nullBool(requires2FA),
		RequiresAdminApproval: nullBool(requiresAdminApproval),
	}

	transaction, err := qtx.CreateVaultTransaction(ctx, txParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Only update balances if not requiring approval
	if status == "completed" {
		// Update vault balance
		if err := qtx.DecrementVaultBalance(ctx, db.DecrementVaultBalanceParams{
			ID:             req.VaultID,
			CurrentBalance: nullString(req.Amount),
		}); err != nil {
			return nil, fmt.Errorf("failed to update vault balance: %w", err)
		}
		// Lock destination wallet
		destWallet, err := s.walletService.GetWalletForUpdate(ctx, tx, req.ToWalletID)
		if err != nil {
			return nil, fmt.Errorf("failed to lock destination wallet: %w", err)
		}

		// Update wallet balance (add)
		destBalance, _ := decimal.NewFromString(destWallet.Balance.String())
		newWalletBalance := destBalance.Add(amount)

		_, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
			ID:     req.ToWalletID,
			Amount: sql.NullString{String: newWalletBalance.String(), Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update wallet balance: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log activity
	action := fmt.Sprintf("Withdrew %s %s from vault: %s", req.Amount, vault.Currency, vault.VaultName)
	_ = s.activityLogService.Create(ctx, db.CreateAuditLogParams{
		UserID: int32(req.UserID),
		Action: action,
	})

	// Get user for notifications
	user, err := s.store.GetUserByID(ctx, req.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get user for notifications: %v", err))
	} else {
		// Send notifications (async)
		go func() {
			bgCtx := context.Background()

			if requires2FA && s.emailService != nil {
				_ = s.emailService.SendWithdrawal2FARequiredEmail(bgCtx, &user, transaction.ID.String())
			} else if requiresAdminApproval && s.emailService != nil {
				_ = s.emailService.SendWithdrawalPendingApprovalEmail(bgCtx, &user, transaction.ID.String())
			} else {
				if s.emailService != nil {
					_ = s.emailService.SendWithdrawalSuccessEmail(bgCtx, &user, vault.VaultName, req.Amount, vault.Currency)
				}
				if s.pushService != nil {
					_ = s.pushService.SendWithdrawalSuccessPush(bgCtx, req.UserID, vault.VaultName, req.Amount, vault.Currency)
				}
			}
		}()
	}

	s.logger.Info(fmt.Sprintf("Successfully processed withdrawal: %s", transaction.ID))
	return &transaction, nil
}

// ============================================================================
// PROCESS RECURRING DEPOSITS (SCHEDULER)
// ============================================================================

func (s *VaultService) ProcessRecurringDeposits(ctx context.Context) error {
	s.logger.Info("Starting recurring deposits processing")

	// Get all vaults with recurring rules that are due for execution
	vaults, err := s.store.GetVaultsWithRecurringRules(ctx, 100) // Process up to 100 at a time
	if err != nil {
		return fmt.Errorf("failed to fetch vaults with recurring rules: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Found %d vaults due for recurring deposit", len(vaults)))

	successCount := 0
	failureCount := 0

	for _, vault := range vaults {
		if err := s.processRecurringDeposit(ctx, vault); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to process recurring deposit for vault %s: %v", vault.ID, err))
			failureCount++
			continue
		}
		successCount++
	}

	s.logger.Info(fmt.Sprintf("Processed recurring deposits: %d successful, %d failed", successCount, failureCount))
	return nil
}

func (s *VaultService) processRecurringDeposit(ctx context.Context, vault db.VaultSaving) error {
	// Parse recurring rule
	if !vault.RecurringRule.Valid || len(vault.RecurringRule.RawMessage) == 0 {
		return errors.New("no recurring rule found")
	}

	var rule RecurringRule
	if err := json.Unmarshal(vault.RecurringRule.RawMessage, &rule); err != nil {
		return fmt.Errorf("invalid recurring rule json: %w", err)
	}

	// Check if rule is active
	if !rule.IsActive() {
		s.logger.Info(fmt.Sprintf("Recurring rule for vault %s is not active", vault.ID))
		return nil
	}

	// Get user's wallet for this currency
	wallet, err := s.store.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
		CustomerID: vault.UserID,
		Currency:   vault.Currency,
	})
	if err != nil {
		return fmt.Errorf("no wallet found for user %d with currency %s", vault.UserID, vault.Currency)
	}

	sourceWallet := wallet

	// Check if user has sufficient balance
	amount, err := decimal.NewFromString(rule.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount in recurring rule: %w", err)
	}

	walletBalance, err := decimal.NewFromString(sourceWallet.Balance.String)
	if err != nil {
		return fmt.Errorf("invalid wallet balance: %w", err)
	}

	// Check if pause on low balance is enabled
	if rule.PauseOnLowBalance && walletBalance.LessThan(amount) {
		s.logger.Info(fmt.Sprintf("Insufficient balance for recurring deposit (vault: %s, required: %s, available: %s)",
			vault.ID, rule.Amount, walletBalance.String()))

		// Send notification about insufficient balance
		if rule.NotifyOnFailure {
			user, _ := s.store.GetUserByID(ctx, vault.UserID)
			if s.emailService != nil {
				_ = s.emailService.SendRecurringDepositFailedEmail(ctx, &user, vault.VaultName, "Insufficient balance")
			}
		}
		return nil
	}

	// Process the deposit
	depositReq := DepositRequest{
		UserID:       vault.UserID,
		VaultID:      vault.ID,
		FromWalletID: sourceWallet.ID,
		Amount:       rule.Amount,
		Currency:     vault.Currency,
		Description:  "Recurring deposit",
	}

	_, err = s.Deposit(ctx, depositReq)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to process recurring deposit: %v", err))

		// Handle retry logic
		if rule.RetryOnFailure && rule.RetryCount < rule.MaxRetries {
			rule.RetryCount++
			b, _ := json.Marshal(rule)
			// Don't mark as executed, will retry next time
			return s.store.UpdateRecurringRule(ctx, db.UpdateRecurringRuleParams{
				ID:            vault.ID,
				RecurringRule: pqtype.NullRawMessage{RawMessage: b, Valid: true},
			})
		}

		return err
	}

	// Mark rule as executed
	rule.MarkExecuted()

	b, _ := json.Marshal(rule)
	if err := s.store.UpdateRecurringRule(ctx, db.UpdateRecurringRuleParams{
		ID:            vault.ID,
		RecurringRule: pqtype.NullRawMessage{RawMessage: b, Valid: true},
	}); err != nil {
		return fmt.Errorf("failed to update recurring rule: %w", err)
	}

	// Send success notification
	if rule.NotifyOnSuccess {
		user, _ := s.store.GetUserByID(ctx, vault.UserID)
		if s.emailService != nil {
			_ = s.emailService.SendRecurringDepositSuccessEmail(ctx, &user, vault.VaultName, rule.Amount, vault.Currency)
		}
		if s.pushService != nil {
			_ = s.pushService.SendRecurringDepositSuccessPush(ctx, vault.UserID, vault.VaultName, rule.Amount, vault.Currency)
		}
	}

	s.logger.Info(fmt.Sprintf("Successfully processed recurring deposit for vault %s: %s %s",
		vault.ID, rule.Amount, vault.Currency))

	return nil
}

// ============================================================================
// UPDATE RECURRING RULE
// ============================================================================

func (s *VaultService) UpdateRecurringRule(ctx context.Context, vaultID uuid.UUID, updates UpdateRecurringRuleRequest) error {
	// Get current vault
	vault, err := s.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		return fmt.Errorf("vault not found: %w", err)
	}

	if !vault.RecurringRule.Valid || len(vault.RecurringRule.RawMessage) == 0 {
		return errors.New("no recurring rule found")
	}

	var rule RecurringRule
	if err := json.Unmarshal(vault.RecurringRule.RawMessage, &rule); err != nil {
		return fmt.Errorf("invalid recurring rule json: %w", err)
	}

	// Apply updates
	if updates.Enabled != nil {
		rule.Enabled = *updates.Enabled
	}
	if updates.Amount != nil {
		rule.Amount = *updates.Amount
	}
	if updates.Interval != nil {
		rule.Interval = *updates.Interval
	}
	if updates.TimeOfDay != nil {
		rule.TimeOfDay = *updates.TimeOfDay
	}
	if updates.SkipWeekends != nil {
		rule.SkipWeekends = *updates.SkipWeekends
	}
	if updates.MaxExecutions != nil {
		rule.MaxExecutions = updates.MaxExecutions
	}
	if updates.DayOfWeek != nil {
		rule.DayOfWeek = updates.DayOfWeek
	}
	if updates.DayOfMonth != nil {
		rule.DayOfMonth = updates.DayOfMonth
	}

	// Recalculate next execution
	if rule.Enabled {
		rule.NextExecutionAt = rule.CalculateNextExecution()
	}

	b, _ := json.Marshal(rule)

	// Update database
	return s.store.UpdateRecurringRule(ctx, db.UpdateRecurringRuleParams{
		ID:            vaultID,
		RecurringRule: pqtype.NullRawMessage{RawMessage: b, Valid: true},
	})
}

// ============================================================================
// GET TRANSACTIONS
// ============================================================================

func (s *VaultService) GetVaultTransactions(ctx context.Context, params db.GetVaultTransactionsByVaultIDParams) ([]db.VaultTransaction, error) {
	return s.store.GetVaultTransactionsByVaultID(ctx, params)
}

func (s *VaultService) GetUserTransactions(ctx context.Context, params db.GetVaultTransactionsByUserIDParams) ([]db.VaultTransaction, error) {
	return s.store.GetVaultTransactionsByUserID(ctx, params)
}

// ============================================================================
// UPDATE VAULT
// ============================================================================

func (s *VaultService) UpdateVaultDetails(ctx context.Context, vaultID uuid.UUID, name, description, goalAmount *string) error {
	params := db.UpdateVaultGoalDetailsParams{
		ID: vaultID,
	}

	if name != nil {
		params.VaultName = sql.NullString{String: *name, Valid: true}
	}
	if description != nil {
		params.Description = sql.NullString{String: *description, Valid: true}
	}
	if goalAmount != nil {
		params.GoalAmount = sql.NullString{String: *goalAmount, Valid: true}
	}

	return s.store.UpdateVaultGoalDetails(ctx, params)
}

// ============================================================================
// DELETE VAULT
// ============================================================================

func (s *VaultService) DeleteVault(ctx context.Context, vaultID uuid.UUID) error {
	// Check if vault has balance
	vault, err := s.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrVaultNotFound
		}
		return err
	}

	balance, _ := decimal.NewFromString(vault.CurrentBalance.String)
	if balance.GreaterThan(decimal.Zero) {
		return errors.New("cannot delete vault with balance. Please withdraw funds first")
	}

	return s.store.DeleteVaultGoal(ctx, vaultID)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func (s *VaultService) validateCreateRequest(req CreateVaultGoalRequest) error {
	if req.UserID == 0 {
		return errors.New("user_id is required")
	}
	if req.Name == "" {
		return errors.New("vault name is required")
	}
	if req.Currency == "" {
		return errors.New("currency is required")
	}

	// Validate currency
	validCurrencies := map[string]bool{"USDT": true, "USDC": true, "NGN": true, "USD": true}
	if !validCurrencies[req.Currency] {
		return ErrInvalidCurrency
	}

	// Validate amount
	if req.TargetAmount != "" {
		amount, err := decimal.NewFromString(req.TargetAmount)
		if err != nil || amount.LessThanOrEqual(decimal.Zero) {
			return errors.New("invalid target amount")
		}
	}

	return nil
}

func (s *VaultService) validateRecurringRule(rule *RecurringRule) error {
	if !rule.Enabled {
		return nil
	}

	amount, err := decimal.NewFromString(rule.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("invalid recurring amount")
	}

	if rule.Interval == "" {
		return errors.New("interval is required")
	}

	validIntervals := map[Interval]bool{
		IntervalDaily:   true,
		IntervalWeekly:  true,
		IntervalMonthly: true,
	}
	if !validIntervals[rule.Interval] {
		return errors.New("invalid interval")
	}

	return nil
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}

func nullBool(b bool) sql.NullBool {
	return sql.NullBool{Bool: b, Valid: true}
}
