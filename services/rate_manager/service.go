package ratemanager

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	exchangerate "github.com/SwiftFiat/SwiftFiat-Backend/services/exchange_rate"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	store               *db.Store
	exchangeRateService *exchangerate.ExchangeRateService
	auditService        *audit.Service
	logger              *logging.Logger
	push                *service.PushNotificationService
	redis               *redis.RedisService
}

func NewService(
	store *db.Store,
	exchangeRateService *exchangerate.ExchangeRateService,
	// notificationService *notifications.Service,
	auditService *audit.Service,
	logger *logging.Logger,
	push *service.PushNotificationService,
	redis *redis.RedisService,
) *Service {
	return &Service{
		store:               store,
		exchangeRateService: exchangeRateService,
		// notificationService: notificationService,
		auditService: auditService,
		logger:       logger,
		push:         push,
		redis:        redis,
	}
}

func (s *Service) CreateVIPLevel(ctx context.Context, req *CreateVIPLevelRequest, user *db.User) (*VIPLevel, error) {
	s.logger.Info(fmt.Sprintf("Creating VIP level: %s", req.LevelName))

	nameExists, err := s.store.CheckVIPLevelNameExists(ctx, db.CheckVIPLevelNameExistsParams{
		LevelName: req.LevelName,
		ID:        uuid.Nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to check level name: %w", err)
	}

	if nameExists {
		return nil, ErrVIPLevelNameExists
	}

	codeExists, err := s.store.CheckVIPLevelCodeExists(ctx, db.CheckVIPLevelCodeExistsParams{
		LevelCode: req.LevelCode,
		ID:        uuid.Nil,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to check level code: %w", err)
	}

	if codeExists {
		return nil, ErrVIPLevelCodeExists
	}

	rankExists, err := s.store.CheckVIPLevelRankExists(ctx, db.CheckVIPLevelRankExistsParams{
		LevelRank: req.LevelRank,
		ID:        uuid.Nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to check level rank: %w", err)
	}
	if rankExists {
		return nil, ErrVIPLevelRankExists
	}

	// Create VIP level
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	params := db.CreateVIPLevelParams{
		LevelName:           req.LevelName,
		LevelCode:           req.LevelCode,
		LevelRank:           req.LevelRank,
		MinConversionVolume: req.MinConversionVolume,
		IsActive:            isActive,
		CreatedBy:           uuid.NullUUID{UUID: user.ID, Valid: true},
		UpdatedBy:           uuid.NullUUID{UUID: user.ID, Valid: true},
	}

	if req.Description != nil {
		params.Description = sql.NullString{String: *req.Description, Valid: true}
	}
	if req.BenefitsDescription != nil {
		params.BenefitsDescription = sql.NullString{String: *req.BenefitsDescription, Valid: true}
	}
	if req.BadgeColor != nil {
		params.BadgeColor = sql.NullString{String: *req.BadgeColor, Valid: true}
	}
	if req.IconURL != nil {
		params.IconUrl = sql.NullString{String: *req.IconURL, Valid: true}
	}

	vipLevel, err := s.store.CreateVIPLevel(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create VIP level: %w", err)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "vip_level_created",
		Severity:      audit.SeverityInfo,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "vip_level",
		EntityID:      vipLevel.ID.String(),
		Action:        audit.ActionCreate,
		Description:   fmt.Sprintf("Created VIP level: %s", req.LevelName),
		NewValues: map[string]any{
			"level_name":            req.LevelName,
			"level_code":            req.LevelCode,
			"level_rank":            req.LevelRank,
			"min_conversion_volume": req.MinConversionVolume,
		},
		Success: true,
	})

	// TODO: admin notification - check claude

	return toVIPLevelModel(&vipLevel), nil
}

// GetVIPLevel retrieves a VIP level by ID
func (s *Service) GetVIPLevel(ctx context.Context, id uuid.UUID) (*VIPLevelResponse, error) {
	vipLevel, err := s.store.GetVIPLevelByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVIPLevelNotFound
		}
		return nil, fmt.Errorf("failed to get VIP level: %w", err)
	}

	// Get user count
	userCount, err := s.store.CountVIPLevelUsers(ctx, uuid.NullUUID{UUID: id, Valid: true})
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to count VIP level users: %v", err))
		userCount = 0
	}

	// Get active rules count
	rulesCount, err := s.store.CountRateAdjustmentRulesByVIPLevel(ctx, uuid.NullUUID{UUID: id, Valid: true})
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to count VIP level rules: %v", err))
		rulesCount = 0
	}

	return toVIPLevelResponse(&vipLevel, userCount, rulesCount), nil
}

