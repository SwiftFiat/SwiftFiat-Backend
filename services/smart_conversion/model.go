package smartconversion

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ConversionRule struct {
	ID                 uuid.UUID        `json:"id"`
	UserID             int64            `json:"user_id"`
	SourceCurrency     string           `json:"source_currency"`
	TargetCurrency     string           `json:"target_currency"`
	SourceWalletID     uuid.UUID        `json:"source_wallet_id"`
	TargetWalletID     uuid.UUID        `json:"target_wallet_id"`
	TriggerType        string           `json:"trigger_type"`                 // rate_based, scheduled, percentage
	TriggerRate        *decimal.Decimal `json:"trigger_rate,omitempty"`       // applicable for rate_based and percentage triggers
	TriggerCondition   *string          `json:"trigger_condition,omitempty"`  // gte, lte, eq
	ConversionType     string           `json:"conversion_type"`              // fixed_amount, percentage, full_balance
	FixedAmount        *decimal.Decimal `json:"fixed_amount,omitempty"`       // applicable for fixed_amount conversion type
	Percentage         *decimal.Decimal `json:"percentage,omitempty"`         // applicable for percentage conversion type
	ScheduleFrequency  *string          `json:"schedule_frequency,omitempty"` // daily, weekly, monthly
	ScheduleDayOfWeek  *int32           `json:"schedule_day_of_week,omitempty"`
	ScheduleDayOfMonth *int32           `json:"schedule_day_of_month,omitempty"`
	ScheduleTime       *time.Time       `json:"schedule_time,omitempty"`
	NextExecutionAt    *time.Time       `json:"next_execution_at,omitempty"`
	Timezone           string           `json:"timezone"`
	Status             string           `json:"status"` // active, paused, triggered, expired, failed
	IsActive           bool             `json:"is_active"`
	LastTriggeredAt    *time.Time       `json:"last_triggered_at,omitempty"`
	LastTriggerRate    *decimal.Decimal `json:"last_trigger_rate,omitempty"`
	ExecutionCount     int32            `json:"execution_count"`
	MaxExecutions      *int32           `json:"max_executions,omitempty"`
	FailureCount       int32            `json:"failure_count"`
	LastFailureReason  *string          `json:"last_failure_reason,omitempty"`
	Description        *string          `json:"description,omitempty"`
	Label              *string          `json:"label,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	DeletedAt          *time.Time       `json:"deleted_at,omitempty"`
}

type CreateConversionRuleRequest struct {
	SourceCurrency     string           `json:"source_currency" binding:"required,oneof=USD NGN USDT USDC"`
	TargetCurrency     string           `json:"target_currency" binding:"required,oneof=USD NGN USDT USDC"`
	SourceWalletID     uuid.UUID        `json:"source_wallet_id" binding:"required"`
	TargetWalletID     uuid.UUID        `json:"target_wallet_id" binding:"required"`
	TriggerType        string           `json:"trigger_type" binding:"required,oneof=rate_based scheduled percentage"`
	TriggerRate        *decimal.Decimal `json:"trigger_rate"`
	TriggerCondition   *string          `json:"trigger_condition" binding:"omitempty,oneof=gte lte eq"`
	ConversionType     string           `json:"conversion_type" binding:"required,oneof=fixed_amount percentage full_balance"`
	FixedAmount        *decimal.Decimal `json:"fixed_amount"`
	Percentage         *decimal.Decimal `json:"percentage"`
	ScheduleFrequency  *string          `json:"schedule_frequency" binding:"omitempty,oneof=daily weekly monthly custom"`
	ScheduleDayOfWeek  *int             `json:"schedule_day_of_week"`
	ScheduleDayOfMonth *int             `json:"schedule_day_of_month"`
	ScheduleTime       *string          `json:"schedule_time"` // HH:MM format
	Timezone           *string          `json:"timezone"`
	Description        *string          `json:"description"`
	Label              *string          `json:"label"`
}

type UpdateConversionRuleRequest struct {
	TriggerRate *decimal.Decimal `json:"trigger_rate"`
	Percentage  *decimal.Decimal `json:"percentage"`
	FixedAmount *decimal.Decimal `json:"fixed_amount"`
	Label       *string          `json:"label"`
	Description *string          `json:"description"`
}

type ConversionRuleResponse struct {
	ID                  uuid.UUID        `json:"id"`
	SourceCurrency      string           `json:"source_currency"`
	TargetCurrency      string           `json:"target_currency"`
	TriggerType         string           `json:"trigger_type"`
	TriggerRate         *decimal.Decimal `json:"trigger_rate,omitempty"`
	TriggerCondition    *string          `json:"trigger_condition,omitempty"`
	ConversionType      string           `json:"conversion_type"`
	FixedAmount         *decimal.Decimal `json:"fixed_amount,omitempty"`
	Percentage          *decimal.Decimal `json:"percentage,omitempty"`
	ScheduleFrequency   *string          `json:"schedule_frequency,omitempty"`
	NextExecutionAt     *time.Time       `json:"next_execution_at,omitempty"`
	Status              string           `json:"status"`
	IsActive            bool             `json:"is_active"`
	ExecutionCount      int              `json:"execution_count"`
	LastTriggeredAt     *time.Time       `json:"last_triggered_at,omitempty"`
	LastTriggerRate     *decimal.Decimal `json:"last_trigger_rate,omitempty"`
	Label               *string          `json:"label,omitempty"`
	Description         *string          `json:"description,omitempty"`
	SourceWalletBalance decimal.Decimal  `json:"source_wallet_balance"`
	TargetWalletBalance decimal.Decimal  `json:"target_wallet_balance"`
	CreatedAt           time.Time        `json:"created_at"`
}

type ConversionHistory struct {
	ID                       uuid.UUID        `json:"id"`
	ConversionRuleID         *uuid.UUID       `json:"conversion_rule_id,omitempty"`
	UserID                   int64            `json:"user_id"`
	TransactionID            *uuid.UUID       `json:"transaction_id,omitempty"`
	SourceCurrency           string           `json:"source_currency"`
	TargetCurrency           string           `json:"target_currency"`
	SourceWalletID           *uuid.UUID       `json:"source_wallet_id,omitempty"`
	TargetWalletID           *uuid.UUID       `json:"target_wallet_id,omitempty"`
	TriggerRate              *decimal.Decimal `json:"trigger_rate,omitempty"`
	ExecutedRate             decimal.Decimal  `json:"executed_rate"`
	RateDifferencePercentage *decimal.Decimal `json:"rate_difference_percentage,omitempty"`
	RateProvider             *string          `json:"rate_provider,omitempty"`
	SourceAmount             decimal.Decimal  `json:"source_amount"`
	TargetAmount             decimal.Decimal  `json:"target_amount"`
	Fees                     decimal.Decimal  `json:"fees"`
	NetAmount                decimal.Decimal  `json:"net_amount"`
	SourceBalanceBefore      *decimal.Decimal `json:"source_balance_before,omitempty"`
	SourceBalanceAfter       *decimal.Decimal `json:"source_balance_after,omitempty"`
	TargetBalanceBefore      *decimal.Decimal `json:"target_balance_before,omitempty"`
	TargetBalanceAfter       *decimal.Decimal `json:"target_balance_after,omitempty"`
	ExecutionType            string           `json:"execution_type"` // automatic, manual, scheduled
	TriggerType              *string          `json:"trigger_type,omitempty"`
	Status                   string           `json:"status"` // success, failed, pending
	FailureReason            *string          `json:"failure_reason,omitempty"`
	ExecutedAt               time.Time        `json:"executed_at"`
	CreatedAt                time.Time        `json:"created_at"`
}

type ConversionHistoryResponse struct {
	ID              uuid.UUID        `json:"id"`
	RuleLabel       *string          `json:"rule_label,omitempty"`
	SourceCurrency  string           `json:"source_currency"`
	TargetCurrency  string           `json:"target_currency"`
	SourceAmount    decimal.Decimal  `json:"source_amount"`
	TargetAmount    decimal.Decimal  `json:"target_amount"`
	TriggerRate     *decimal.Decimal `json:"trigger_rate,omitempty"`
	ExecutedRate    decimal.Decimal  `json:"executed_rate"`
	RateImprovement *decimal.Decimal `json:"rate_improvement_percentage,omitempty"`
	Fees            decimal.Decimal  `json:"fees"`
	NetAmount       decimal.Decimal  `json:"net_amount"`
	ExecutionType   string           `json:"execution_type"`
	Status          string           `json:"status"`
	ExecutedAt      time.Time        `json:"executed_at"`
}

type ConversionStats struct {
	TotalConversions      int             `json:"total_conversions"`
	SuccessfulConversions int             `json:"successful_conversions"`
	FailedConversions     int             `json:"failed_conversions"`
	TotalConverted        decimal.Decimal `json:"total_converted"`
	TotalFees             decimal.Decimal `json:"total_fees"`
}

type ManualConversionRequest struct {
	SourceCurrency string `json:"source_currency" binding:"required,oneof=USD NGN USDT USDC"`
	TargetCurrency string `json:"target_currency" binding:"required,oneof=USD NGN USDT USDC"`
	Amount         string `json:"amount" binding:"required,gt=0"` // Amount to convert
	Reference      string `json:"reference" binding:"required"`
	Pin            string `json:"pin" binding:"required"`
}

type ManualConversionResponse struct {
	SourceAmount float64 `json:"source_amount"`
	Reference    string  `json:"reference"`
	TargetAmount float64 `json:"target_amount"`
	ExecutedRate float64 `json:"rate"`
	Fees         float64 `json:"fees"`
	NetAmount    float64 `json:"net_amount"`
	Status       string  `json:"status"`
}

// ============================================================
// ERROR TYPES
// ============================================================

type ConversionError struct {
	Code    string
	Message string
	Err     error
}

func (e *ConversionError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

var (
	ErrRuleNotFound        = &ConversionError{Code: "RULE_NOT_FOUND", Message: "Conversion rule not found"}
	ErrDuplicateRule       = &ConversionError{Code: "DUPLICATE_RULE", Message: "Active rule already exists for this currency pair"}
	ErrWalletNotFound      = &ConversionError{Code: "WALLET_NOT_FOUND", Message: "Wallet not found"}
	ErrInsufficientBalance = &ConversionError{Code: "INSUFFICIENT_BALANCE", Message: "Insufficient wallet balance"}
	ErrConversionFailed    = &ConversionError{Code: "CONVERSION_FAILED", Message: "Conversion execution failed"}
)
