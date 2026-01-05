package rewards

import (
	"fmt"
	"strconv"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
)

// ============================================================================
// REWARD CONFIGURATION MODELS
// ============================================================================

// RewardConfiguration represents admin-defined reward earning rules
type RewardConfigurationResponse struct {
	ID                      int64     `json:"id"`
	ConfigName              string    `json:"config_name"`
	RewardRate              string    `json:"reward_rate"` // e.g., 0.01 for 1%
	TransactionType         string    `json:"transaction_type"`
	MinTransactionAmount    string    `json:"min_transaction_amount"`
	MaxPointsPerTransaction string    `json:"max_points_per_transaction"`
	IsActive                bool      `json:"is_active"`
	ValidFrom               time.Time `json:"valid_from"`
	ValidUntil              time.Time `json:"valid_until,omitempty"`
	CreatedBy               int64     `json:"created_by,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func MapRewardConfigToResponse(r *db.RewardConfiguration) *RewardConfigurationResponse {
	return &RewardConfigurationResponse{
		ID:                      r.ID,
		ConfigName:              r.ConfigName,
		RewardRate:              r.RewardRate,
		TransactionType:         r.TransactionType,
		MinTransactionAmount:    r.MinTransactionAmount,
		MaxPointsPerTransaction: r.MaxPointsPerTransaction.String,
		IsActive:                r.IsActive,
		ValidFrom:               r.ValidFrom,
		ValidUntil:              r.ValidUntil.Time,
		CreatedBy:               int64(r.CreatedBy.Int32),
		CreatedAt:               r.CreatedAt,
		UpdatedAt:               r.UpdatedAt,
	}
}

// CreateRewardConfigRequest represents the request to create reward configuration
type CreateRewardConfigRequest struct {
	ConfigName              string     `json:"config_name" binding:"required"`
	RewardRate              float64     `json:"reward_rate" binding:"required,gt=0,lte=1"`
	TransactionType         string     `json:"transaction_type" binding:"required"`
	MinTransactionAmount    string     `json:"min_transaction_amount" binding:"gte=0"`
	MaxPointsPerTransaction *string    `json:"max_points_per_transaction,omitempty"`
	IsActive                bool       `json:"is_active"`
	ValidFrom               *time.Time `json:"valid_from,omitempty"`
	ValidUntil              time.Time  `json:"valid_until,omitempty"`
}

// UpdateRewardConfigRequest represents the request to update reward configuration
type UpdateRewardConfigRequest struct {
	ConfigName              string    `json:"config_name,omitempty"`
	RewardRate              string    `json:"reward_rate,omitempty"`
	TransactionType         string    `json:"transaction_type,omitempty"`
	MinTransactionAmount    string    `json:"min_transaction_amount,omitempty" binding:"omitempty,gte=0"`
	MaxPointsPerTransaction string    `json:"max_points_per_transaction,omitempty"`
	IsActive                *bool      `json:"is_active,omitempty"`
	ValidFrom               time.Time `json:"valid_from,omitempty"`
	ValidUntil              time.Time `json:"valid_until,omitempty"`
}

// ============================================================================
// REWARD TRANSACTION MODELS
// ============================================================================

// RewardTransaction represents a record of earned or redeemed points
type RewardTransactionResponse struct {
	ID                    int64          `json:"id"`
	UserID                int32          `json:"user_id"`
	TransactionID         uuid.UUID          `json:"transaction_id,omitempty"`
	TransactionType       string         `json:"transaction_type"` // "earned" or "redeemed"
	SourceTransactionType string         `json:"source_transaction_type,omitempty"`
	TransactionAmount     string         `json:"transaction_amount,omitempty"`
	PointsAmount          string         `json:"points_amount"`
	NairaValue            string         `json:"naira_value"`
	RewardConfigID        int64          `json:"reward_config_id,omitempty"`
	Description           string         `json:"description,omitempty"`
	Status                string         `json:"status"` // "completed", "pending", "reversed", "failed"
	BalanceAfter          string         `json:"balance_after"`
	Metadata              map[string]any `json:"metadata,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

func MapRewardTransactionToResponse(r *db.RewardTransaction) *RewardTransactionResponse {
	return &RewardTransactionResponse{
		ID:                    r.ID,
		UserID:                int32(r.UserID),
		TransactionID:         r.TransactionID.UUID,
		TransactionType:       r.TransactionType,
		SourceTransactionType: r.SourceTransactionType.String,
		TransactionAmount:     r.TransactionAmount.String,
		PointsAmount:          r.PointsAmount,
		NairaValue:            r.NairaValue,
		RewardConfigID:        r.RewardConfigID.Int64,
		Description:           r.Description.String,
		Status:                r.Status,
		BalanceAfter:          r.BalanceAfter,
		Metadata:              utils.UnmarshalMetadata(r.Metadata),
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}
}

// RewardRedemption represents detailed information about a redemption
type RewardRedemptionResponse struct {
	ID                       int64     `json:"id"`
	RewardTransactionID      int64     `json:"reward_transaction_id"`
	UserID                   int32     `json:"user_id"`
	BillPaymentTransactionID uuid.UUID `json:"bill_payment_transaction_id"`
	PointsRedeemed           string    `json:"points_redeemed"`
	DiscountAmount           string    `json:"discount_amount"`
	OriginalBillAmount       string    `json:"original_bill_amount"`
	FinalAmountPaid          string    `json:"final_amount_paid"`
	ServiceType              string    `json:"service_type,omitempty"`
	ServiceProvider          string    `json:"service_provider,omitempty"`
	RedeemedAt               time.Time `json:"redeemed_at"`
	CreatedAt                time.Time `json:"created_at"`
}

func MapRewardRedemptionToResponse(r *db.RewardRedemption) *RewardRedemptionResponse {
	return &RewardRedemptionResponse{
		ID:                       r.ID,
		RewardTransactionID:      r.RewardTransactionID,
		UserID:                   r.UserID,
		BillPaymentTransactionID: r.BillPaymentTransactionID,
		PointsRedeemed:           r.PointsRedeemed,
		DiscountAmount:           r.DiscountAmount,
		OriginalBillAmount:       r.OriginalBillAmount,
		FinalAmountPaid:          r.FinalAmountPaid,
		ServiceType:              r.ServiceType.String,
		ServiceProvider:          r.ServiceProvider.String,
		RedeemedAt:               r.RedeemedAt,
		CreatedAt:                r.CreatedAt,
	}
}

// ============================================================================
// REQUEST/RESPONSE MODELS
// ============================================================================

// RedeemRewardPointsRequest represents the request to redeem reward points
type RedeemRewardPointsRequest struct {
	PointsToRedeem  string `json:"points_to_redeem" binding:"required,gt=0"`
	UseRewardPoints bool   `json:"use_reward_points"`
}

// RewardBalanceResponse represents user's reward balance information
type RewardBalanceResponse struct {
	CurrentBalance  string `json:"current_balance"`
	TotalEarned     string `json:"total_earned"`
	TotalRedeemed   string `json:"total_redeemed"`
	AvailablePoints string `json:"available_points"` // Same as current_balance, for clarity
}

// RewardSummaryResponse represents comprehensive reward summary
type RewardSummaryResponse struct {
	UserID                  int32     `json:"user_id"`
	CurrentBalance          string    `json:"current_balance"`
	TotalEarned             string    `json:"total_earned"`
	TotalRedeemed           string    `json:"total_redeemed"`
	TotalEarnTransactions   int64     `json:"total_earn_transactions"`
	TotalRedeemTransactions int64     `json:"total_redeem_transactions"`
	LastUpdated             time.Time `json:"last_updated"`
}

// RewardHistoryResponse represents paginated reward transaction history
type RewardHistoryResponse struct {
	Transactions []RewardTransactionItem `json:"transactions"`
	Pagination   PaginationMetadata      `json:"pagination"`
}

// RewardTransactionItem represents a single transaction in history
type RewardTransactionItem struct {
	ID                string    `json:"id"`
	Type              string    `json:"type"` // "earned" or "redeemed"
	Amount            string    `json:"amount"`
	Description       string    `json:"description"`
	Status            string    `json:"status"`
	BalanceAfter      string    `json:"balance_after"`
	SourceTransaction string    `json:"source_transaction,omitempty"`
	Date              time.Time `json:"date"`
}

// PaginationMetadata represents pagination information
type PaginationMetadata struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int   `json:"total_pages"`
	TotalCount int64 `json:"total_count"`
}