// ListVIPLevels retrieves all VIP levels
func (s *Service) ListVIPLevels(ctx context.Context, activeOnly bool) ([]*VIPLevelResponse, error) {
	var vipLevels []db.VipLevel
	var err error

	if activeOnly {
		vipLevels, err = s.store.ListActiveVIPLevels(ctx)
	} else {
		vipLevels, err = s.store.ListVIPLevels(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list VIP levels: %w", err)
	}

	responses := make([]*VIPLevelResponse, len(vipLevels))
	for i, vipLevel := range vipLevels {
		userCount, _ := s.store.CountVIPLevelUsers(ctx, uuid.NullUUID{UUID: vipLevel.ID, Valid: true})
		rulesCount, _ := s.store.CountRateAdjustmentRulesByVIPLevel(ctx, uuid.NullUUID{UUID: vipLevel.ID, Valid: true})
		responses[i] = toVIPLevelResponse(&vipLevel, userCount, rulesCount)
	}

	return responses, nil
}

// UpdateVIPLevel updates a VIP level
func (s *Service) UpdateVIPLevel(ctx context.Context, id uuid.UUID, req *UpdateVIPLevelRequest, user *db.User) (*VIPLevel, error) {
	s.logger.Info(fmt.Sprintf("Updating VIP level: %s", id))

	// Get existing level for audit
	existing, err := s.store.GetVIPLevelByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVIPLevelNotFound
		}
		return nil, fmt.Errorf("failed to get VIP level: %w", err)
	}

	// Validate uniqueness if name, code, or rank changed
	if req.LevelName != nil {
		nameExists, err := s.store.CheckVIPLevelNameExists(ctx, db.CheckVIPLevelNameExistsParams{
			LevelName: *req.LevelName,
			ID:        id,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check level name: %w", err)
		}
		if nameExists {
			return nil, ErrVIPLevelNameExists
		}
	}

	if req.LevelCode != nil {
		codeExists, err := s.store.CheckVIPLevelCodeExists(ctx, db.CheckVIPLevelCodeExistsParams{
			LevelCode: *req.LevelCode,
			ID:        id,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check level code: %w", err)
		}
		if codeExists {
			return nil, ErrVIPLevelCodeExists
		}
	}

	if req.LevelRank != nil {
		rankExists, err := s.store.CheckVIPLevelRankExists(ctx, db.CheckVIPLevelRankExistsParams{
			LevelRank: *req.LevelRank,
			ID:        id,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check level rank: %w", err)
		}
		if rankExists {
			return nil, ErrVIPLevelRankExists
		}
	}

	// Build update params
	params := db.UpdateVIPLevelParams{
		ID:        id,
		UpdatedBy: uuid.NullUUID{UUID: user.ID, Valid: true},
	}

	oldValues := make(map[string]any)
	newValues := make(map[string]any)

	if req.LevelName != nil {
		params.LevelName = sql.NullString{String: *req.LevelName, Valid: true}
		oldValues["level_name"] = existing.LevelName
		newValues["level_name"] = *req.LevelName
	}
	if req.LevelCode != nil {
		params.LevelCode = sql.NullString{String: *req.LevelCode, Valid: true}
		oldValues["level_code"] = existing.LevelCode
		newValues["level_code"] = *req.LevelCode
	}
	if req.LevelRank != nil {
		params.LevelRank = sql.NullInt32{Int32: *req.LevelRank, Valid: true}
		oldValues["level_rank"] = existing.LevelRank
		newValues["level_rank"] = *req.LevelRank
	}
	if req.MinConversionVolume != nil {
		params.MinConversionVolume = sql.NullString{String: *req.MinConversionVolume, Valid: true}
		oldValues["min_conversion_volume"] = existing.MinConversionVolume
		newValues["min_conversion_volume"] = *req.MinConversionVolume
	}
	params.Description = sql.NullString{String: *req.Description, Valid: true}
	if req.BenefitsDescription != nil {
		params.BenefitsDescription = sql.NullString{String: *req.BenefitsDescription, Valid: true}
		oldValues["benefits_description"] = existing.BenefitsDescription
		newValues["benefits_description"] = *req.BenefitsDescription
	}
	if req.IsActive != nil {
		params.IsActive = sql.NullBool{Bool: *req.IsActive, Valid: true}
		oldValues["is_active"] = existing.IsActive
		newValues["is_active"] = *req.IsActive
	}

	updated, err := s.store.UpdateVIPLevel(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to update VIP level: %w", err)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "vip_level_updated",
		Severity:      audit.SeverityInfo,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "vip_level",
		EntityID:      id.String(),
		Action:        audit.ActionUpdate,
		Description:   fmt.Sprintf("Updated VIP level: %s", existing.LevelName),
		OldValues:     oldValues,
		NewValues:     newValues,
		Success:       true,
	})

	return toVIPLevelModel(&updated), nil
}

// DeleteVIPLevel soft deletes a VIP level
func (s *Service) DeleteVIPLevel(ctx context.Context, id uuid.UUID, user *db.User) error {
	s.logger.Info(fmt.Sprintf("Deleting VIP level: %s", id))

	// Check if level has users
	userCount, err := s.store.CountVIPLevelUsers(ctx, uuid.NullUUID{UUID: id, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to count VIP level users: %w", err)
	}
	if userCount > 0 {
		return ErrVIPLevelHasUsers
	}

	// Get level for audit
	vipLevel, err := s.store.GetVIPLevelByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrVIPLevelNotFound
		}
		return fmt.Errorf("failed to get VIP level: %w", err)
	}

	// Delete
	_, err = s.store.DeleteVIPLevel(ctx, db.DeleteVIPLevelParams{
		ID:        id,
		UpdatedBy: uuid.NullUUID{UUID: user.ID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to delete VIP level: %w", err)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "vip_level_deleted",
		Severity:      audit.SeverityWarning,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "vip_level",
		EntityID:      id.String(),
		Action:        audit.ActionDelete,
		Description:   fmt.Sprintf("Deleted VIP level: %s", vipLevel.LevelName),
		OldValues: map[string]any{
			"level_name": vipLevel.LevelName,
			"level_code": vipLevel.LevelCode,
		},
		Success: true,
	})

	return nil
}

// =====================================================
// RATE ADJUSTMENT RULES MANAGEMENT
// =====================================================

// CreateRateAdjustmentRule creates a new rate adjustment rule
func (s *Service) CreateRateAdjustmentRule(ctx context.Context, req *CreateRateAdjustmentRuleRequest, user *db.User) (*RateAdjustmentRule, error) {
	s.logger.Info(fmt.Sprintf("Creating rate adjustment rule: %s", req.RuleName))

	// Validate currency pair
	if req.SourceCurrency == req.TargetCurrency {
		return nil, ErrInvalidCurrencyPair
	}

	// Validate global rule constraints
	if req.IsGlobalRule && req.VIPLevelID != nil {
		return nil, &RateManagerError{
			Code:    "INVALID_RULE_CONFIG",
			Message: "Global rules cannot be associated with a VIP level",
		}
	}

	if !req.IsGlobalRule && req.VIPLevelID == nil {
		return nil, &RateManagerError{
			Code:    "INVALID_RULE_CONFIG",
			Message: "Non-global rules must be associated with a VIP level",
		}
	}

	// Check for duplicate global rule
	if req.IsGlobalRule {
		existing, err := s.store.GetActiveGlobalRule(ctx, db.GetActiveGlobalRuleParams{
			SourceCurrency: req.SourceCurrency,
			TargetCurrency: req.TargetCurrency,
		})
		if err == nil && existing.ID != uuid.Nil {
			return nil, ErrDuplicateGlobalRule
		}
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	priority := int32(0)
	if req.Priority != nil {
		priority = *req.Priority
	}

	params := db.CreateRateAdjustmentRuleParams{
		RuleName:            req.RuleName,
		IsGlobalRule:        req.IsGlobalRule,
		SourceCurrency:      req.SourceCurrency,
		TargetCurrency:      req.TargetCurrency,
		AdjustmentType:      string(req.AdjustmentType),
		AdjustmentValue:     req.AdjustmentValue,
		AdjustmentDirection: string(req.AdjustmentDirection),
		Priority:            priority,
		IsActive:            isActive,
		CreatedBy:           uuid.NullUUID{UUID: user.ID, Valid: true},
		UpdatedBy:           uuid.NullUUID{UUID: user.ID, Valid: true},
	}

	if req.RuleDescription != nil {
		params.RuleDescription = sql.NullString{String: *req.RuleDescription, Valid: true}
	}
	if req.VIPLevelID != nil {
		params.VipLevelID = uuid.NullUUID{UUID: *req.VIPLevelID, Valid: true}
	}
	if req.MinConversionAmount != nil {
		params.MinConversionAmount = sql.NullString{String: *req.MinConversionAmount, Valid: true}
	}
	if req.MaxConversionAmount != nil {
		params.MaxConversionAmount = sql.NullString{String: *req.MaxConversionAmount, Valid: true}
	}
	if req.ValidFrom != nil {
		params.ValidFrom = sql.NullTime{Time: *req.ValidFrom, Valid: true}
	}
	if req.ValidUntil != nil {
		params.ValidUntil = sql.NullTime{Time: *req.ValidUntil, Valid: true}
	}

	rule, err := s.store.CreateRateAdjustmentRule(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create rate adjustment rule: %w", err)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "rate_rule_created",
		Severity:      audit.SeverityInfo,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "rate_adjustment_rule",
		EntityID:      rule.ID.String(),
		Action:        audit.ActionCreate,
		Description:   fmt.Sprintf("Created rate adjustment rule: %s", req.RuleName),
		NewValues: map[string]any{
			"rule_name":        req.RuleName,
			"is_global_rule":   req.IsGlobalRule,
			"currency_pair":    fmt.Sprintf("%s/%s", req.SourceCurrency, req.TargetCurrency),
			"adjustment_type":  req.AdjustmentType,
			"adjustment_value": req.AdjustmentValue,
		},
		Success: true,
	})

	// Notify admins
	// TODO: admin notification - check claude

	return toRateAdjustmentRuleModel(&rule), nil
}

// GetRateAdjustmentRule retrieves a rate adjustment rule by ID
func (s *Service) GetRateAdjustmentRule(ctx context.Context, id uuid.UUID) (*RateAdjustmentRuleResponse, error) {
	rule, err := s.store.GetRateAdjustmentRuleByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("failed to get rate adjustment rule: %w", err)
	}

	// Get impact statistics
	impact, _ := s.store.GetRateAdjustmentImpact(ctx, db.GetRateAdjustmentImpactParams{
		RuleID:      uuid.NullUUID{UUID: id, Valid: true},
		CreatedAt:   time.Now().AddDate(0, -1, 0), // Last month
		CreatedAt_2: time.Now(),
	})

	return toRateAdjustmentRuleResponse(&rule, impact.TotalAdjustments, fmt.Sprintf("%d", impact.TotalAdjustmentValue)), nil
}

