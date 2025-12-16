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
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	store               *db.Store
	exchangeRateService *exchangerate.ExchangeRateService
	auditService        *audit.Service
	logger              *logging.Logger
}

func NewService(
	store *db.Store,
	exchangeRateService *exchangerate.ExchangeRateService,
	// notificationService *notifications.Service,
	auditService *audit.Service,
	logger *logging.Logger,
) *Service {
	return &Service{
		store:               store,
		exchangeRateService: exchangeRateService,
		// notificationService: notificationService,
		auditService: auditService,
		logger:       logger,
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
		LevelName:            req.LevelName,
		LevelCode:            req.LevelCode,
		LevelRank:            req.LevelRank,
		MinTransactionVolume: req.MinTransactionVolume,
		IsActive:             isActive,
		CreatedBy:            sql.NullInt64{Int64: user.ID, Valid: true},
		UpdatedBy:            sql.NullInt64{Int64: user.ID, Valid: true},
	}

	if req.MinMonthlyVolume != nil {
		params.MinMonthlyVolume = sql.NullString{String: *req.MinMonthlyVolume, Valid: true}
	}
	if req.MinConversionCount != nil {
		params.MinConversionCount = sql.NullInt32{Int32: *req.MinConversionCount, Valid: true}
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
			"level_name":             req.LevelName,
			"level_code":             req.LevelCode,
			"level_rank":             req.LevelRank,
			"min_transaction_volume": req.MinTransactionVolume,
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
	userCount, err := s.store.CountVIPLevelUsers(ctx, id)
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
		userCount, _ := s.store.CountVIPLevelUsers(ctx, vipLevel.ID)
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
		UpdatedBy: sql.NullInt64{Int64: user.ID, Valid: true},
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
	if req.MinTransactionVolume != nil {
		params.MinTransactionVolume = sql.NullString{String: *req.MinTransactionVolume, Valid: true}
		oldValues["min_transaction_volume"] = existing.MinTransactionVolume
		newValues["min_transaction_volume"] = *req.MinTransactionVolume
	}
	params.Description = sql.NullString{String: *req.Description, Valid: true}
	if req.BenefitsDescription != nil {
		params.BenefitsDescription = sql.NullString{String: *req.BenefitsDescription, Valid: true}
		oldValues["benefits_description"] = existing.BenefitsDescription
		newValues["benefits_description"] = *req.BenefitsDescription
	}
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
	userCount, err := s.store.CountVIPLevelUsers(ctx, id)
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
		UpdatedBy: sql.NullInt64{Int64: user.ID, Valid: true},
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
		CreatedBy:           sql.NullInt64{Int64: user.ID, Valid: true},
		UpdatedBy:           sql.NullInt64{Int64: user.ID, Valid: true},
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

// =====================================================
// RATE CALCULATION WITH ADJUSTMENTS
// =====================================================

// GetAdjustedRateForUser calculates the adjusted rate for a specific user
func (s *Service) GetAdjustedRateForUser(ctx context.Context, userID int64, from, to string, amount decimal.Decimal) (*RateSimulationResponse, error) {
	s.logger.Info(fmt.Sprintf("Calculating adjusted rate for user %d: %s -> %s, amount: %s", userID, from, to, amount))

	// Get base rate from exchange rate service
	baseRate, err := s.exchangeRateService.GetExchangeRate(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get base rate 1: %w", err)
	}

	// Get applicable rule for user
	applicableRule, err := s.store.GetApplicableRulesForUser(ctx, db.GetApplicableRulesForUserParams{
		SourceCurrency:      from,
		TargetCurrency:      to,
		MinConversionAmount: sql.NullString{String: amount.String(), Valid: true},
		UserID:              userID,
	})

	s.logger.Infof("Applicable rule for user %d: %s -> %s, amount: %s", userID, from, to, amount)
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
	targetAmount, fees, netAmount := s.exchangeRateService.CalculateConversionAmount(amount, adjustedRate, feePercentage)

	return &RateSimulationResponse{
		BaseRate:            baseRate.Rate.String(),
		AdjustedRate:        adjustedRate.String(),
		AdjustmentAmount:    adjustmentAmount.String(),
		SourceAmount:        amount.String(),
		TargetAmount:        targetAmount.String(),
		Fees:                fees.String(),
		NetAmount:           netAmount.String(),
		RateProvider:        baseRate.Provider,
		VIPLevelApplied:     vipLevelApplied,
		RuleApplied:         ruleApplied,
		SimulationTimestamp: time.Now(),
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
func (s *Service) recordRateChange(ctx context.Context, baseRate *exchangerate.ExchangeRate, adjustedRate, adjustmentAmount decimal.Decimal, rule *db.GetApplicableRulesForUserRow, userID *int64, conversionID *uuid.UUID) {
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
		params.AppliedToUserID = sql.NullInt64{Int64: *userID, Valid: true}
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
			req.AdjustmentValue.String(),
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

	// Get user transaction metrics
	metrics, err := s.store.GetUserTransactionMetrics(ctx, req.UserID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get user metrics: %v", err))
		metrics.TotalVolume = decimal.Zero.String()
		metrics.ConversionCount = 0
	}

	totalVolume, _ := decimal.NewFromString(metrics.TotalVolume)

	params := db.AssignUserToVIPLevelParams{
		UserID:                 req.UserID,
		VipLevelID:             req.VIPLevelID,
		AssignedBy:             sql.NullInt64{Int64: user.ID, Valid: true},
		AssignmentType:         string(AssignmentTypeManual),
		TotalTransactionVolume: totalVolume.String(),
		TotalConversionCount:   int32(metrics.ConversionCount),
	}

	if req.ExpiresAt != nil {
		params.ExpiresAt = sql.NullTime{Time: *req.ExpiresAt, Valid: true}
	}

	assignment, err := s.store.AssignUserToVIPLevel(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to assign user to VIP level: %w", err)
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
	if user.Email != "" {
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
		EntityType:    "user_vip_assignment",
		EntityID:      assignment.ID.String(),
		Action:        audit.ActionCreate,
		Description:   fmt.Sprintf("Assigned user %d to VIP level %s", req.UserID, vipLevel.LevelName),
		NewValues: map[string]any{
			"user_id":        req.UserID,
			"vip_level_id":   req.VIPLevelID,
			"vip_level_name": vipLevel.LevelName,
		},
		Success: true,
	})

	return toUserVIPAssignmentResponse(&assignment, &assigned_user, &vipLevel), nil
}

// AutoAssignUserVIPLevel automatically assigns VIP level based on user metrics
func (s *Service) AutoAssignUserVIPLevel(ctx context.Context, userID int64) error {
	// Get user transaction metrics
	metrics, err := s.store.GetUserTransactionMetrics(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user metrics: %w", err)
	}

	totalVolume, _ := decimal.NewFromString(metrics.TotalVolume)

	// Find appropriate VIP level
	vipLevel, err := s.store.GetVIPLevelForVolume(ctx, totalVolume.String())
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to find VIP level for volume %s: %v", totalVolume, err))
		// Assign default level
		vipLevel, err = s.store.GetDefaultVIPLevel(ctx)
		if err != nil {
			return fmt.Errorf("failed to get default VIP level: %w", err)
		}
	}

	// Assign user to VIP level
	params := db.AssignUserToVIPLevelParams{
		UserID:                 userID,
		VipLevelID:             vipLevel.ID,
		AssignmentType:         string(AssignmentTypeAutomatic),
		TotalTransactionVolume: totalVolume.String(),
		TotalConversionCount:   int32(metrics.ConversionCount),
	}

	_, err = s.store.AssignUserToVIPLevel(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to auto-assign VIP level: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Auto-assigned user %d to VIP level %s", userID, vipLevel.LevelName))
	return nil
}

// sendVIPAssignmentEmail sends email notification for VIP assignment (placeholder)
func (s *Service) sendVIPAssignmentEmail(ctx context.Context, user *db.User, vipLevel *db.VipLevel) {
	//TODO: Implement email sending logic similar to plunk.go examples
	s.logger.Info(fmt.Sprintf("Sending VIP assignment email to %s for level %s", user.Email, vipLevel.LevelName))
}
