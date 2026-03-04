package vaultsavings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/streaks"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

type VaultService struct {
	store           *db.Store
	logger          *logging.Logger
	walletService   *wallet.WalletService
	emailService    *service.Plunk
	pushService     *service.PushNotificationService
	notifService    *service.Notification
	streakScheduler *streaks.StreakScheduler
}

func NewVaultService(
	store *db.Store,
	logger *logging.Logger,
	walletService *wallet.WalletService,
	emailService *service.Plunk,
	pushService *service.PushNotificationService,
	notifService *service.Notification,
	streakScheduler *streaks.StreakScheduler,
) *VaultService {
	return &VaultService{
		store:           store,
		logger:          logger,
		walletService:   walletService,
		emailService:    emailService,
		pushService:     pushService,
		notifService:    notifService,
		streakScheduler: streakScheduler,
	}
}

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

type Weekday int

const (
	Monday Weekday = iota + 1
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
	Sunday
)

type SavingsFrequency string

const (
	SavingsFrequencyDaily   SavingsFrequency = "daily"
	SavingsFrequencyWeekly  SavingsFrequency = "weekly"
	SavingsFrequencyMonthly SavingsFrequency = "monthly"
)

type SavingsType string

const (
	SavingsTypeFlexible SavingsType = "flexible"
	SavingsTypeLocked   SavingsType = "locked"
)

type SavingsStatus string

const (
	SavingsStatusActive    SavingsStatus = "active"
	SavingsStatusPaused    SavingsStatus = "paused"
	SavingsStatusCancelled SavingsStatus = "cancelled"
	SavingsStatusCompleted SavingsStatus = "completed"
)

type TransactionType string

const (
	TransactionTypeDeposit           TransactionType = "deposit"
	TransactionTypeWithdrawal        TransactionType = "withdrawal"
	TransactionTypeAutoSave          TransactionType = "auto_save"
	TransactionTypeYieldCredit       TransactionType = "yield_credit"
	TransactionTypeSavingsDeposit    TransactionType = "savings_deposit"
	TransactionTypeSavingsWithdrawal TransactionType = "savings_withdrawal"
)

type TransactionStatus string

const (
	TransactionStatusPending    TransactionStatus = "pending"
	TransactionStatusSuccessful TransactionStatus = "successful"
	TransactionStatusFailed     TransactionStatus = "failed"
)

type TransactionRequires2fa bool

const (
	TransactionRequires2faTrue  TransactionRequires2fa = true
	TransactionRequires2faFalse TransactionRequires2fa = false
)

// ============================================================================
// RECURRING RULE MODELS
// ============================================================================
// Interval defines the frequency of recurring deposits
type Interval string

const (
	IntervalDaily   Interval = "daily"   // Execute once per day
	IntervalWeekly  Interval = "weekly"  // Execute once per week
	IntervalMonthly Interval = "monthly" // Execute once per month
)

// RecurringRule represents the configuration for automated recurring deposits to a vault.
// This struct is stored as JSONB in PostgreSQL, allowing for flexible and extensible
// configuration without schema changes.
//
// Example Usage:
//   // Weekly deposit every Friday at 9 AM
//   rule := &RecurringRule{
//       Enabled:           true,
//       Amount:            "100.00",
//       Interval:          IntervalWeekly,
//       DayOfWeek:         ptr(time.Friday),
//       TimeOfDay:         "09:00",
//       SkipWeekends:      false,
//       NotifyOnSuccess:   true,
//       PauseOnLowBalance: true,
//   }