// UpdateRateAdjustmentRule updates an existing rate adjustment rule
func (s *Service) UpdateRateAdjustmentRule(ctx context.Context, id uuid.UUID, req *UpdateRateAdjustmentRuleRequest, user *db.User) (*RateAdjustmentRule, error) {
	s.logger.Info(fmt.Sprintf("Updating rate adjustment rule: %s", id))

	// Get existing rule for audit
	existing, err := s.store.GetRateAdjustmentRuleByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("failed to get rate adjustment rule: %w", err)
	}

	// Build update params
	params := db.UpdateRateAdjustmentRuleParams{
		ID:        id,
		UpdatedBy: uuid.NullUUID{UUID: user.ID, Valid: true},
	}

	oldValues := make(map[string]any)
	newValues := make(map[string]any)

	if req.RuleName != nil {
		params.RuleName = sql.NullString{String: *req.RuleName, Valid: true}
		oldValues["rule_name"] = existing.RuleName
		newValues["rule_name"] = *req.RuleName
	}

	if req.AdjustmentType != nil {
		params.AdjustmentType = sql.NullString{String: string(*req.AdjustmentType), Valid: true}
		oldValues["adjustment_type"] = existing.AdjustmentType
		newValues["adjustment_type"] = string(*req.AdjustmentType)
	}

	if req.AdjustmentValue != nil {
		params.AdjustmentValue = sql.NullString{String: *req.AdjustmentValue, Valid: true}
		oldValues["adjustment_value"] = existing.AdjustmentValue
		newValues["adjustment_value"] = *req.AdjustmentValue
	}

	if req.AdjustmentDirection != nil {
		params.AdjustmentDirection = sql.NullString{String: string(*req.AdjustmentDirection), Valid: true}
		oldValues["adjustment_direction"] = existing.AdjustmentDirection
		newValues["adjustment_direction"] = string(*req.AdjustmentDirection)
	}

	if req.Priority != nil {
		params.Priority = sql.NullInt32{Int32: *req.Priority, Valid: true}
		oldValues["priority"] = existing.Priority
		newValues["priority"] = *req.Priority
	}

	if req.MinConversionAmount != nil {
		params.MinConversionAmount = sql.NullString{String: *req.MinConversionAmount, Valid: true}
		oldValues["min_conversion_amount"] = existing.MinConversionAmount
		newValues["min_conversion_amount"] = *req.MinConversionAmount
	}

	if req.MaxConversionAmount != nil {
		params.MaxConversionAmount = sql.NullString{String: *req.MaxConversionAmount, Valid: true}
		oldValues["max_conversion_amount"] = existing.MaxConversionAmount
		newValues["max_conversion_amount"] = *req.MaxConversionAmount
	}

	if req.ValidFrom != nil {
		params.ValidFrom = sql.NullTime{Time: *req.ValidFrom, Valid: true}
		oldValues["valid_from"] = existing.ValidFrom
		newValues["valid_from"] = *req.ValidFrom
	}

	if req.ValidUntil != nil {
		params.ValidUntil = sql.NullTime{Time: *req.ValidUntil, Valid: true}
		oldValues["valid_until"] = existing.ValidUntil
		newValues["valid_until"] = *req.ValidUntil
	}

	if req.IsActive != nil {
		params.IsActive = sql.NullBool{Bool: *req.IsActive, Valid: true}
		oldValues["is_active"] = existing.IsActive
		newValues["is_active"] = *req.IsActive
	}

	// Update the rule
	updated, err := s.store.UpdateRateAdjustmentRule(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to update rate adjustment rule: %w", err)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "rate_rule_updated",
		Severity:      audit.SeverityInfo,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "rate_adjustment_rule",
		EntityID:      id.String(),
		Action:        audit.ActionUpdate,
		Description:   fmt.Sprintf("Updated rate adjustment rule: %s", existing.RuleName),
		OldValues:     oldValues,
		NewValues:     newValues,
		Success:       true,
	})

	return toRateAdjustmentRuleModel(&updated), nil
}

