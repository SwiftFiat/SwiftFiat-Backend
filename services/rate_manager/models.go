package ratemanager

import (
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// =====================================================
// VIP LEVEL MODELS
// =====================================================

type VIPLevel struct {
	ID                  uuid.UUID  `json:"id"`
	LevelName           string     `json:"level_name"`
	LevelCode           string     `json:"level_code"`
	LevelRank           int32      `json:"level_rank"`
	MinConversionVolume string     `json:"min_conversion_volume"`
	MinMonthlyVolume    *string    `json:"min_monthly_volume,omitempty"`
	MinConversionCount  *int32     `json:"min_conversion_count,omitempty"`
	Description         *string    `json:"description,omitempty"`
	BenefitsDescription *string    `json:"benefits_description,omitempty"`
	BadgeColor          *string    `json:"badge_color,omitempty"`
	IconURL             *string    `json:"icon_url,omitempty"`
	IsActive            bool       `json:"is_active"`
	IsDefault           bool       `json:"is_default"`
	CreatedBy           *int64     `json:"created_by,omitempty"`
	UpdatedBy           *int64     `json:"updated_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
}

type CreateVIPLevelRequest struct {
	LevelName           string  `json:"level_name" binding:"required,min=2,max=50"`
	LevelCode           string  `json:"level_code" binding:"required,min=2,max=20,uppercase"`
	LevelRank           int32   `json:"level_rank" binding:"required,min=1"`
	MinConversionVolume string  `json:"min_conversion_volume" binding:"required,gte=0"`
	MinMonthlyVolume    *string `json:"min_monthly_volume,omitempty"`
	MinConversionCount  *int32  `json:"min_conversion_count,omitempty"`
	Description         *string `json:"description,omitempty"`
	BenefitsDescription *string `json:"benefits_description,omitempty"`
	BadgeColor          *string `json:"badge_color,omitempty" binding:"omitempty,hexcolor"`
	IconURL             *string `json:"icon_url,omitempty" binding:"omitempty,url"`
	IsActive            *bool   `json:"is_active,omitempty"`
}

type UpdateVIPLevelRequest struct {
	LevelName           *string `json:"level_name,omitempty" binding:"omitempty,min=2,max=50"`
	LevelCode           *string `json:"level_code,omitempty" binding:"omitempty,min=2,max=20,uppercase"`
	LevelRank           *int32  `json:"level_rank,omitempty" binding:"omitempty,min=1"`
	MinConversionVolume *string `json:"min_conversion_volume,omitempty" binding:"omitempty,gte=0"`
	MinMonthlyVolume    *string `json:"min_monthly_volume,omitempty"`
	MinConversionCount  *int32  `json:"min_conversion_count,omitempty"`
	Description         *string `json:"description,omitempty"`
	BenefitsDescription *string `json:"benefits_description,omitempty"`
	BadgeColor          *string `json:"badge_color,omitempty" binding:"omitempty,hexcolor"`
	IconURL             *string `json:"icon_url,omitempty" binding:"omitempty,url"`
	IsActive            *bool   `json:"is_active,omitempty"`
}

type VIPLevelResponse struct {
	ID                  uuid.UUID `json:"id"`
	LevelName           string    `json:"level_name"`
	LevelCode           string    `json:"level_code"`
	LevelRank           int32     `json:"level_rank"`
	MinConversionVolume string    `json:"min_conversion_volume"`
	MinMonthlyVolume    *string   `json:"min_monthly_volume,omitempty"`
	MinConversionCount  *int32    `json:"min_conversion_count,omitempty"`
	Description         *string   `json:"description,omitempty"`
	BenefitsDescription *string   `json:"benefits_description,omitempty"`
	BadgeColor          *string   `json:"badge_color,omitempty"`
	IconURL             *string   `json:"icon_url,omitempty"`
	IsActive            bool      `json:"is_active"`
	IsDefault           bool      `json:"is_default"`
	UserCount           int64     `json:"user_count"`
	ActiveRulesCount    int64     `json:"active_rules_count"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// =====================================================
// RATE ADJUSTMENT RULE MODELS
// =====================================================

type AdjustmentType string

const (
	AdjustmentTypeFixed      AdjustmentType = "fixed"
	AdjustmentTypePercentage AdjustmentType = "percentage"
)

type AdjustmentDirection string

const (
	AdjustmentDirectionAdd      AdjustmentDirection = "add"
	AdjustmentDirectionSubtract AdjustmentDirection = "subtract"
)

type RateAdjustmentRule struct {
	ID                  uuid.UUID           `json:"id"`
	RuleName            string              `json:"rule_name"`
	RuleDescription     *string             `json:"rule_description,omitempty"`
	VIPLevelID          *uuid.UUID          `json:"vip_level_id,omitempty"`
	IsGlobalRule        bool                `json:"is_global_rule"`
	SourceCurrency      string              `json:"source_currency"`
	TargetCurrency      string              `json:"target_currency"`
	AdjustmentType      AdjustmentType      `json:"adjustment_type"`
	AdjustmentValue     string              `json:"adjustment_value"`
	AdjustmentDirection AdjustmentDirection `json:"adjustment_direction"`
	Priority            int32               `json:"priority"`
	MinConversionAmount *string             `json:"min_conversion_amount,omitempty"`
	MaxConversionAmount *string             `json:"max_conversion_amount,omitempty"`
	ValidFrom           *time.Time          `json:"valid_from,omitempty"`
	ValidUntil          *time.Time          `json:"valid_until,omitempty"`
	IsActive            bool                `json:"is_active"`
	CreatedBy           *int64              `json:"created_by,omitempty"`
	UpdatedBy           *int64              `json:"updated_by,omitempty"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
	DeletedAt           *time.Time          `json:"deleted_at,omitempty"`
}

type CreateRateAdjustmentRuleRequest struct {
	RuleName            string              `json:"rule_name" binding:"required,min=3,max=100"`
	RuleDescription     *string             `json:"rule_description,omitempty"`
	VIPLevelID          *uuid.UUID          `json:"vip_level_id,omitempty"`
	IsGlobalRule        bool                `json:"is_global_rule"`
	SourceCurrency      string              `json:"source_currency" binding:"required,oneof=USD NGN USDT USDC"`
	TargetCurrency      string              `json:"target_currency" binding:"required,oneof=USD NGN USDT USDC"`
	AdjustmentType      AdjustmentType      `json:"adjustment_type" binding:"required,oneof=fixed percentage"`
	AdjustmentValue     string              `json:"adjustment_value" binding:"required,gt=0"`
	AdjustmentDirection AdjustmentDirection `json:"adjustment_direction" binding:"required,oneof=add subtract"`
	Priority            *int32              `json:"priority,omitempty"`
	MinConversionAmount *string             `json:"min_conversion_amount,omitempty" binding:"omitempty,gte=0"`
	MaxConversionAmount *string             `json:"max_conversion_amount,omitempty" binding:"omitempty,gte=0"`
	ValidFrom           *time.Time          `json:"valid_from,omitempty"`
	ValidUntil          *time.Time          `json:"valid_until,omitempty"`
	IsActive            *bool               `json:"is_active,omitempty"`
}

type UpdateRateAdjustmentRuleRequest struct {
	RuleName            *string              `json:"rule_name,omitempty" binding:"omitempty,min=3,max=100"`
	RuleDescription     *string              `json:"rule_description,omitempty"`
	AdjustmentType      *AdjustmentType      `json:"adjustment_type,omitempty" binding:"omitempty,oneof=fixed percentage"`
	AdjustmentValue     *string              `json:"adjustment_value,omitempty" binding:"omitempty,gt=0"`
	AdjustmentDirection *AdjustmentDirection `json:"adjustment_direction,omitempty" binding:"omitempty,oneof=add subtract"`
	Priority            *int32               `json:"priority,omitempty"`
	MinConversionAmount *string              `json:"min_conversion_amount,omitempty" binding:"omitempty,gte=0"`
	MaxConversionAmount *string              `json:"max_conversion_amount,omitempty" binding:"omitempty,gte=0"`
	ValidFrom           *time.Time           `json:"valid_from,omitempty"`
	ValidUntil          *time.Time           `json:"valid_until,omitempty"`
	IsActive            *bool                `json:"is_active,omitempty"`
}

type RateAdjustmentRuleResponse struct {
	ID                  uuid.UUID           `json:"id"`
	RuleName            string              `json:"rule_name"`
	RuleDescription     *string             `json:"rule_description,omitempty"`
	VIPLevelID          *uuid.UUID          `json:"vip_level_id,omitempty"`
	VIPLevelName        *string             `json:"vip_level_name,omitempty"`
	VIPLevelCode        *string             `json:"vip_level_code,omitempty"`
	IsGlobalRule        bool                `json:"is_global_rule"`
	SourceCurrency      string              `json:"source_currency"`
	TargetCurrency      string              `json:"target_currency"`
	AdjustmentType      AdjustmentType      `json:"adjustment_type"`
	AdjustmentValue     string              `json:"adjustment_value"`
	AdjustmentDirection AdjustmentDirection `json:"adjustment_direction"`
	Priority            int32               `json:"priority"`
	MinConversionAmount *string             `json:"min_conversion_amount,omitempty"`
	MaxConversionAmount *string             `json:"max_conversion_amount,omitempty"`
	ValidFrom           *time.Time          `json:"valid_from,omitempty"`
	ValidUntil          *time.Time          `json:"valid_until,omitempty"`
	IsActive            bool                `json:"is_active"`
	ApplicationCount    int64               `json:"application_count"`
	TotalImpact         string              `json:"total_impact"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
}

// =====================================================
// RATE SIMULATION & PREVIEW
// =====================================================

type RateSimulationRequest struct {
	SourceCurrency      string               `json:"source_currency" binding:"required,oneof=USD NGN USDT USDC"`
	TargetCurrency      string               `json:"target_currency" binding:"required,oneof=USD NGN USDT USDC"`
	Amount              string               `json:"amount" binding:"required,gt=0"`
	VIPLevelID          *uuid.UUID           `json:"vip_level_id,omitempty"`
	AdjustmentType      *AdjustmentType      `json:"adjustment_type,omitempty" binding:"omitempty,oneof=fixed percentage"`
	AdjustmentValue     *string              `json:"adjustment_value,omitempty" binding:"omitempty,gt=0"`
	AdjustmentDirection *AdjustmentDirection `json:"adjustment_direction,omitempty" binding:"omitempty,oneof=add subtract"`
}

type RateSimulationResponse struct {
	BaseRate            string    `json:"base_rate"`
	AdjustedRate        string    `json:"adjusted_rate"`
	AdjustmentAmount    string    `json:"adjustment_amount"`
	SourceAmount        string    `json:"source_amount"`
	TargetAmount        string    `json:"target_amount"`
	Fees                string    `json:"fees"`
	NetAmount           string    `json:"net_amount"`
	RateProvider        string    `json:"rate_provider"`
	VIPLevelApplied     *string   `json:"vip_level_applied,omitempty"`
	RuleApplied         *string   `json:"rule_applied,omitempty"`
	SimulationTimestamp time.Time `json:"simulation_timestamp"`
}

// =====================================================
// USER VIP ASSIGNMENT MODELS
// =====================================================

type AssignmentType string

const (
	AssignmentTypeAutomatic   AssignmentType = "automatic"
	AssignmentTypeManual      AssignmentType = "manual"
	AssignmentTypePromotional AssignmentType = "promotional"
)

type UserVIPAssignment struct {
	ID                    uuid.UUID       `json:"id"`
	UserID                int64           `json:"user_id"`
	VIPLevelID            uuid.UUID       `json:"vip_level_id"`
	AssignedAt            time.Time       `json:"assigned_at"`
	AssignedBy            *int64          `json:"assigned_by,omitempty"`
	AssignmentType        AssignmentType  `json:"assignment_type"`
	TotalConversionVolume decimal.Decimal `json:"total_conversion_volume"`
	TotalConversionCount  int32           `json:"total_conversion_count"`
	IsActive              bool            `json:"is_active"`
	ExpiresAt             *time.Time      `json:"expires_at,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type AssignVIPLevelRequest struct {
	UserID     uuid.UUID      `json:"user_id" binding:"required"`
	VIPLevelID uuid.UUID  `json:"vip_level_id" binding:"required"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Reason     *string    `json:"reason,omitempty"`
}

type UserVIPAssignmentResponse struct {
	ID                    uuid.UUID      `json:"id"`
	UserID                uuid.UUID          `json:"user_id"`
	UserEmail             string         `json:"user_email"`
	UserName              string         `json:"user_name"`
	VIPLevelID            uuid.UUID      `json:"vip_level_id"`
	VIPLevelName          string         `json:"vip_level_name"`
	VIPLevelCode          string         `json:"vip_level_code"`
	VIPLevelRank          int32          `json:"vip_level_rank"`
	BadgeColor            *string        `json:"badge_color,omitempty"`
	BenefitsDescription   *string        `json:"benefits_description,omitempty"`
	AssignedAt            time.Time      `json:"assigned_at"`
	AssignmentType        AssignmentType `json:"assignment_type"`
	TotalConversionVolume string         `json:"total_conversion_volume"`
	TotalConversionCount  int32          `json:"total_conversion_count"`
	ExpiresAt             *time.Time     `json:"expires_at,omitempty"`
	IsActive              bool           `json:"is_active"`
}

// =====================================================
// RATE CHANGE HISTORY MODELS
// =====================================================

type RateChangeHistory struct {
	ID               uuid.UUID  `json:"id"`
	SourceCurrency   string     `json:"source_currency"`
	TargetCurrency   string     `json:"target_currency"`
	BaseRate         string     `json:"base_rate"`
	AdjustedRate     string     `json:"adjusted_rate"`
	AdjustmentAmount string     `json:"adjustment_amount"`
	RuleID           *uuid.UUID `json:"rule_id,omitempty"`
	RuleName         *string    `json:"rule_name,omitempty"`
	VIPLevelID       *uuid.UUID `json:"vip_level_id,omitempty"`
	VIPLevelName     *string    `json:"vip_level_name,omitempty"`
	RateProvider     *string    `json:"rate_provider,omitempty"`
	AppliedToUserID  *int64     `json:"applied_to_user_id,omitempty"`
	ConversionID     *uuid.UUID `json:"conversion_id,omitempty"`
	ChangeReason     *string    `json:"change_reason,omitempty"`
	ChangedBy        *int64     `json:"changed_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type RateChangeHistoryResponse struct {
	ID                uuid.UUID `json:"id"`
	SourceCurrency    string    `json:"source_currency"`
	TargetCurrency    string    `json:"target_currency"`
	BaseRate          string    `json:"base_rate"`
	AdjustedRate      string    `json:"adjusted_rate"`
	AdjustmentAmount  string    `json:"adjustment_amount"`
	AdjustmentPercent string    `json:"adjustment_percent"`
	RuleName          *string   `json:"rule_name,omitempty"`
	VIPLevelName      *string   `json:"vip_level_name,omitempty"`
	RateProvider      *string   `json:"rate_provider,omitempty"`
	UserEmail         *string   `json:"user_email,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// =====================================================
// ANALYTICS & REPORTING MODELS
// =====================================================

type VIPLevelDistribution struct {
	LevelName   string `json:"level_name"`
	LevelCode   string `json:"level_code"`
	LevelRank   int32  `json:"level_rank"`
	UserCount   int64  `json:"user_count"`
	TotalVolume string `json:"total_volume"`
	Percentage  string `json:"percentage"`
}

type RateAdjustmentImpact struct {
	RuleID              uuid.UUID `json:"rule_id"`
	RuleName            string    `json:"rule_name"`
	TotalAdjustments    int64     `json:"total_adjustments"`
	TotalImpactValue    string    `json:"total_impact_value"`
	AvgImpactValue      string    `json:"avg_impact_value"`
	UniqueUsersAffected int64     `json:"unique_users_affected"`
	StartDate           time.Time `json:"start_date"`
	EndDate             time.Time `json:"end_date"`
}

type RateStatistics struct {
	CurrencyPair    string    `json:"currency_pair"`
	TotalChanges    int64     `json:"total_changes"`
	AvgAdjustment   string    `json:"avg_adjustment"`
	MinBaseRate     string    `json:"min_base_rate"`
	MaxBaseRate     string    `json:"max_base_rate"`
	MinAdjustedRate string    `json:"min_adjusted_rate"`
	MaxAdjustedRate string    `json:"max_adjusted_rate"`
	StartDate       time.Time `json:"start_date"`
	EndDate         time.Time `json:"end_date"`
}

type TopVIPUser struct {
	UserID                int64  `json:"user_id"`
	Email                 string `json:"email"`
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	VIPLevel              string `json:"vip_level"`
	TotalConversionVolume string `json:"total_conversion_volume"`
	TotalConversionCount  int32  `json:"total_conversion_count"`
}

// =====================================================
// ADMIN NOTIFICATIONS
// =====================================================

type NotificationType string

const (
	NotificationTypeRateSpike    NotificationType = "rate_spike"
	NotificationTypeRuleConflict NotificationType = "rule_conflict"
	NotificationTypeVIPUpgrade   NotificationType = "vip_upgrade"
	NotificationTypeVIPDowngrade NotificationType = "vip_downgrade"
	NotificationTypeRuleExpiring NotificationType = "rule_expiring"
	NotificationTypeSystemIssue  NotificationType = "system_issue"
)

type NotificationSeverity string

const (
	SeverityCritical NotificationSeverity = "critical"
	SeverityWarning  NotificationSeverity = "warning"
	SeverityInfo     NotificationSeverity = "info"
)

type RateAdminNotification struct {
	ID                uuid.UUID            `json:"id"`
	NotificationType  NotificationType     `json:"notification_type"`
	Severity          NotificationSeverity `json:"severity"`
	Title             string               `json:"title"`
	Message           string               `json:"message"`
	RelatedEntityType *string              `json:"related_entity_type,omitempty"`
	RelatedEntityID   *string              `json:"related_entity_id,omitempty"`
	Metadata          map[string]any       `json:"metadata,omitempty"`
	IsRead            bool                 `json:"is_read"`
	ReadAt            *time.Time           `json:"read_at,omitempty"`
	ReadBy            *int64               `json:"read_by,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
}

// =====================================================
// ERROR TYPES
// =====================================================

type RateManagerError struct {
	Code    string
	Message string
	Err     error
}

func (e *RateManagerError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

var (
	ErrVIPLevelNotFound         = &RateManagerError{Code: "VIP_LEVEL_NOT_FOUND", Message: "VIP level not found"}
	ErrVIPLevelNameExists       = &RateManagerError{Code: "VIP_LEVEL_NAME_EXISTS", Message: "VIP level name already exists"}
	ErrVIPLevelCodeExists       = &RateManagerError{Code: "VIP_LEVEL_CODE_EXISTS", Message: "VIP level code already exists"}
	ErrVIPLevelRankExists       = &RateManagerError{Code: "VIP_LEVEL_RANK_EXISTS", Message: "VIP level rank already exists"}
	ErrVIPLevelHasUsers         = &RateManagerError{Code: "VIP_LEVEL_HAS_USERS", Message: "Cannot delete VIP level with active users"}
	ErrRuleNotFound             = &RateManagerError{Code: "RULE_NOT_FOUND", Message: "Rate adjustment rule not found"}
	ErrDuplicateGlobalRule      = &RateManagerError{Code: "DUPLICATE_GLOBAL_RULE", Message: "Active global rule already exists for this currency pair"}
	ErrInvalidRuleConfiguration = &RateManagerError{Code: "INVALID_RULE_CONFIG", Message: "Invalid rule configuration"}
	ErrUserAssignmentNotFound   = &RateManagerError{Code: "USER_ASSIGNMENT_NOT_FOUND", Message: "User VIP assignment not found"}
	ErrInvalidCurrencyPair      = &RateManagerError{Code: "INVALID_CURRENCY_PAIR", Message: "Invalid currency pair"}
)

// =====================================================
// PAGINATION
// =====================================================

type PaginationParams struct {
	Page     int32 `json:"page" form:"page" binding:"omitempty,min=1"`
	PageSize int32 `json:"page_size" form:"page_size" binding:"omitempty,min=1,max=100"`
}

func (p *PaginationParams) GetLimit() int32 {
	if p.PageSize == 0 {
		return 20 // Default page size
	}
	return p.PageSize
}

func (p *PaginationParams) GetOffset() int32 {
	if p.Page == 0 {
		p.Page = 1
	}
	return (p.Page - 1) * p.GetLimit()
}

type PaginatedResponse struct {
	Data       any   `json:"data"`
	Page       int32 `json:"page"`
	PageSize   int32 `json:"page_size"`
	TotalCount int64 `json:"total_count,omitempty"`
	TotalPages int32 `json:"total_pages,omitempty"`
}

// =====================================================
// HELPER FUNCTIONS
// =====================================================

func toVIPLevelModel(vipLevel *db.VipLevel) *VIPLevel {
	model := &VIPLevel{
		ID:                  vipLevel.ID,
		LevelName:           vipLevel.LevelName,
		LevelCode:           vipLevel.LevelCode,
		LevelRank:           vipLevel.LevelRank,
		MinConversionVolume: vipLevel.MinConversionVolume,
		IsActive:            vipLevel.IsActive,
		IsDefault:           vipLevel.IsDefault,
		CreatedAt:           vipLevel.CreatedAt,
		UpdatedAt:           vipLevel.UpdatedAt,
	}

	if vipLevel.Description.Valid {
		model.Description = &vipLevel.Description.String
	}
	if vipLevel.BenefitsDescription.Valid {
		model.BenefitsDescription = &vipLevel.BenefitsDescription.String
	}
	if vipLevel.BadgeColor.Valid {
		model.BadgeColor = &vipLevel.BadgeColor.String
	}

	return model
}

func toVIPLevelResponse(vipLevel *db.VipLevel, userCount, rulesCount int64) *VIPLevelResponse {
	resp := &VIPLevelResponse{
		ID:                  vipLevel.ID,
		LevelName:           vipLevel.LevelName,
		LevelCode:           vipLevel.LevelCode,
		LevelRank:           vipLevel.LevelRank,
		MinConversionVolume: vipLevel.MinConversionVolume,
		IsActive:            vipLevel.IsActive,
		IsDefault:           vipLevel.IsDefault,
		UserCount:           userCount,
		ActiveRulesCount:    rulesCount,
		CreatedAt:           vipLevel.CreatedAt,
		UpdatedAt:           vipLevel.UpdatedAt,
	}

	if vipLevel.Description.Valid {
		resp.Description = &vipLevel.Description.String
	}
	if vipLevel.BenefitsDescription.Valid {
		resp.BenefitsDescription = &vipLevel.BenefitsDescription.String
	}
	if vipLevel.BadgeColor.Valid {
		resp.BadgeColor = &vipLevel.BadgeColor.String
	}

	return resp
}

func toRateAdjustmentRuleModel(rule *db.RateAdjustmentRule) *RateAdjustmentRule {
	model := &RateAdjustmentRule{
		ID:                  rule.ID,
		RuleName:            rule.RuleName,
		IsGlobalRule:        rule.IsGlobalRule,
		SourceCurrency:      rule.SourceCurrency,
		TargetCurrency:      rule.TargetCurrency,
		AdjustmentType:      AdjustmentType(rule.AdjustmentType),
		AdjustmentValue:     rule.AdjustmentValue,
		AdjustmentDirection: AdjustmentDirection(rule.AdjustmentDirection),
		Priority:            rule.Priority,
		IsActive:            rule.IsActive,
		CreatedAt:           rule.CreatedAt,
		UpdatedAt:           rule.UpdatedAt,
	}

	if rule.VipLevelID.Valid {
		model.VIPLevelID = &rule.VipLevelID.UUID
	}

	return model
}

func toRateAdjustmentRuleResponse(rule *db.RateAdjustmentRule, appCount int64, totalImpact string) *RateAdjustmentRuleResponse {
	resp := &RateAdjustmentRuleResponse{
		ID:                  rule.ID,
		RuleName:            rule.RuleName,
		IsGlobalRule:        rule.IsGlobalRule,
		SourceCurrency:      rule.SourceCurrency,
		TargetCurrency:      rule.TargetCurrency,
		AdjustmentType:      AdjustmentType(rule.AdjustmentType),
		AdjustmentValue:     rule.AdjustmentValue,
		AdjustmentDirection: AdjustmentDirection(rule.AdjustmentDirection),
		Priority:            rule.Priority,
		IsActive:            rule.IsActive,
		ApplicationCount:    appCount,
		TotalImpact:         totalImpact,
		CreatedAt:           rule.CreatedAt,
		UpdatedAt:           rule.UpdatedAt,
	}

	if rule.VipLevelID.Valid {
		resp.VIPLevelID = &rule.VipLevelID.UUID
	}

	return resp
}

func toUserVIPAssignmentResponse(assignment *db.UserVipAssignment, user *db.User, vipLevel *db.VipLevel) *UserVIPAssignmentResponse {
	return &UserVIPAssignmentResponse{
		ID:                    assignment.ID,
		UserID:                assignment.UserID,
		UserEmail:             user.Email,
		UserName:              fmt.Sprintf("%s %s", user.FirstName.String, user.LastName.String),
		VIPLevelID:            assignment.VipLevelID,
		VIPLevelName:          vipLevel.LevelName,
		VIPLevelCode:          vipLevel.LevelCode,
		VIPLevelRank:          vipLevel.LevelRank,
		AssignedAt:            assignment.AssignedAt,
		AssignmentType:        AssignmentType(assignment.AssignmentType),
		TotalConversionVolume: assignment.TotalConversionVolume,
		IsActive:              assignment.IsActive,
	}
}