// RewardHistoryFilter represents filters for reward history queries
type RewardHistoryFilter struct {
	Type     string     `form:"type" json:"type"` // "earned" or "redeemed"
	DateFrom *time.Time `form:"date_from" json:"date_from"`
	DateTo   *time.Time `form:"date_to" json:"date_to"`
	Page     int        `form:"page" json:"page" binding:"omitempty,gte=1"`
	PageSize int        `form:"page_size" json:"page_size" binding:"omitempty,gte=1,lte=100"`
}

// ============================================================================
// ADMIN ANALYTICS MODELS
// ============================================================================

// RewardStatisticsSummary represents overall reward system statistics
type RewardStatisticsSummary struct {
	OutstandingLiability string `json:"outstanding_liability"`
	TotalPointsIssued    string `json:"total_points_issued"`
	TotalPointsRedeemed  string `json:"total_points_redeemed"`
	UsersWithBalance     int64  `json:"users_with_balance"`
	TotalUsers           int64  `json:"total_users"`
	RedemptionRate       string `json:"redemption_rate"` // Calculated: redeemed/issued
}

// TopRewardUser represents a user in the top earners list
type TopRewardUser struct {
	UserID        int32  `json:"user_id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	RewardBalance string `json:"reward_balance"`
	TotalEarned   string `json:"total_earned"`
	TotalRedeemed string `json:"total_redeemed"`
}

// RewardBreakdownByType represents reward statistics by source type
type RewardBreakdownByType struct {
	SourceType              string `json:"source_type"`
	TransactionCount        int64  `json:"transaction_count"`
	TotalPointsEarned       string `json:"total_points_earned"`
	AvgPointsPerTransaction string `json:"avg_points_per_transaction"`
}

// DailyRewardStatistics represents daily reward trends
type DailyRewardStatistics struct {
	Date             string `json:"date"`
	TransactionType  string `json:"transaction_type"`
	TransactionCount int64  `json:"transaction_count"`
	TotalPoints      string `json:"total_points"`
}

// ============================================================================
// BILL PAYMENT INTEGRATION MODELS
// ============================================================================

// BillPaymentWithRewards extends bill payment request with reward redemption
type BillPaymentWithRewards struct {
	// Original bill payment fields
	Amount          string `json:"amount" binding:"required,gt=0"`
	ServiceType     string `json:"service_type" binding:"required"`
	ServiceProvider string `json:"service_provider" binding:"required"`
	ServiceID       string `json:"service_id" binding:"required"`

	// Reward points fields
	UseRewardPoints bool   `json:"use_reward_points"`
	PointsToUse     string `json:"points_to_use" binding:"omitempty,gte=0"`
}

// BillPaymentResponse includes reward information
type BillPaymentResponseWithRewards struct {
	TransactionID    string `json:"transaction_id"`
	Status           string `json:"status"`
	OriginalAmount   string `json:"original_amount"`
	DiscountApplied  string `json:"discount_applied"`
	FinalAmountPaid  string `json:"final_amount_paid"`
	PointsUsed       string `json:"points_used,omitempty"`
	PointsEarned     string `json:"points_earned,omitempty"`
	NewRewardBalance string `json:"new_reward_balance"`
	Message          string `json:"message"`
}

// RewardNotification represents a notification about reward activity
type RewardNotification struct {
	Type        string    `json:"type"` // "earned" or "redeemed"
	Points      string    `json:"points"`
	NewBalance  string    `json:"new_balance"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
}