// DeleteRateAdjustmentRule soft deletes a rate adjustment rule
func (s *Service) DeleteRateAdjustmentRule(ctx context.Context, id uuid.UUID, user *db.User) error {
	s.logger.Info(fmt.Sprintf("Deleting rate adjustment rule: %s", id))

	// Get rule for audit
	rule, err := s.store.GetRateAdjustmentRuleByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrRuleNotFound
		}
		return fmt.Errorf("failed to get rate adjustment rule: %w", err)
	}

	// Delete
	err = s.store.DeleteRateAdjustmentRule(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete rate adjustment rule: %w", err)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "rate_rule_deleted",
		Severity:      audit.SeverityWarning,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "rate_adjustment_rule",
		EntityID:      id.String(),
		Action:        audit.ActionDelete,
		Description:   fmt.Sprintf("Deleted rate adjustment rule: %s", rule.RuleName),
		OldValues: map[string]any{
			"rule_name":        rule.RuleName,
			"adjustment_type":  rule.AdjustmentType,
			"adjustment_value": rule.AdjustmentValue,
		},
		Success: true,
	})

	return nil
}

// ToggleRateAdjustmentRule enables or disables a rate adjustment rule
func (s *Service) ToggleRateAdjustmentRule(ctx context.Context, id uuid.UUID, enabled bool, user *db.User) error {
	s.logger.Info(fmt.Sprintf("Toggling rate adjustment rule: %s to %v", id, enabled))

	// Get existing rule
	existing, err := s.store.GetRateAdjustmentRuleByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrRuleNotFound
		}
		return fmt.Errorf("failed to get rate adjustment rule: %w", err)
	}

	// Update status
	_, err = s.store.ToggleRateAdjustmentRule(ctx, db.ToggleRateAdjustmentRuleParams{
		ID:        id,
		IsActive:  enabled,
		UpdatedBy: uuid.NullUUID{UUID: user.ID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to toggle rate adjustment rule: %w", err)
	}

	// Audit log
	action := "disabled"
	if enabled {
		action = "enabled"
	}

	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "rate_rule_toggled",
		Severity:      audit.SeverityInfo,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "rate_adjustment_rule",
		EntityID:      id.String(),
		Action:        audit.ActionUpdate,
		Description:   fmt.Sprintf("Rate adjustment rule %s: %s", action, existing.RuleName),
		OldValues: map[string]any{
			"is_active": existing.IsActive,
		},
		NewValues: map[string]any{
			"is_active": enabled,
		},
		Success: true,
	})

	return nil
}

// ListRateAdjustmentRules lists all rate adjustment rules with pagination
// func (s *Service) ListRateAdjustmentRules(ctx context.Context, params *PaginationParams, activeOnly bool) (*PaginatedResponse, error) {
// 	s.logger.Info("Listing rate adjustment rules")

// 		rules, err := s.store.ListRateAdjustmentRules(ctx, db.ListRateAdjustmentRulesParams{
// 			Limit: params.GetLimit(),
// 			Offset: params.GetOffset(),
// 		})
// 		if err != nil {
// 			return nil, fmt.Errorf("failed to list rules: %w", err)
// 		}

// 	// 	totalCount, err = s.store.CountRateAdjustmentRules(ctx)
// 	// 	if err != nil {
// 	// 		return nil, fmt.Errorf("failed to count rules: %w", err)
// 	// 	}

// 	// // Convert to response format
// 	// responses := make([]*RateAdjustmentRuleResponse, len(rules))
// 	// for i, rule := range rules {
// 	// 	// Get impact statistics for each rule
// 	// 	impact, _ := s.store.GetRateAdjustmentImpact(ctx, db.GetRateAdjustmentImpactParams{
// 	// 		RuleID:      uuid.NullUUID{UUID: rule.ID, Valid: true},
// 	// 		CreatedAt:   time.Now().AddDate(0, -1, 0), // Last month
// 	// 		CreatedAt_2: time.Now(),
// 	// 	})

// 	// 	responses[i] = toRateAdjustmentRuleResponse(&rule, impact.TotalAdjustments, fmt.Sprintf("%d", impact.TotalAdjustmentValue))
// 	// }

// 	// Calculate total pages
// 	// totalPages := int32(0)
// 	// if params.GetLimit() > 0 {
// 	// 	totalPages = int32((totalCount + int64(params.GetLimit()) - 1) / int64(params.GetLimit()))
// 	// }

// 	// return &PaginatedResponse{
// 	// 	Data:       responses,
// 	// 	Page:       params.Page,
// 	// 	PageSize:   params.GetLimit(),
// 	// 	TotalCount: totalCount,
// 	// 	TotalPages: totalPages,
// 	// }, nil
// }