// RecurringRule represents the flexible JSONB recurring deposit configuration
type RecurringRule struct {
	// ========================================================================
	// CORE CONFIGURATION
	// ========================================================================

	// Enabled indicates whether recurring deposits are active.
	// When false, the scheduler will skip this vault entirely.
	// Users can toggle this to pause/resume their auto-savings without losing configuration.
	Enabled bool `json:"enabled" enum:"true,false"`

	// Amount is the deposit amount as a string to maintain precision.
	// Should be a valid decimal number (e.g., "100.00", "50", "25.50").
	// This is the amount transferred from wallet to vault on each execution.
	Amount string `json:"amount" example:"100.00"`

	// Interval defines how frequently deposits occur.
	// Valid values: "daily", "weekly", "monthly"
	// Determines which calculation method is used for NextExecutionAt.
	Interval Interval `json:"interval" enum:"daily,weekly,monthly"`

	// ========================================================================
	// SCHEDULING
	// ========================================================================

	// StartDate is when recurring deposits should begin.
	// Executions will not occur before this date.
	// Useful for scheduling future auto-savings (e.g., "start next month").
	StartDate time.Time `json:"start_date" example:"2024-07-01T00:00:00Z"`

	// EndDate is when recurring deposits should stop (optional).
	// If set, no executions will occur after this date.
	// Nil means deposits continue indefinitely until manually stopped.
	// Useful for time-limited savings plans (e.g., "save for 6 months").
	EndDate *time.Time `json:"end_date,omitempty" example:"2024-12-31T00:00:00Z"`

	// DayOfWeek specifies which day for weekly intervals (optional).
	// Only used when Interval is "weekly".
	// Values: Sunday=0, Monday=1, ..., Saturday=6
	// Example: ptr(time.Friday) for deposits every Friday.
	DayOfWeek *Weekday `json:"day_of_week,omitempty" example:"5"`

	// DayOfMonth specifies which day for monthly intervals (optional).
	// Only used when Interval is "monthly".
	// Valid range: 1-31, or -1 for last day of month
	// If day doesn't exist (e.g., Feb 30), uses last valid day.
	// Example: 1 = first of month, 15 = mid-month, -1 = last day
	DayOfMonth *int `json:"day_of_month,omitempty" example:"15"`

	// TimeOfDay specifies when deposits execute (format: "HH:MM").
	// Uses 24-hour format. Timezone is server timezone.
	// Example: "09:00" = 9 AM, "14:30" = 2:30 PM, "00:00" = midnight
	// Applied to all intervals (daily/weekly/monthly).
	TimeOfDay string `json:"time_of_day" example:"09:00"`

	// NextExecutionAt is the calculated timestamp for the next deposit.
	// Automatically updated after each execution via CalculateNextExecution().
	// Scheduler checks this field to determine if execution is due.
	// This field is managed by the system, not user-configurable.
	NextExecutionAt time.Time `json:"next_execution_at"`

	// ========================================================================
	// EXECUTION TRACKING
	// ========================================================================

	// ExecutionCount tracks how many times this rule has executed successfully.
	// Incremented after each successful deposit.
	// Used with MaxExecutions to limit total number of deposits.
	// Useful for analytics and debugging.
	ExecutionCount int `json:"execution_count"`

	// MaxExecutions limits total number of deposits (optional).
	// When ExecutionCount reaches this value, rule becomes inactive.
	// Nil means unlimited executions.
	// Example: ptr(12) for exactly 12 monthly deposits (1 year).
	MaxExecutions *int `json:"max_executions,omitempty" example:"12"`

	// LastExecutionAt records when the last successful deposit occurred (optional).
	// Updated after each successful execution.
	// Useful for debugging, auditing, and showing users last activity.
	// Nil if never executed.
	LastExecutionAt *time.Time `json:"last_execution_at,omitempty"`

	// ========================================================================
	// BEHAVIOR CONFIGURATION
	// ========================================================================

	// SkipWeekends determines if Saturday/Sunday executions are postponed.
	// When true and execution falls on weekend, moves to next Monday.
	// Useful for mimicking payroll schedules or avoiding weekend processing.
	// Only affects calculated NextExecutionAt, not stored schedules
	SkipWeekends bool `json:"skip_weekends"`

	// RetryOnFailure enables automatic retry of failed deposits.
	// When true, failed deposits will be retried up to MaxRetries times.
	// Failures may occur due to insufficient wallet balance, network issues, etc.
	// Helps ensure deposits eventually succeed for transient failures.
	RetryOnFailure bool `json:"retry_on_failure"`

	// MaxRetries specifies maximum retry attempts for failed deposits.
	// Only used when RetryOnFailure is true.
	// After MaxRetries failed attempts, execution is skipped and marked as failed.
	// Example: 3 means try initial + 3 retries = 4 total attempts.
	MaxRetries int `json:"max_retries"`

	// RetryCount tracks current retry attempt for this execution cycle.
	// Incremented on each retry, reset to 0 after successful execution.
	// When this reaches MaxRetries, retries stop.
	// Managed by the system during failure handling.
	RetryCount int `json:"retry_count"`

	// PauseOnLowBalance determines behavior when wallet has insufficient funds.
	// When true: execution is skipped, rule remains enabled (will retry next cycle)
	// When false: execution fails, may trigger failure notifications
	// Recommended: true for better user experience (avoids failure emails for expected low balance)
	PauseOnLowBalance bool `json:"pause_on_low_balance"`

	// ========================================================================
	// NOTIFICATIONS
	// ========================================================================

	// NotifyOnSuccess determines if user receives notification after successful deposits.
	// When true, triggers email and/or push notification.
	// Useful for users who want confirmation of each auto-save.
	// Can be disabled to reduce notification fatigue for frequent deposits.
	NotifyOnSuccess bool `json:"notify_on_success"`

	// NotifyOnFailure determines if user receives notification when deposits fail.
	// When true, sends alert about failed deposit with reason.
	// Recommended: true so users can address issues (low balance, etc.)
	// Important for debugging and user awareness.
	NotifyOnFailure bool `json:"notify_on_failure"`

	// ========================================================================
	// EXTENSIBILITY
	// ========================================================================

	// Metadata stores additional custom data for future features (optional).
	// Free-form map for extension without schema changes.
	// Examples:
	//   - {"campaign_id": "summer_savings"}
	//   - {"goal_milestone": 50}
	//   - {"user_note": "Emergency fund"}
	// Not used by core system, available for custom logic.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Scan implements sql.Scanner interface for RecurringRule
func (r *RecurringRule) Scan(value any) error {
	if value == nil {
		return nil
	}

	var bytes []byte

	// Handle both []byte and pqtype.NullRawMessage
	switch v := value.(type) {
	case []byte:
		bytes = v
	case pqtype.NullRawMessage:
		if !v.Valid {
			return nil
		}
		bytes = v.RawMessage
	default:
		return fmt.Errorf("failed to scan RecurringRule: unsupported type %T", value)
	}

	if len(bytes) == 0 {
		return nil
	}

	return json.Unmarshal(bytes, r)
}

// Value implements driver.Valuer interface for RecurringRule
func (r RecurringRule) Value() (any, error) {
	if r.Amount == "" {
		return nil, nil
	}
	bytes, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return pqtype.NullRawMessage{RawMessage: bytes, Valid: true}, nil
}

// ============================================================================
// RULE STATUS CHECKS
// ============================================================================

// IsActive checks if the recurring rule should be executed right now.
// Returns true only if ALL conditions are met:
//   - Rule is enabled
//   - Current time is after StartDate
//   - Current time is before EndDate (if set)
//   - ExecutionCount hasn't reached MaxExecutions (if set)
//
// This is the primary check used by the scheduler to determine
// if a vault is eligible for recurring deposit processing.
//
// Example:
//
//	if rule.IsActive() {
//	    // Process recurring deposit
//	}
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

// ============================================================================
// EXECUTION TIME CALCULATION
// ============================================================================

func (r *RecurringRule) CalculateNextExecution() time.Time {
	now := time.Now()

	// if last execution exists, calculate from there
	if r.LastExecutionAt != nil {
		return r.CalculateFromLastExecution(now)
	}

	// first execution - find the next valid execution time
	return r.calculateFirstExecution(now)
}
func (r *RecurringRule) CalculateFromLastExecution(now time.Time) time.Time {

	var next time.Time

	switch r.Interval {
	case IntervalDaily:
		next = r.LastExecutionAt.Add(24 * time.Hour)
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

func (r *RecurringRule) calculateFirstExecution(now time.Time) time.Time {
	var next time.Time

	switch r.Interval {
	case IntervalDaily:
		next = now
	case IntervalWeekly:
		next = r.calculateNextWeekly(now)
	case IntervalMonthly:
		next = r.calculateNextMonthly(now)
	default:
		next = now
	}

	next = r.applyTimeOfDay(next)

	// if calcuated time is in the past, move to the next interval
	if next.Before(now) || next.Equal(now) {
		switch r.Interval {
		case IntervalDaily:
			next = next.Add(24 * time.Hour)
		case IntervalWeekly:
			next = next.Add(7 * 24 * time.Hour)
		case IntervalMonthly:
			next = r.addMonth(next)
		}
	}

	// Skip weekends if configured
	if r.SkipWeekends {
		next = r.skipWeekends(next)
	}

	return next
}

func (r *RecurringRule) addMonth(t time.Time) time.Time {
	// Add one month, handling year rollover
	return time.Date(t.Year(), t.Month()+1, t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// calculateNextWeekly computes next execution for weekly interval.
// Advances by 7 days, then adjusts to configured DayOfWeek if set.
//
// Example:
//
//	If DayOfWeek=Friday and today is Monday,
//	returns next Friday instead of next Monday.
func (r *RecurringRule) calculateNextWeekly(from time.Time) time.Time {
	if r.DayOfWeek == nil {
		// NO specific day, use simple weekly calculation for next executiom

		if r.LastExecutionAt != nil {
			return r.LastExecutionAt.Add(7 * 24 * time.Hour)
		}

		// First execution - if todays time hasnt passed, use today
		todayExecution := r.applyTimeOfDay(from)
		if todayExecution.After(from) {
			return todayExecution
		}
		// Otherwise, schedule for next week
		return todayExecution.Add(7 * 24 * time.Hour)
	}

	targetWeekday := time.Weekday(*r.DayOfWeek)
	currentWeekday := from.Weekday()

	if currentWeekday == targetWeekday {
		todayExecution := r.applyTimeOfDay(from)
		// If today's execution time hasn't passed yet, use today
		if todayExecution.After(from) {
			return todayExecution
		}
		// If time has passed, schedule for next week
		return todayExecution.Add(7 * 24 * time.Hour)
	}

	// calculate days until target weekday
	daysUntiTarget := int(targetWeekday - currentWeekday)
	if daysUntiTarget <= 0 {
		daysUntiTarget += 7 // move to next week
	}

	nextDate := from.Add(time.Duration(daysUntiTarget) * 24 * time.Hour)
	nextExecution := r.applyTimeOfDay(nextDate)

	// If this calculated time is in the past (shouldn't happen with proper logic),
	// move to next week
	if nextExecution.Before(from) || nextExecution.Equal(from) {
		nextExecution = nextExecution.Add(7 * 24 * time.Hour)
	}

	return nextExecution
}

// calculateNextMonthly computes next execution for monthly interval.
// Moves to next month, then sets to configured DayOfMonth if specified.
//
// Special handling:
//   - DayOfMonth=-1 means last day of month
//   - If DayOfMonth > days in month, uses last day (e.g., Feb 30 → Feb 28/29)
//
// Example:
//
//	If DayOfMonth=15 and today is Jan 20,
//	returns Feb 15
func (r *RecurringRule) calculateNextMonthly(from time.Time) time.Time {
	now := time.Now()
	year, month, day := now.Date()

	targetDay := 1
	if r.DayOfMonth != nil {
		if *r.DayOfMonth == -1 {
			// last day of month
			targetDay = r.lastDayOfMonth(year, month)
		} else {
			targetDay = *r.DayOfMonth
			// ensure day is valid for the month
			lastDay := r.lastDayOfMonth(year, month)
			if targetDay > lastDay {
				targetDay = lastDay
			}
		}
	}

	// check if execution can be done this month
	if day <= targetDay {
		// We're before or on the target day this month
		thisMonthExecution := time.Date(year, month, targetDay, 0, 0, 0, 0, from.Location())

		// if execution time has not passed, use this month
		if thisMonthExecution.After(now) {
			return thisMonthExecution
		}
	}

	// else move to next month
	month++
	if month > 12 {
		month = 1
		year++
	}

	// determine day for next month
	if r.DayOfMonth != nil {
		if *r.DayOfMonth == -1 {
			// last day of month
			targetDay = r.lastDayOfMonth(year, month)
		} else {
			targetDay = *r.DayOfMonth
			// ensure day is valid for the month
			lastDay := r.lastDayOfMonth(year, month)
			if targetDay > lastDay {
				targetDay = lastDay
			}
		}
	}

	nextExecution := time.Date(year, month, targetDay, 0, 0, 0, 0, from.Location())
	return r.applyTimeOfDay(nextExecution)
}

// lastDayOfMonth returns the last day number for a given month/year.
// Accounts for leap years (February 29).
//
// Example:
//
//	lastDayOfMonth(2024, February) = 29 (leap year)
//	lastDayOfMonth(2023, February) = 28
//	lastDayOfMonth(2024, January) = 31
func (r *RecurringRule) lastDayOfMonth(year int, month time.Month) int {
	// First day of next month minus one day
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// applyTimeOfDay sets the execution time to the configured TimeOfDay.
// Preserves the date but changes the hour and minute.
//
// Format: "HH:MM" (24-hour)
// Example:
//
//	If t is "2024-01-15 10:30" and TimeOfDay is "14:00",
//	returns "2024-01-15 14:00"
func (r *RecurringRule) applyTimeOfDay(t time.Time) time.Time {
	if r.TimeOfDay == "" {
		return t
	}

	// Parse time (format: "HH:MM")
	var hour, minute int
	fmt.Sscanf(r.TimeOfDay, "%d:%d", &hour, &minute)

	return time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, t.Location())
}

// skipWeekends moves execution from weekend to next Monday.
// Only moves forward, never backward.
//
// Example:
//
//	Saturday → Monday
//	Sunday → Monday
//	Weekday → unchanged
func (r *RecurringRule) skipWeekends(t time.Time) time.Time {
	for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		t = t.Add(24 * time.Hour)
	}
	return t
}

// ============================================================================
// EXECUTION MANAGEMENT
// ============================================================================

// MarkExecuted updates the rule after a successful deposit execution.
// This method should be called by the scheduler after processing.
//
// Actions performed:
//   - Increments ExecutionCount
//   - Sets LastExecutionAt to now
//   - Calculates and sets NextExecutionAt
//   - Resets RetryCount to 0
//
// After calling this, the rule should be saved back to the database.
//
// Example:
//   rule.MarkExecuted()
//   db.UpdateRecurringRule(ctx, vaultID, rule)

func (r *RecurringRule) MarkExecuted() {
	now := time.Now()
	r.ExecutionCount++
	r.LastExecutionAt = &now
	r.NextExecutionAt = r.CalculateNextExecution()
	r.RetryCount = 0 // Reset retry count on success
}

// ============================================================================
// HELPER CONSTRUCTORS
// ============================================================================

// NewDailyRecurringRule creates a rule for daily deposits.
// Deposits occur every day at the specified time.
//
// Parameters:
//   - amount: Deposit amount (e.g., "10.00")
//   - timeOfDay: Execution time (e.g., "09:00")
//
// Returns: Configured RecurringRule with sensible defaults
//
// Example:
//
//	rule := NewDailyRecurringRule("5.00", "08:00")
//	// Deposits $5 every day at 8 AM
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

// NewWeeklyRecurringRule creates a rule for weekly deposits.
// Deposits occur once per week on the specified day.
//
// Parameters:
//   - amount: Deposit amount (e.g., "100.00")
//   - dayOfWeek: Day to deposit (e.g., time.Friday)
//   - timeOfDay: Execution time (e.g., "09:00")
//
// Returns: Configured RecurringRule with sensible defaults
//
// Example:
//
//	rule := NewWeeklyRecurringRule("100.00", time.Friday, "14:00")
//	// Deposits $100 every Friday at 2 PM
func NewWeeklyRecurringRule(amount string, dayOfWeek Weekday, timeOfDay string) *RecurringRule {
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

// NewMonthlyRecurringRule creates a rule for monthly deposits.
// Deposits occur once per month on the specified day.
//
// Parameters:
//   - amount: Deposit amount (e.g., "500.00")
//   - dayOfMonth: Day to deposit (1-31, or -1 for last day)
//   - timeOfDay: Execution time (e.g., "09:00")
//
// Returns: Configured RecurringRule with sensible defaults
//
// Example:
//
//		rule := NewMonthlyRecurringRule("500.00", 1, "00:00")
//		// Deposits $500 on the 1st of every month at midnight
//
//		rule := NewMonthlyRecurringRule("1000.00", -1, "23:59")
//	  // Deposits $1000 on the last day of every month at 11:59 PM
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
	Name          string         `json:"name" binding:"required" example:"Vacation Fund"`
	Description   *string        `json:"description" example:"Saving for a trip to Hawaii"`
	TargetAmount  string         `json:"target_amount" binding:"required" example:"5000.00"`
	Currency      string         `json:"currency" binding:"required" example:"NGN" enums:"NGN,USD,USDC,USDT"`
	VaultType     string         `json:"vault_type,omitempty" default:"flexible" enums:"flexible,locked"`
	RecurringRule *RecurringRule `json:"recurring_rule,omitempty"`
}

type VaultSavingResponse struct {
	ID                   uuid.UUID      `json:"id"`
	UserID               int64          `json:"user_id"`
	VaultName            string         `json:"vault_name"`
	Description          string         `json:"description"`
	GoalAmount           string         `json:"goal_amount"`
	CurrentBalance       string         `json:"current_balance"`
	Currency             string         `json:"currency"`
	AutoSaveEnabled      bool           `json:"auto_save_enabled"`
	AutoSaveFrequency    string         `json:"auto_save_frequency"`
	AutoSaveAmount       string         `json:"auto_save_amount"`
	NextAutoSave         time.Time      `json:"next_auto_save"`
	RecurringRule        map[string]any `json:"recurring_rule"`
	TotalYieldEarned     string         `json:"total_yield_earned"`
	NextYieldCalculation time.Time      `json:"next_yield_calculation"`
	LastYieldCalculation time.Time      `json:"last_yield_calculation"`
	Status               string         `json:"status"`
	VaultType            string         `json:"vault_type"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	CompletedAt          time.Time      `json:"completed_at"`
}

func MapVaultSavingToResponse(vs *db.VaultSaving) *VaultSavingResponse {
	return &VaultSavingResponse{
		ID:                   vs.ID,
		UserID:               vs.UserID,
		VaultName:            vs.VaultName,
		Description:          vs.Description.String,
		GoalAmount:           vs.GoalAmount.String,
		CurrentBalance:       vs.CurrentBalance.String,
		Currency:             vs.Currency,
		AutoSaveEnabled:      vs.AutoSaveEnabled,
		AutoSaveFrequency:    vs.AutoSaveFrequency.String,
		AutoSaveAmount:       vs.AutoSaveAmount.String,
		NextAutoSave:         vs.NextAutoSave.Time,
		RecurringRule:        utils.UnmarshalMetadata(vs.RecurringRule),
		TotalYieldEarned:     vs.TotalYieldEarned.String,
		NextYieldCalculation: vs.NextYieldCalculation.Time,
		LastYieldCalculation: vs.LastYieldCalculation.Time,
		Status:               vs.Status,
		VaultType:            vs.VaultType,
		CreatedAt:            vs.CreatedAt,
		UpdatedAt:            vs.UpdatedAt,
		CompletedAt:          vs.CompletedAt.Time,
	}
}

type GetVaultGoalProgressResponse struct {
	ID                 uuid.UUID `json:"id"`
	VaultName          string    `json:"vault_name"`
	CurrentBalance     string    `json:"current_balance"`
	GoalAmount         string    `json:"goal_amount"`
	ProgressPercentage int32     `json:"progress_percentage"`
	GoalReached        bool      `json:"goal_reached"`
}

func MapGetVaultGoalProgressRowToReponse(a *db.GetVaultGoalProgressRow) *GetVaultGoalProgressResponse {
	return &GetVaultGoalProgressResponse{
		ID:                 a.ID,
		VaultName:          a.VaultName,
		CurrentBalance:     a.CurrentBalance.String,
		GoalAmount:         a.GoalAmount.String,
		ProgressPercentage: a.ProgressPercentage,
		GoalReached:        a.GoalReached,
	}
}

type DepositRequest struct {
	UserID         int64     `json:"user_id"`
	VaultID        uuid.UUID `json:"vault_id"`
	FromWalletID   uuid.UUID `json:"from_wallet_id" binding:"required"`
	Amount         string    `json:"amount" binding:"required"`
	Currency       string    `json:"currency"`
	Description    string    `json:"description"`
	IdempotencyKey string    `json:"idempotency_key" binding:"required"`
}

type DepositResponse struct {
	ID                    uuid.UUID      `json:"id"`
	UserID                int64          `json:"user_id"`
	VaultID               uuid.UUID      `json:"vault_id"`
	TransactionType       string         `json:"transaction_type"`
	Amount                string         `json:"amount"`
	Currency              string         `json:"currency"`
	SourceWallet          uuid.UUID      `json:"source_wallet"`
	DestinationWallet     uuid.UUID      `json:"destination_wallet"`
	BalanceBefore         string         `json:"balance_before"`
	BalanceAfter          string         `json:"balance_after"`
	Reference             string         `json:"reference"`
	Description           string         `json:"description"`
	Metadata              map[string]any `json:"metadata"`
	Status                string         `json:"status"`
	Requires2fa           bool           `json:"requires_2fa"`
	TwoFaVerifiedAt       time.Time      `json:"two_fa_verified_at"`
	RequiresAdminApproval bool           `json:"requires_admin_approval"`
	AdminApprovedBy       int64          `json:"admin_approved_by"`
	AdminApprovedAt       time.Time      `json:"admin_approved_at"`
	ApprovalNotes         string         `json:"approval_notes"`
	CompletedAt           time.Time      `json:"completed_at"`
	CreatedAt             time.Time      `json:"created_at"`
}

func MapVaultTxToDepositResponse(d *db.VaultTransaction) *DepositResponse {
	return &DepositResponse{
		ID:                d.ID,
		UserID:            d.UserID,
		Amount:            d.Amount,
		VaultID:           d.VaultID,
		TransactionType:   d.TransactionType,
		Currency:          d.Currency,
		SourceWallet:      d.SourceWallet.UUID,
		DestinationWallet: d.DestinationWallet.UUID,
		BalanceBefore:     d.BalanceBefore,
		BalanceAfter:      d.BalanceAfter,
		Reference:         d.Reference.String,
		Description:       d.Description.String,
		Metadata:          utils.UnmarshalMetadata(d.Metadata),
		Status:            d.Status.String,
		Requires2fa:       d.Requires2fa.Bool,
		CompletedAt:       d.CompletedAt.Time,
		CreatedAt:         d.CreatedAt,
	}
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
	Enabled       *bool     `json:"enabled,omitempty"`
	Amount        *string   `json:"amount,omitempty"`
	Interval      *Interval `json:"interval,omitempty"`
	DayOfWeek     *Weekday  `json:"day_of_week,omitempty"`
	DayOfMonth    *int      `json:"day_of_month,omitempty"`
	TimeOfDay     *string   `json:"time_of_day,omitempty"`
	SkipWeekends  *bool     `json:"skip_weekends,omitempty"`
	MaxExecutions *int      `json:"max_executions,omitempty"`
}

// ============================================================================
// CREATE VAULT GOAL
// ============================================================================
func (s *VaultService) CreateVaultGoal(ctx context.Context, req CreateVaultGoalRequest, userID int64, ip, ua string) (*VaultSavingResponse, error) {
	// Get user
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if !user.Verified {
		return nil, fmt.Errorf("you need to verify your email")
	}

	if !user.IsActive {
		return nil, fmt.Errorf("account inactive")
	}

	if !user.IsKycVerified {
		return nil, fmt.Errorf("you need to complete KYC verification to create a vault goal")
	}

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

	// Set default vault type
	if req.VaultType == "" {
		req.VaultType = string(SavingsTypeFlexible)
	}

	var freq, amt string
	var nextAuto sql.NullTime
	if req.RecurringRule != nil {
		freq = string(req.RecurringRule.Interval)
		amt = req.RecurringRule.Amount
		nextAuto = nullTime(req.RecurringRule.NextExecutionAt)
	}

	var recurring pqtype.NullRawMessage
	if req.RecurringRule != nil {
		b, _ := json.Marshal(req.RecurringRule)
		recurring = pqtype.NullRawMessage{RawMessage: b, Valid: true}
	}

	// Create vault goal
	params := db.CreateVaultGoalParams{
		UserID:            userID,
		VaultName:         req.Name,
		Description:       nullString(stringOrEmpty(req.Description)),
		GoalAmount:        nullString(req.TargetAmount),
		CurrentBalance:    nullString("0.00"),
		Currency:          req.Currency,
		AutoSaveEnabled:   req.RecurringRule != nil && req.RecurringRule.Enabled,
		AutoSaveFrequency: nullString(freq),
		AutoSaveAmount:    nullString(amt),
		NextAutoSave:      nextAuto,
		RecurringRule:     recurring,
		Status:            string(SavingsStatusActive),
		VaultType:         req.VaultType,
	}

	vault, err := s.store.CreateVaultGoal(ctx, params)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create vault goal: %v", err))
		return nil, fmt.Errorf("failed to create vault goal: %w", err)
	}

	// Send notifications (async, don't block on errors)
	go func() {
		bgCtx := context.Background()

		if s.notifService != nil {
			if _, err := s.notifService.CreateWithRecipients(bgCtx, nil, "Vault savings created", fmt.Sprintf("You have created a %s:%s savings plan", vault.VaultType, vault.VaultName), "system", []int64{userID}); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to create vault goal notification: %v", err))
			}
		}

		if s.emailService != nil {
			if err := s.emailService.SendGoalCreatedEmail(bgCtx, &user, req.Name, req.Currency, req.TargetAmount); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to send goal created email: %v", err))
			}
		}

		if s.pushService != nil {
			if err := s.pushService.SendVaultGoalCreatedPush(bgCtx, userID, req.Name); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to send goal created push: %v", err))
			}
		}
	}()

	s.logger.Info(fmt.Sprintf("Successfully created vault goal %s for user %d", vault.ID, userID))
	return MapVaultSavingToResponse(&vault), nil
}

// ============================================================================
// GET VAULT GOALS
// ============================================================================

func (s *VaultService) GetVaultByID(ctx context.Context, vaultID uuid.UUID) (*VaultSavingResponse, error) {
	vault, err := s.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVaultNotFound
		}
		return nil, err
	}
	return MapVaultSavingToResponse(&vault), nil
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

func (s *VaultService) GetVaultYieldID(ctx context.Context, yieldID uuid.UUID) (db.VaultYield, error) {
	return s.store.GetVaultYieldByID(ctx, yieldID)
}

func (s *VaultService) ListVaultYields(ctx context.Context, vaultID uuid.UUID, limit, offset int32) ([]db.VaultYield, error) {
	return s.store.GetVaultYieldsByVaultID(ctx, db.GetVaultYieldsByVaultIDParams{
		VaultID: vaultID,
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *VaultService) GetTotalVaultYields(ctx context.Context, vaultID uuid.UUID) (string, error) {
	return s.store.GetTotalYieldEarned(ctx, vaultID)
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
	sourceWallet, err := qtx.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
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
	// newWalletBalance := walletBalance.Sub(amount)

	// Generate reference
	reference := req.IdempotencyKey

	amountUsd, err := utils.ConvertToUSD(ctx, amount, vault.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	// Create main Transaction record
	maintx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          req.UserID,
		Type:            string(transaction.Vault),
		Description:     sql.NullString{String: req.Description, Valid: true},
		Amount:          amount.String(),
		Currency:        vault.Currency,
		AmountUsd:       amountUsd.String(),
		Status:          string(transaction.Pending),
		TransactionFlow: string(transaction.InPlatform),
		IdempotencyKey:  reference,
		TFrom:           string(transaction.Wallet),
		TTo:             string(transaction.Vault),
		Direction:       string(transaction.Debit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction record: %w", err)
	}

	// Create transaction record
	txParams := db.CreateVaultTransactionParams{
		UserID:          req.UserID,
		VaultID:         req.VaultID,
		TransactionType: string(TransactionTypeDeposit),
		Amount:          req.Amount,
		Currency:        req.Currency,
		SourceWallet:    uuid.NullUUID{UUID: req.FromWalletID, Valid: true},
		BalanceBefore:   vault.CurrentBalance.String,
		BalanceAfter:    newVaultBalance.String(),
		Reference:       sql.NullString{String: reference, Valid: true},
		Description:     sql.NullString{String: req.Description, Valid: true},
		Status:          sql.NullString{String: string(transaction.Pending), Valid: true},
		Requires2fa:     sql.NullBool{Bool: false, Valid: true},
		TransactionID:   uuid.NullUUID{UUID: maintx.ID, Valid: true},
	}

	vtx, err := qtx.CreateVaultTransaction(ctx, txParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Update vault balance
	if err := qtx.IncrementVaultBalance(ctx, db.IncrementVaultBalanceParams{
		ID:             req.VaultID,
		CurrentBalance: nullString(req.Amount),
	}); err != nil {
		return nil, fmt.Errorf("failed to update vault balance: %w", err)
	}

	// Update wallet balance
	_, err = qtx.DecrementWalletBalance(ctx, db.DecrementWalletBalanceParams{
		ID:      req.FromWalletID,
		Balance: sql.NullString{String: req.Amount, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update wallet balance: %w", err)
	}

	// Update main transaction status to Success
	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     maintx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update main transaction status: %w", err)
	}

	err = qtx.UpdateVaultTransactionStatus(ctx, db.UpdateVaultTransactionStatusParams{
		ID:     vtx.ID,
		Status: sql.NullString{String: string(transaction.Success), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update vault transaction status: %w", err)
	}

	// Check if goal reached
	goalAmount, err := decimal.NewFromString(vault.GoalAmount.String)
	goalReached := false
	if err == nil && newVaultBalance.GreaterThanOrEqual(goalAmount) {
		goalReached = true
		if err := qtx.UpdateVaultStatus(ctx, db.UpdateVaultStatusParams{
			ID:     req.VaultID,
			Status: string(SavingsStatusCompleted),
		}); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to mark vault as completed: %v", err))
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	progress := newVaultBalance.Div(goalAmount).Mul(decimal.NewFromInt(100)).String()

	if progress == "" {
		progress = "0"
	}

	// Update user streak
	if err := s.streakScheduler.UpdateStreakOnTransaction(ctx, req.UserID, maintx.ID, "vault"); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to update user streak: %v", err))
	}
	

	// err = s.store.UpdateUserTransactionVolume(ctx, db.UpdateUserTransactionVolumeParams{
	// 	TotalTransactionVolume: sql.NullString{String: req.Amount, Valid: true},
	// 	ID: req.UserID,
	// })
	// if err != nil {
	// 	s.logger.Error(fmt.Sprintf("Failed to update user transaction volume: %v", err))
	// }

	// Get user for notifications
	user, err := s.store.GetUserByID(ctx, req.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get user for notifications: %v", err))
	} else {
		// Send notifications (async)
		go func() {
			bgCtx := context.Background()

			if goalReached {
				daysToComplete := int(time.Since(vault.CreatedAt).Hours() / 24)
				if s.emailService != nil {
					_ = s.emailService.SendGoalCompletedEmail(bgCtx, &user, vault.VaultName, goalAmount.String(), vault.Currency, fmt.Sprintf("%d days", daysToComplete))
				}
				if s.pushService != nil {
					_ = s.pushService.SendGoalCompletedPush(bgCtx, req.UserID, vault.VaultName)
				}
				if s.notifService != nil {
					if _, err := s.notifService.CreateWithRecipients(bgCtx, nil, "Vault Goal Completed", fmt.Sprintf("Congratulations! You have completed your vault goal: %s", vault.VaultName), "system", []int64{req.UserID}); err != nil {
						s.logger.Error(fmt.Sprintf("Failed to create goal completed notification: %v", err))
					}
				}
			} else {
				if s.emailService != nil {
					_ = s.emailService.SendDepositSuccessEmail(bgCtx, &user, vault.VaultName, req.Amount, req.Currency, newVaultBalance.String(), reference)
				}
				if s.pushService != nil {
					_ = s.pushService.SendDepositSuccessPush(bgCtx, req.UserID, vault.VaultName, req.Amount, req.Currency)
				}
				if s.notifService != nil {
					if _, err := s.notifService.CreateWithRecipients(bgCtx, nil, "Vault Deposit Successful", fmt.Sprintf("You have deposited %s %s to your vault: %s", req.Amount, req.Currency, vault.VaultName), "system", []int64{req.UserID}); err != nil {
						s.logger.Error(fmt.Sprintf("Failed to create deposit notification: %v", err))
					}
				}
			}
		}()
	}
	s.logger.Info(fmt.Sprintf("Successfully processed deposit: %s", vtx.ID))
	return &vtx, nil
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
	requires2FA := amount.GreaterThan(decimal.NewFromInt(1000))

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
		reference = utils.NewTxRef("vault_withdrawal")
	}

	amountUsd, err := utils.ConvertToUSD(ctx, amount, vault.Currency)
	if err != nil {
		return nil, fmt.Errorf("failed to convert amount to USD: %w", err)
	}

	// Create main Transaction
	maintx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		Type:            string(transaction.Vault),
		Description:     sql.NullString{String: req.Description, Valid: true},
		Status:          string(transaction.Pending),
		TransactionFlow: string(transaction.InPlatform),
		Amount:          req.Amount,
		Currency:        vault.Currency,
		AmountUsd:       amountUsd.String(),
		UserID:          req.UserID,
		TTo:             string(transaction.Wallet),
		TFrom:           string(transaction.Vault),
		Direction:       string(transaction.Credit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction record: %w", err)
	}

	// Create transaction record
	txParams := db.CreateVaultTransactionParams{
		UserID:            req.UserID,
		VaultID:           req.VaultID,
		TransactionType:   string(transaction.Withdrawal),
		Amount:            req.Amount,
		Currency:          vault.Currency,
		DestinationWallet: uuid.NullUUID{UUID: req.ToWalletID, Valid: true},
		BalanceBefore:     vault.CurrentBalance.String,
		BalanceAfter:      newVaultBalance.String(),
		Reference:         sql.NullString{String: reference, Valid: true},
		Description:       sql.NullString{String: req.Description, Valid: req.Description != ""},
		Status:            nullString(string(transaction.Pending)),
		Requires2fa:       nullBool(requires2FA),
		TransactionID:     uuid.NullUUID{UUID: maintx.ID, Valid: true},
	}

	vtx, err := qtx.CreateVaultTransaction(ctx, txParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

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

	_, err = qtx.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
		ID:      destWallet.ID,
		Balance: sql.NullString{String: req.Amount, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update wallet balance: %w", err)
	}

	_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     maintx.ID,
		Status: string(transaction.Success),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to updated transaction [Vault-Withdraw]: %v", err)
	}

	err = qtx.UpdateVaultTransactionStatus(ctx, db.UpdateVaultTransactionStatusParams{
		ID:     maintx.ID,
		Status: nullString(string(transaction.Success)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to updated vault transaction [Vault-Withdraw]: %v", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		_, err = qtx.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     maintx.ID,
			Status: string(transaction.Failed),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to updated transaction [Vault-Withdraw]: %v", err)
		}

		err = qtx.UpdateVaultTransactionStatus(ctx, db.UpdateVaultTransactionStatusParams{
			ID:     maintx.ID,
			Status: nullString(string(transaction.Failed)),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to updated vault transaction [Vault-Withdraw]: %v", err)
		}

		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Update user streak
	if err := s.streakScheduler.UpdateStreakOnTransaction(ctx, req.UserID, maintx.ID, "vault"); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to update user streak: %v", err))
	}
	

	// err = s.store.UpdateUserTransactionVolume(ctx, db.UpdateUserTransactionVolumeParams{
	// 	TotalTransactionVolume: sql.NullString{String: req.Amount, Valid: true},
	// 	ID: req.UserID,
	// })
	// if err != nil {
	// 	s.logger.Error(fmt.Sprintf("Failed to update user transaction volume: %v", err))
	// }

	// Get user for notifications
	user, err := s.store.GetUserByID(ctx, req.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get user for notifications: %v", err))
	} else {
		// Send notifications (async)
		go func() {
			bgCtx := context.Background()

			if requires2FA && s.emailService != nil {
				_ = s.emailService.SendWithdrawal2FARequiredEmail(bgCtx, &user, vtx.ID.String(), reference, req.Amount, vault.Currency, time.Now().Format("02 Jan 2006 15:04 MST"), req.ToWalletID.String())
			} else {
				if s.emailService != nil {
					_ = s.emailService.SendWithdrawalSuccessEmail(bgCtx, &user, vault.VaultName, req.Amount, vault.Currency, reference)
				}
				if s.pushService != nil {
					_ = s.pushService.SendWithdrawalSuccessPush(bgCtx, req.UserID, vault.VaultName, req.Amount, vault.Currency)
				}
				if s.notifService != nil {
					if _, err := s.notifService.CreateWithRecipients(bgCtx, nil, "Vault Withdrawal Successful", fmt.Sprintf("You have withdrawn %s %s from your vault: %s", req.Amount, vault.Currency, vault.VaultName), "system", []int64{req.UserID}); err != nil {
						s.logger.Error(fmt.Sprintf("Failed to create withdrawal notification: %v", err))
					}
				}
			}
		}()
	}

	s.logger.Info(fmt.Sprintf("Successfully processed withdrawal: %s", vtx.ID))
	return &vtx, nil
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
				_ = s.emailService.SendRecurringDepositFailedEmail(ctx, &user, vault.VaultName, rule.Amount, vault.Currency, "Insufficient balance", *rule.LastExecutionAt)
			}
		}
		return nil
	}

	// Process the deposit
	depositReq := DepositRequest{
		UserID:         vault.UserID,
		VaultID:        vault.ID,
		FromWalletID:   sourceWallet.ID,
		Amount:         rule.Amount,
		Currency:       vault.Currency,
		Description:    "Recurring deposit",
		IdempotencyKey: utils.WatRequestID(),
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

	depositDate := time.Now()

	// Send success notification
	if rule.NotifyOnSuccess {
		user, _ := s.store.GetUserByID(ctx, vault.UserID)
		if s.emailService != nil {
			_ = s.emailService.SendRecurringDepositSuccessEmail(ctx, &user, vault.VaultName, rule.Amount, vault.Currency, depositReq.IdempotencyKey, depositDate)
		}
		if s.pushService != nil {
			_ = s.pushService.SendRecurringDepositSuccessPush(ctx, vault.UserID, vault.VaultName, rule.Amount, vault.Currency)
		}
		if s.notifService != nil {
			if _, err := s.notifService.CreateWithRecipients(ctx, nil, "Recurring Deposit Successful", fmt.Sprintf("A recurring deposit of %s %s has been made to your vault: %s", rule.Amount, vault.Currency, vault.VaultName), "system", []int64{vault.UserID}); err != nil {
				s.logger.Error(fmt.Sprintf("Failed to create recurring deposit success notification: %v", err))
			}
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
		return errors.New("VAULT_HAS_BALANCE_ERROR")
	}

	return s.store.DeleteVaultGoal(ctx, vaultID)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func (s *VaultService) validateCreateRequest(req CreateVaultGoalRequest) error {
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