// ============================================================================
// CONSTANTS
// ============================================================================

const (
	// Transaction Types
	RewardTransactionTypeEarned   = "earned"
	RewardTransactionTypeRedeemed = "redeemed"

	// Transaction Status
	RewardStatusCompleted = "completed"
	RewardStatusPending   = "pending"
	RewardStatusReversed  = "reversed"
	RewardStatusFailed    = "failed"

	// Source Transaction Types
	SourceTransactionTypeBillPayment = "bill_payment"
	SourceTransactionTypeGiftCard    = "gift_card"

	// Point to Naira conversion
	PointToNairaRatio = 1.0 // 1 point = ₦1

	// Default pagination
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// ============================================================================
// HELPER METHODS
// ============================================================================

// CalculateRewardPoints calculates reward points earned based on
// an integer transaction amount and a floating-point reward rate.
//
// Example:
//
//	points := CalculateRewardPoints(5000, 0.02)
//	fmt.Println(points) // Output: 100
func CalculateRewardPoints(amount int64, rewardRate float64) int64 {
	points := float64(amount) * rewardRate
	return int64(points)
}

// CalculateDiscount calculates discount amount from points (1:1 ratio)
func CalculateDiscount(points float64) float64 {
	return points * PointToNairaRatio
}

// ValidateRewardRedemption validates redemption request
func ValidateRewardRedemption(pointsToRedeem, availableBalance, billAmount float64) error {
	if pointsToRedeem <= 0 {
		return &AppError{
			Code:    "INVALID_REDEMPTION_AMOUNT",
			Message: "Points to redeem must be greater than zero",
			Status:  400,
		}
	}

	if pointsToRedeem > availableBalance {
		return &AppError{
			Code:    "INSUFFICIENT_REWARD_BALANCE",
			Message: "Insufficient reward points balance",
			Status:  400,
			Details: map[string]interface{}{
				"available": availableBalance,
				"requested": pointsToRedeem,
			},
		}
	}

	if pointsToRedeem > billAmount {
		return &AppError{
			Code:    "REDEMPTION_EXCEEDS_BILL_AMOUNT",
			Message: "Cannot redeem more points than bill amount",
			Status:  400,
			Details: map[string]interface{}{
				"bill_amount": billAmount,
				"requested":   pointsToRedeem,
			},
		}
	}

	return nil
}

// FormatRewardMessage formats notification message for earned/redeemed points
func FormatRewardMessage(transactionType string, points int64, serviceInfo string) string {
	switch transactionType {
	case RewardTransactionTypeEarned:
		return fmt.Sprintf("🎉 You earned ₦%d in Reward Points for your %s!", points, serviceInfo)
	case RewardTransactionTypeRedeemed:
		return fmt.Sprintf("You redeemed ₦%d Reward Points for your %s", points, serviceInfo)
	default:
		return fmt.Sprintf("Reward Points transaction: ₦%d", points)
	}
}

// AppError represents application error with details
type AppError struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Status  int                    `json:"status"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	return e.Message
}

func mustParseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return f
}