func (s *Service) GetUserVIPStatus(ctx context.Context, userID uuid.UUID) (*UserVIPStatusResponse, error) {
	s.logger.Info(fmt.Sprintf("Getting VIP status for user: %s", userID))

	// Get user with VIP fields
	userVIP, err := s.store.GetUserWithVIPFields(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			// User not found or has no VIP data, return default
			defaultLevel, err := s.store.GetDefaultVIPLevel(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get default VIP level: %w", err)
			}

			return &UserVIPStatusResponse{
				UserID:            userID,
				VIPLevelID:        defaultLevel.ID,
				VIPLevelName:      defaultLevel.LevelName,
				VIPLevelCode:      defaultLevel.LevelCode,
				VIPLevelRank:      defaultLevel.LevelRank,
				IsActive:          true,
				AssignmentType:    "automatic",
				BadgeColor:        &defaultLevel.BadgeColor.String,
				BenefitsDesc:      &defaultLevel.BenefitsDescription.String,
				HasActiveBenefits: true,
			}, nil
		}
		return nil, fmt.Errorf("failed to get user VIP data: %w", err)
	}

	// Parse volumes
	totalConversionVolume, err := decimal.NewFromString(userVIP.TotalConversionVolume.String)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to parse total conversion volume: %v", err))
		totalConversionVolume = decimal.Zero
	}

	// Determine current VIP level
	var vipLevelID uuid.UUID
	var vipLevelName string
	var vipLevelCode string
	var vipLevelRank int32
	var badgeColor *string
	var benefitsDesc *string

	if userVIP.CurrentVipLevelID.Valid {
		// User has an assigned VIP level
		vipLevel, err := s.store.GetVIPLevelByID(ctx, userVIP.CurrentVipLevelID.UUID)
		if err != nil {
			return nil, fmt.Errorf("failed to get VIP level: %w", err)
		}
		vipLevelID = vipLevel.ID
		vipLevelName = vipLevel.LevelName
		vipLevelCode = vipLevel.LevelCode
		vipLevelRank = vipLevel.LevelRank
		badgeColor = &vipLevel.BadgeColor.String
		benefitsDesc = &vipLevel.BenefitsDescription.String

		// Check if the current VIP level is still appropriate based on conversion volume
		// If not, auto-assign the correct level
		appropriateLevel, err := s.store.GetVIPLevelForVolume(ctx, totalConversionVolume.String())
		if err == nil && appropriateLevel.ID != vipLevel.ID {
			// User's conversion volume qualifies for a different level, update it
			err = s.store.UpdateUserVIPFields(ctx, db.UpdateUserVIPFieldsParams{
				ID:                     userID,
				TotalConversionVolume:  sql.NullString{String: totalConversionVolume.String(), Valid: true},
				TotalTransactionVolume: sql.NullString{String: userVIP.TotalTransactionVolume.String, Valid: true},
				CurrentVipLevelID:      uuid.NullUUID{UUID: appropriateLevel.ID, Valid: true},
			})
			if err != nil {
				s.logger.Error(fmt.Sprintf("Failed to update VIP level for user %d: %v", userID, err))
			} else {
				// Use the new level for the response
				vipLevelID = appropriateLevel.ID
				vipLevelName = appropriateLevel.LevelName
				vipLevelCode = appropriateLevel.LevelCode
				vipLevelRank = appropriateLevel.LevelRank
				badgeColor = &appropriateLevel.BadgeColor.String
				benefitsDesc = &appropriateLevel.BenefitsDescription.String
			}
		}
	} else {
		// User doesn't have a VIP level assigned, determine based on conversion volume
		vipLevel, err := s.store.GetVIPLevelForVolume(ctx, totalConversionVolume.String())
		if err != nil {
			// Fall back to default level
			defaultLevel, err := s.store.GetDefaultVIPLevel(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get default VIP level: %w", err)
			}
			vipLevel = defaultLevel
		}

		// Auto-assign this level to the user
		err = s.store.UpdateUserVIPFields(ctx, db.UpdateUserVIPFieldsParams{
			ID:                     userID,
			TotalConversionVolume:  sql.NullString{String: totalConversionVolume.String(), Valid: true},
			TotalTransactionVolume: sql.NullString{String: userVIP.TotalTransactionVolume.String, Valid: true},
			CurrentVipLevelID:      uuid.NullUUID{UUID: vipLevel.ID, Valid: true},
		})
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to auto-assign VIP level for user %d: %v", userID, err))
		}

		vipLevelID = vipLevel.ID
		vipLevelName = vipLevel.LevelName
		vipLevelCode = vipLevel.LevelCode
		vipLevelRank = vipLevel.LevelRank
		badgeColor = &vipLevel.BadgeColor.String
		benefitsDesc = &vipLevel.BenefitsDescription.String
	}

	// Get active rate rules for this user
	activeRules, err := s.store.GetActiveRulesForUser(ctx, uuid.NullUUID{UUID: vipLevelID, Valid: true})
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get active rules: %v", err))
	}

	// Get next VIP level (if any)
	var nextLevel *VIPLevel
	nextLevelInfo, err := s.store.GetNextVIPLevel(ctx, vipLevelRank)
	if err == nil {
		nextLevel = toVIPLevelModel(&nextLevelInfo)
	}

	// Calculate progress to next level
	var progressPercentage float64
	var volumeToNextLevel string

	if nextLevel != nil {
		nextVolume, _ := decimal.NewFromString(nextLevel.MinConversionVolume)
		remaining := nextVolume.Sub(totalConversionVolume)

		if remaining.IsPositive() {
			volumeToNextLevel = remaining.String()
			progressPercentage = totalConversionVolume.Div(nextVolume).Mul(decimal.NewFromInt(100)).InexactFloat64()
		}
	}

	return &UserVIPStatusResponse{
		UserID:                userID,
		VIPLevelID:            vipLevelID,
		VIPLevelName:          vipLevelName,
		VIPLevelCode:          vipLevelCode,
		VIPLevelRank:          vipLevelRank,
		AssignedAt:            time.Now(), // Since we're using direct fields, use current time
		AssignmentType:        "automatic",
		IsActive:              true,
		BadgeColor:            badgeColor,
		BenefitsDesc:          benefitsDesc,
		TotalConversionVolume: totalConversionVolume.String(),
		ActiveRulesCount:      int64(len(activeRules)),
		NextLevel:             nextLevel,
		ProgressToNextLevel:   progressPercentage,
		VolumeToNextLevel:     volumeToNextLevel,
		HasActiveBenefits:     true,
	}, nil
}

type UserVIPStatusResponse struct {
	UserID                uuid.UUID      `json:"user_id"`
	VIPLevelID            uuid.UUID      `json:"vip_level_id"`
	VIPLevelName          string         `json:"vip_level_name"`
	VIPLevelCode          string         `json:"vip_level_code"`
	VIPLevelRank          int32          `json:"vip_level_rank"`
	AssignedAt            time.Time      `json:"assigned_at,omitempty"`
	AssignmentType        AssignmentType `json:"assignment_type"`
	IsActive              bool           `json:"is_active"`
	ExpiresAt             sql.NullTime   `json:"expires_at,omitempty"`
	BadgeColor            *string        `json:"badge_color,omitempty"`
	BenefitsDesc          *string        `json:"benefits_description,omitempty"`
	TotalConversionVolume string         `json:"total_conversion_volume"`
	TotalConversionCount  int64          `json:"total_conversion_count"`
	MonthlyVolume         string         `json:"monthly_volume"`
	ActiveRulesCount      int64          `json:"active_rules_count"`
	NextLevel             *VIPLevel      `json:"next_level,omitempty"`
	ProgressToNextLevel   float64        `json:"progress_to_next_level"`
	VolumeToNextLevel     string         `json:"volume_to_next_level,omitempty"`
	HasActiveBenefits     bool           `json:"has_active_benefits"`
}

// =====================================================
// RATE SOURCE MANAGEMENT (MANUAL vs EXCHANGE SERVICE)
// =====================================================

// RateSourcePreference represents which rate source to use
type RateSourcePreference string

const (
	RateSourceExchangeService RateSourcePreference = "exchange_service"
	RateSourceManual          RateSourcePreference = "manual"
)

// SetRateSourcePreference sets whether to use manual or exchange service rates for a currency pair
func (s *Service) SetRateSourcePreference(ctx context.Context, currencyPair string, preference string) error {
	// Validate preference
	if preference != string(RateSourceExchangeService) && preference != string(RateSourceManual) {
		return fmt.Errorf("invalid rate source preference: %s", preference)
	}

	// Store in Redis with key: rate_source_pref:{currency_pair}
	// Use 0 expiration for permanent storage
	rateSourceKey := fmt.Sprintf("rate_source_pref:%s", currencyPair)
	s.logger.Info(fmt.Sprintf("Setting rate source preference for %s to %s", currencyPair, preference))

	return s.redis.Set(ctx, rateSourceKey, preference, 0)
}

// GetRateSourcePreference gets the rate source preference for a currency pair
func (s *Service) GetRateSourcePreference(ctx context.Context, currencyPair string) RateSourcePreference {
	// Try to get from Redis
	rateSourceKey := fmt.Sprintf("rate_source_pref:%s", currencyPair)
	preference, err := s.redis.Get(ctx, rateSourceKey)

	if err != nil {
		// Default to exchange service if not found or error
		s.logger.Debugf("No rate source preference found for %s, defaulting to exchange service: %v", currencyPair, err)
		return RateSourceExchangeService
	}

	if preference == string(RateSourceManual) {
		return RateSourceManual
	}

	return RateSourceExchangeService
}

// =====================================================
// RATE CALCULATION WITH ADJUSTMENTS (UPDATED)
// =====================================================

// GetAdjustedRateForUser calculates the adjusted rate for a specific user
func (s *Service) GetAdjustedRateForUser(ctx context.Context, userID uuid.UUID, from, to string, amount string) (*RateSimulationResponse, error) {
	s.logger.Info(fmt.Sprintf("Calculating adjusted rate for user %s: %s to %s, amount: %s", userID, from, to, amount))

	currencyPair := fmt.Sprintf("%s/%s", from, to)
	rateSourcePref := s.GetRateSourcePreference(ctx, currencyPair)

	// Get base rate based on preference
	var baseRate *exchangerate.ExchangeRate
	var err error

	if rateSourcePref == RateSourceManual {
		// Try to get manual rate first
		baseRate, err = s.getManualRate(ctx, from, to)
		if err != nil {
			// Fall back to exchange service if manual rate not available
			s.logger.Warn(fmt.Sprintf("Manual rate not found for %s/%s, falling back to exchange service: %v", from, to, err))
			baseRate, err = s.exchangeRateService.GetExchangeRate(ctx, from, to)
		}
	} else {
		// Use exchange service rate
		baseRate, err = s.exchangeRateService.GetExchangeRate(ctx, from, to)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get base rate: %w", err)
	}

	// Get applicable rule for user
	applicableRule, err := s.store.GetApplicableRulesForUser(ctx, db.GetApplicableRulesForUserParams{
		SourceCurrency:      from,
		TargetCurrency:      to,
		MinConversionAmount: sql.NullString{String: amount, Valid: true},
		UserID:              userID,
	})

	s.logger.Infof("Applicable rule for user %s: %s -> %s, amount: %s", userID, from, to, amount)
	s.logger.Infof("Applicable rule name : %v", applicableRule)

	var adjustedRate decimal.Decimal
	var adjustmentAmount decimal.Decimal
	var vipLevelApplied *string
	var ruleApplied *string

	if err == nil && len(applicableRule) > 0 {
		// Apply the rule
		rule := applicableRule[0]
		adjustedRate, adjustmentAmount = s.applyRateAdjustment(baseRate.Rate, rule.AdjustmentType, rule.AdjustmentValue, rule.AdjustmentDirection)

		if rule.VipLevelName.Valid {
			levelName := rule.VipLevelName.String
			vipLevelApplied = &levelName
		}
		ruleName := rule.RuleName
		ruleApplied = &ruleName

		// Record rate change
		go s.recordRateChange(context.Background(), baseRate, adjustedRate, adjustmentAmount, &rule, &userID, nil)
	} else {
		// No adjustment - use base rate
		adjustedRate = baseRate.Rate
		adjustmentAmount = decimal.Zero
	}

	// Calculate conversion amounts
	feePercentage := s.exchangeRateService.GetFeePercentage(from, to)
	amt, _ := utils.ToDecimal(amount)
	targetAmount, fees, netAmount := s.exchangeRateService.CalculateConversionAmount(amt, adjustedRate, feePercentage)

	var rateSource string
	if rateSourcePref == RateSourceManual {
		rateSource = "manual"
	} else {
		rateSource = baseRate.Provider
	}

	return &RateSimulationResponse{
		BaseRate:            baseRate.Rate.String(),
		AdjustedRate:        adjustedRate.String(),
		AdjustmentAmount:    adjustmentAmount.String(),
		SourceAmount:        amount,
		TargetAmount:        targetAmount.String(),
		Fees:                fees.String(),
		NetAmount:           netAmount.String(),
		RateProvider:        rateSource,
		VIPLevelApplied:     vipLevelApplied,
		RuleApplied:         ruleApplied,
		SimulationTimestamp: time.Now(),
	}, nil
}

// getManualRate retrieves a manually set rate for a currency pair
func (s *Service) getManualRate(ctx context.Context, from, to string) (*exchangerate.ExchangeRate, error) {
	// Query the exchange_rates table for manually set rates with source='manual'
	rate, err := s.store.GetLatestManualExchangeRate(ctx, db.GetLatestManualExchangeRateParams{
		BaseCurrency:  from,
		QuoteCurrency: to,
	})
	if err != nil {
		return nil, fmt.Errorf("manual rate not found for %s/%s: %w", from, to, err)
	}

	rateDecimal, _ := decimal.NewFromString(rate.Rate)

	return &exchangerate.ExchangeRate{
		From:     from,
		To:       to,
		Rate:     rateDecimal,
		Provider: "manual",
	}, nil
}

// applyRateAdjustment applies an adjustment to a base rate
func (s *Service) applyRateAdjustment(baseRate decimal.Decimal, adjustmentType, adjustmentValue, adjustmentDirection string) (adjustedRate, adjustmentAmount decimal.Decimal) {
	value, _ := decimal.NewFromString(adjustmentValue)

	if adjustmentType == "fixed" {
		adjustmentAmount = value
	} else { // percentage
		adjustmentAmount = baseRate.Mul(value).Div(decimal.NewFromInt(100))
	}

	if adjustmentDirection == "add" {
		adjustedRate = baseRate.Add(adjustmentAmount)
	} else {
		adjustedRate = baseRate.Sub(adjustmentAmount)
	}

	return adjustedRate, adjustmentAmount
}

// recordRateChange records a rate change in history
func (s *Service) recordRateChange(ctx context.Context, baseRate *exchangerate.ExchangeRate, adjustedRate, adjustmentAmount decimal.Decimal, rule *db.GetApplicableRulesForUserRow, userID *uuid.UUID, conversionID *uuid.UUID) {
	params := db.RecordRateChangeParams{
		SourceCurrency:   baseRate.From,
		TargetCurrency:   baseRate.To,
		BaseRate:         baseRate.Rate.String(),
		AdjustedRate:     adjustedRate.String(),
		AdjustmentAmount: adjustmentAmount.String(),
		RateProvider:     sql.NullString{String: baseRate.Provider, Valid: true},
	}

	if rule != nil {
		params.RuleID = uuid.NullUUID{UUID: rule.ID, Valid: true}
		params.RuleName = sql.NullString{String: rule.RuleName, Valid: true}
		if rule.VipLevelID.Valid {
			params.VipLevelID = rule.VipLevelID
			params.VipLevelName = rule.VipLevelName
		}
	}

	if userID != nil {
		params.AppliedToUserID = uuid.NullUUID{UUID: *userID, Valid: true}
	}

	if conversionID != nil {
		params.ConversionID = uuid.NullUUID{UUID: *conversionID, Valid: true}
	}

	_, err := s.store.RecordRateChange(ctx, params)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to record rate change: %v", err))
	}
}

// SimulateRateAdjustment simulates a rate adjustment without applying it
func (s *Service) SimulateRateAdjustment(ctx context.Context, req *RateSimulationRequest) (*RateSimulationResponse, error) {
	s.logger.Info(fmt.Sprintf("Simulating rate adjustment: %s -> %s", req.SourceCurrency, req.TargetCurrency))

	// Get base rate
	baseRate, err := s.exchangeRateService.GetExchangeRate(ctx, req.SourceCurrency, req.TargetCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to get base rate 2: %w", err)
	}

	var adjustedRate decimal.Decimal
	var adjustmentAmount decimal.Decimal

	if req.AdjustmentType != nil && req.AdjustmentValue != nil && req.AdjustmentDirection != nil {
		// Apply custom adjustment
		adjustedRate, adjustmentAmount = s.applyRateAdjustment(
			baseRate.Rate,
			string(*req.AdjustmentType),
			*req.AdjustmentValue,
			string(*req.AdjustmentDirection),
		)
	} else {
		adjustedRate = baseRate.Rate
		adjustmentAmount = decimal.Zero
	}

	amountTodecimal, err := utils.ToDecimal(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("cannot convert amount to decimal: %v", err)
	}

	// Calculate conversion amounts
	feePercentage := s.exchangeRateService.GetFeePercentage(req.SourceCurrency, req.TargetCurrency)
	targetAmount, fees, netAmount := s.exchangeRateService.CalculateConversionAmount(amountTodecimal, adjustedRate, feePercentage)

	return &RateSimulationResponse{
		BaseRate:            baseRate.Rate.String(),
		AdjustedRate:        adjustedRate.String(),
		AdjustmentAmount:    adjustmentAmount.String(),
		SourceAmount:        req.Amount,
		TargetAmount:        targetAmount.String(),
		Fees:                fees.String(),
		NetAmount:           netAmount.String(),
		RateProvider:        baseRate.Provider,
		SimulationTimestamp: time.Now(),
	}, nil
}

// =====================================================
// USER VIP ASSIGNMENT
// =====================================================

// AssignUserToVIPLevel assigns a user to a VIP level
func (s *Service) AssignUserToVIPLevel(ctx context.Context, req *AssignVIPLevelRequest, user *db.User) (*UserVIPAssignmentResponse, error) {
	s.logger.Info(fmt.Sprintf("Assigning user %d to VIP level %s", req.UserID, req.VIPLevelID))

	// Verify VIP level exists
	vipLevel, err := s.store.GetVIPLevelByID(ctx, req.VIPLevelID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVIPLevelNotFound
		}
		return nil, fmt.Errorf("failed to get VIP level: %w", err)
	}

	// Get current user VIP data
	userVIP, err := s.store.GetUserWithVIPFields(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user VIP data: %w", err)
	}

	// Parse current volumes
	totalConversionVolume, _ := decimal.NewFromString(userVIP.TotalConversionVolume.String)
	totalTransactionVolume, _ := decimal.NewFromString(userVIP.TotalTransactionVolume.String)

	// Update user VIP fields
	err = s.store.UpdateUserVIPFields(ctx, db.UpdateUserVIPFieldsParams{
		ID:                     req.UserID,
		TotalConversionVolume:  sql.NullString{String: totalConversionVolume.String(), Valid: true},
		TotalTransactionVolume: sql.NullString{String: totalTransactionVolume.String(), Valid: true},
		CurrentVipLevelID:      uuid.NullUUID{UUID: req.VIPLevelID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update user VIP fields: %w", err)
	}

	// Also update user_vip_assignments for history/audit
	assignment, err := s.store.AssignUserToVIPLevel(ctx, db.AssignUserToVIPLevelParams{
		UserID:                req.UserID,
		VipLevelID:            req.VIPLevelID,
		AssignedBy:            uuid.NullUUID{UUID: user.ID, Valid: true},
		AssignmentType:        string(AssignmentTypeManual),
		TotalConversionVolume: totalConversionVolume.String(),
	})
	if err != nil {
		s.logger.Errorf("Failed to update user_vip_assignments: %v", err)
		// We continue as the users table was successfully updated
		// But we need a dummy assignment for the response if it failed
		assignment = db.UserVipAssignment{ID: uuid.New()}
	}

	// Get user for notification
	assigned_user, _ := s.store.GetUserByID(ctx, req.UserID)

	// Send notification to user
	// s.notificationService.NotifyUser(ctx, req.UserID, notifications.NotificationTypeVIPUpgrade,
	// 	fmt.Sprintf("Congratulations! You've been assigned to %s", vipLevel.LevelName),
	// 	fmt.Sprintf("You now have access to %s benefits", vipLevel.LevelName),
	// 	map[string]any{
	// 		"vip_level_id":   vipLevel.ID,
	// 		"vip_level_name": vipLevel.LevelName,
	// 	},
	// )

	// Send email notification
	if assigned_user.Email != "" {
		go s.sendVIPAssignmentEmail(context.Background(), &assigned_user, &vipLevel)
	}

	// Audit log
	s.auditService.Log(&audit.LogEntry{
		EventCategory: audit.CategoryRateManager,
		EventType:     "vip_assignment",
		Severity:      audit.SeverityInfo,
		ActorType:     user.Role,
		ActorID:       &user.ID,
		ActorEmail:    &user.Email,
		EntityType:    "user",
		EntityID:      req.UserID.String(),
		Action:        audit.ActionUpdate,
		Description:   fmt.Sprintf("Assigned user %s to VIP level %s", req.UserID, vipLevel.LevelName),
		NewValues: map[string]any{
			"user_id":              req.UserID,
			"current_vip_level_id": req.VIPLevelID,
			"vip_level_name":       vipLevel.LevelName,
		},
		Success: true,
	})

	// Create a mock assignment response since we're not using the assignments table anymore
	return &UserVIPAssignmentResponse{
		ID:                    assignment.ID,
		UserID:                req.UserID,
		VIPLevelID:            req.VIPLevelID,
		VIPLevelName:          vipLevel.LevelName,
		VIPLevelCode:          vipLevel.LevelCode,
		VIPLevelRank:          vipLevel.LevelRank,
		AssignedAt:            time.Now(),
		AssignmentType:        AssignmentTypeManual,
		IsActive:              true,
		TotalConversionVolume: totalConversionVolume.String(),
		UserEmail:             assigned_user.Email,
	}, nil
}

// AutoAssignUserVIPLevel automatically assigns VIP level based on user metrics
func (s *Service) AutoAssignUserVIPLevel(ctx context.Context, userID uuid.UUID) error {
	// Get current user VIP data
	userVIP, err := s.store.GetUserWithVIPFields(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user VIP data: %w", err)
	}

	totalConversionVolume, _ := decimal.NewFromString(userVIP.TotalConversionVolume.String)

	// Find appropriate VIP level
	vipLevel, err := s.store.GetVIPLevelForVolume(ctx, totalConversionVolume.String())
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to find VIP level for volume %s: %v", totalConversionVolume, err))
		// Assign default level
		vipLevel, err = s.store.GetDefaultVIPLevel(ctx)
		if err != nil {
			return fmt.Errorf("failed to get default VIP level: %w", err)
		}
	}

	// Update user VIP fields
	err = s.store.UpdateUserVIPFields(ctx, db.UpdateUserVIPFieldsParams{
		ID:                     userID,
		TotalConversionVolume:  sql.NullString{String: totalConversionVolume.String(), Valid: true},
		TotalTransactionVolume: userVIP.TotalTransactionVolume,
		CurrentVipLevelID:      uuid.NullUUID{UUID: vipLevel.ID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to auto-assign VIP level: %w", err)
	}

	// Also update user_vip_assignments for history/audit
	_, err = s.store.AssignUserToVIPLevel(ctx, db.AssignUserToVIPLevelParams{
		UserID:                userID,
		VipLevelID:            vipLevel.ID,
		AssignedBy:            uuid.NullUUID{Valid: false},
		AssignmentType:        string(AssignmentTypeAutomatic),
		TotalConversionVolume: totalConversionVolume.String(),
	})
	if err != nil {
		s.logger.Errorf("Failed to update user_vip_assignments during auto-assignment: %v", err)
	}

	s.logger.Info(fmt.Sprintf("Auto-assigned user %s to VIP level %s", userID, vipLevel.LevelName))
	return nil
}

// sendVIPAssignmentEmail sends email notification for VIP assignment (placeholder)
func (s *Service) sendVIPAssignmentEmail(ctx context.Context, user *db.User, vipLevel *db.VipLevel) {
	//TODO: Implement email sending logic similar to plunk.go examples
	s.push.SendPushNotification(ctx, user.ID, "VIP Level Assignment", fmt.Sprintf("You have been assigned to VIP level %s", vipLevel.LevelName))
	s.logger.Info(fmt.Sprintf("Sending VIP assignment email to %s for level %s", user.Email, vipLevel.LevelName))
}

// IncrementUserConversionVolume increments the user's total conversion volume
func (s *Service) IncrementUserConversionVolume(ctx context.Context, userID uuid.UUID, amount decimal.Decimal) error {
	// Update user VIP fields atomically
	err := s.store.IncrementUserConversionVolume(ctx, db.IncrementUserConversionVolumeParams{
		UserID: userID,
		Amount: amount.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to increment user conversion volume: %w", err)
	}

	// Check if user should be auto-assigned to a higher VIP level
	go func() {
		ctx := context.Background()
		if err := s.AutoAssignUserVIPLevel(ctx, userID); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to auto-assign VIP level after volume increment: %v", err))
		}
	}()

	return nil
}
