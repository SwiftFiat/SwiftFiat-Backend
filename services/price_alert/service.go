package pricealert

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	exchangerate "github.com/SwiftFiat/SwiftFiat-Backend/services/exchange_rate"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// AlertCondition represents the comparison operator for price alerts
type AlertCondition string

const (
	ConditionAbove       AlertCondition = "above"
	ConditionBelow       AlertCondition = "below"
	ConditionEquals      AlertCondition = "equals"
	ConditionPercentUp   AlertCondition = "percent_up"   // Price increases by X%
	ConditionPercentDown AlertCondition = "percent_down" // Price decreases by X%
	ConditionRange       AlertCondition = "range"        // Price enters a specific range
	ConditionBreakout    AlertCondition = "breakout"     // Price breaks resistance/support
)

// AlertType defines the behavior of the alert
type AlertType string

const (
	AlertTypeOneTime   AlertType = "one_time"  // Trigger once and deactivate
	AlertTypeRecurring AlertType = "recurring" // Trigger every time condition is met
	AlertTypeTrailing  AlertType = "trailing"  // Dynamically adjust trigger based on price movement
)

// AlertPriority determines notification urgency
type AlertPriority string

const (
	PriorityLow      AlertPriority = "low"
	PriorityMedium   AlertPriority = "medium"
	PriorityHigh     AlertPriority = "high"
	PriorityCritical AlertPriority = "critical"
)

// PriceAlertService handles custom price alerts with advanced features
type PriceAlertService struct {
	store               *db.Store
	logger              *logging.Logger
	exchangeRateService *exchangerate.ExchangeRateService
	notificationService *service.Notification
	pushService         *service.PushNotificationService
	// taskScheduler       *tasks.TaskScheduler
	checkInterval time.Duration
}

// PriceAlert represents a user-configured price alert
type PriceAlert struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	SourceCurrency   string
	TargetCurrency   string
	AlertCondition   AlertCondition
	AlertType        AlertType
	Priority         AlertPriority
	TargetRate       *decimal.Decimal
	PercentageChange *decimal.Decimal
	RangeMin         *decimal.Decimal
	RangeMax         *decimal.Decimal
	BaselineRate     *decimal.Decimal // For percentage and trailing alerts
	TrailingDistance *decimal.Decimal // For trailing alerts (percentage)
	MaxTrailingRate  *decimal.Decimal // Highest rate seen (for trailing down)
	MinTrailingRate  *decimal.Decimal // Lowest rate seen (for trailing up)
	Description      *string
	Label            *string
	IsActive         bool
	TriggeredCount   int
	LastTriggeredAt  *time.Time
	LastCheckedAt    *time.Time
	ExpiresAt        *time.Time
	NotifyPush       bool
	NotifyInApp      bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CreateAlertRequest encapsulates parameters for creating a price alert
type CreateAlertRequest struct {
	SourceCurrency   string           `json:"source_currency" binding:"required"`
	TargetCurrency   string           `json:"target_currency" binding:"required"`
	AlertCondition   AlertCondition   `json:"alert_condition" binding:"required"`
	AlertType        AlertType        `json:"alert_type" binding:"required"`
	Priority         AlertPriority    `json:"priority"`
	TargetRate       *decimal.Decimal `json:"target_rate"`
	PercentageChange *decimal.Decimal `json:"percentage_change"`
	RangeMin         *decimal.Decimal `json:"range_min"`
	RangeMax         *decimal.Decimal `json:"range_max"`
	TrailingDistance *decimal.Decimal `json:"trailing_distance"` // Percentage (e.g., 5 for 5%)
	Description      *string          `json:"description"`
	Label            *string          `json:"label"`
	ExpiresAt        *time.Time       `json:"expires_at"`
	NotifyPush       *bool            `json:"notify_push"`
	NotifyInApp      *bool            `json:"notify_in_app"`
}

// AlertTriggerEvent contains information about a triggered alert
type AlertTriggerEvent struct {
	Alert         *PriceAlert
	CurrentRate   decimal.Decimal
	PreviousRate  *decimal.Decimal
	ChangePercent *decimal.Decimal
	TriggeredAt   time.Time
}

func NewPriceAlertService(
	store *db.Store,
	logger *logging.Logger,
	exchangeRateService *exchangerate.ExchangeRateService,
	notificationService *service.Notification,
	pushService *service.PushNotificationService,
	// taskScheduler *tasks.TaskScheduler,
	checkInterval time.Duration,
) *PriceAlertService {
	if checkInterval == 0 {
		checkInterval = 30 * time.Second // Default: check every 30 seconds for price alerts
	}
	return &PriceAlertService{
		store:               store,
		logger:              logger,
		exchangeRateService: exchangeRateService,
		notificationService: notificationService,
		pushService:         pushService,
		// taskScheduler:       taskScheduler,
		checkInterval: checkInterval,
	}
}

// CreateAlert creates a new price alert with comprehensive validation
func (s *PriceAlertService) CreateAlert(ctx context.Context, userID uuid.UUID, req *CreateAlertRequest) (*PriceAlert, error) {
	s.logger.Info(fmt.Sprintf("Creating price alert for user %d: %s/%s", userID, req.SourceCurrency, req.TargetCurrency))

	//TODO: Validate currency pair
	// if err := s.exchangeRateService.ValidateCurrencyPair(req.SourceCurrency, req.TargetCurrency); err != nil {
	// 	return nil, exchangerate.ErrInvalidCurrencyPair
	// }

	kyc, err := s.store.Queries.GetKYCByUserID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier_1" {
		go s.pushService.SendPushNotification(ctx, userID, "Verification required.", "This feature requires Tier 2 verification. Complete identity verification to continue")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_2")
	}

	alerts, err := s.GetUserAlerts(ctx, userID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user alerts: %w", err)
	}

	if len(alerts) == 5 {
		return nil, fmt.Errorf("alert limit reached: maximum 5 active alerts allowed")
	}

	// Validate alert configuration based on condition
	if err := s.validateAlertConfig(req); err != nil {
		return nil, err
	}

	// Get current rate for baseline (especially important for percentage and trailing alerts)
	currentRate, err := s.exchangeRateService.GetExchangeRate(ctx, req.SourceCurrency, req.TargetCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to get current exchange rate: %w", err)
	}

	// baselineRate, err := utils.ToDecimal(currentRate.Rate)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to parse current rate: %w", err)
	// }

	// Set defaults
	notifyPush := req.NotifyPush != nil && *req.NotifyPush
	notifyInApp := true
	if req.NotifyInApp != nil {
		notifyInApp = *req.NotifyInApp
	}

	priority := req.Priority
	if priority == "" {
		priority = PriorityMedium
	}

	// Initialize trailing alert baselines
	var maxTrailingRate, minTrailingRate *decimal.Decimal
	if req.AlertType == AlertTypeTrailing {
		maxTrailingRate = &currentRate.Rate
		minTrailingRate = &currentRate.Rate
	}

	// Create the alert in database
	params := db.CreatePriceAlertParams{
		UserID:           userID,
		SourceCurrency:   req.SourceCurrency,
		TargetCurrency:   req.TargetCurrency,
		AlertCondition:   string(req.AlertCondition),
		AlertType:        string(req.AlertType),
		Priority:         string(priority),
		TargetRate:       s.decimalToNullString(req.TargetRate),
		PercentageChange: s.decimalToNullString(req.PercentageChange),
		RangeMin:         s.decimalToNullString(req.RangeMin),
		RangeMax:         s.decimalToNullString(req.RangeMax),
		BaselineRate:     s.decimalToNullString(&currentRate.Rate),
		TrailingDistance: s.decimalToNullString(req.TrailingDistance),
		MaxTrailingRate:  s.decimalToNullString(maxTrailingRate),
		MinTrailingRate:  s.decimalToNullString(minTrailingRate),
		Description:      s.stringToNullString(req.Description),
		Label:            s.stringToNullString(req.Label),
		ExpiresAt:        s.timeToNullTime(req.ExpiresAt),
		NotifyPush:       notifyPush,
		NotifyInApp:      notifyInApp,
	}

	alert, err := s.store.CreatePriceAlert(ctx, params)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create price alert: %v", err))
		return nil, fmt.Errorf("failed to create price alert: %w", err)
	}

	return s.dbAlertToModel(&alert), nil
}

// validateAlertConfig validates alert configuration based on condition type
func (s *PriceAlertService) validateAlertConfig(req *CreateAlertRequest) error {
	switch req.AlertCondition {
	case ConditionAbove, ConditionBelow, ConditionEquals:
		if req.TargetRate == nil || req.TargetRate.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("target_rate is required and must be positive for %s condition", req.AlertCondition)
		}

	case ConditionPercentUp, ConditionPercentDown:
		if req.PercentageChange == nil || req.PercentageChange.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("percentage_change is required and must be positive for %s condition", req.AlertCondition)
		}

	case ConditionRange:
		if req.RangeMin == nil || req.RangeMax == nil {
			return fmt.Errorf("both range_min and range_max are required for range condition")
		}
		if req.RangeMin.GreaterThanOrEqual(*req.RangeMax) {
			return fmt.Errorf("range_min must be less than range_max")
		}

	case ConditionBreakout:
		if req.TargetRate == nil {
			return fmt.Errorf("target_rate is required for breakout condition")
		}
	}

	// Validate trailing alert
	if req.AlertType == AlertTypeTrailing {
		if req.TrailingDistance == nil || req.TrailingDistance.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("trailing_distance is required and must be positive for trailing alerts")
		}
		// Trailing alerts work with specific conditions
		if req.AlertCondition != ConditionAbove && req.AlertCondition != ConditionBelow {
			return fmt.Errorf("trailing alerts only support 'above' and 'below' conditions")
		}
	}

	return nil
}

// CheckAlerts processes all active alerts and triggers notifications
func (s *PriceAlertService) CheckAlerts(ctx context.Context) error {
	s.logger.Debug("Checking active price alerts...")

	// Get all active alerts grouped by currency pair to optimize rate fetching
	alerts, err := s.store.GetActivePriceAlerts(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to fetch active alerts: %v", err))
		return err
	}

	if len(alerts) == 0 {
		s.logger.Debug("No active price alerts to check")
		return nil
	}

	s.logger.Info(fmt.Sprintf("Checking %d active price alerts", len(alerts)))

	// Group alerts by currency pair to minimize API calls
	alertsByCurrencyPair := make(map[string][]*PriceAlert)
	for _, alert := range alerts {
		model := s.dbAlertToModel(&alert)
		key := fmt.Sprintf("%s/%s", model.SourceCurrency, model.TargetCurrency)
		alertsByCurrencyPair[key] = append(alertsByCurrencyPair[key], model)
	}

	// Process each currency pair
	for pair, pairAlerts := range alertsByCurrencyPair {
		if err := s.checkCurrencyPairAlerts(ctx, pairAlerts); err != nil {
			s.logger.Error(fmt.Sprintf("Error checking alerts for %s: %v", pair, err))
			// Continue processing other pairs even if one fails
		}
	}

	return nil
}

// checkCurrencyPairAlerts checks all alerts for a specific currency pair
func (s *PriceAlertService) checkCurrencyPairAlerts(ctx context.Context, alerts []*PriceAlert) error {
	if len(alerts) == 0 {
		return nil
	}

	// All alerts in this group have the same currency pair
	sourceCurrency := alerts[0].SourceCurrency
	targetCurrency := alerts[0].TargetCurrency

	// Fetch current rate once for all alerts in this pair
	currentRate, err := s.exchangeRateService.GetExchangeRate(ctx, sourceCurrency, targetCurrency)
	if err != nil {
		return fmt.Errorf("failed to get exchange rate for %s/%s: %w", sourceCurrency, targetCurrency, err)
	}

	// currentRate, err := utils.ToDecimal(rateInfo.Rate)
	// if err != nil {
	// 	return fmt.Errorf("failed to parse rate: %w", err)
	// }

	// Check each alert
	for _, alert := range alerts {
		if err := s.evaluateAlert(ctx, alert, currentRate.Rate); err != nil {
			s.logger.Error(fmt.Sprintf("Error evaluating alert %s: %v", alert.ID, err))
			// Continue processing other alerts
		}
	}

	return nil
}

// evaluateAlert checks if an alert condition is met and triggers notification
func (s *PriceAlertService) evaluateAlert(ctx context.Context, alert *PriceAlert, currentRate decimal.Decimal) error {
	// Check if alert has expired
	if alert.ExpiresAt != nil && time.Now().After(*alert.ExpiresAt) {
		s.logger.Info(fmt.Sprintf("Alert %s has expired, deactivating", alert.ID))
		return s.DeactivateAlert(ctx, alert.ID, alert.UserID)
	}

	// Update trailing alert tracking
	if alert.AlertType == AlertTypeTrailing {
		s.updateTrailingAlert(ctx, alert, currentRate)
	}

	// Evaluate condition
	triggered, message := s.evaluateCondition(alert, currentRate)
	if !triggered {
		// Update last checked time
		_ = s.store.UpdateAlertLastChecked(ctx, alert.ID)
		return nil
	}

	// Alert is triggered!
	s.logger.Info(fmt.Sprintf("Alert %s triggered: %s", alert.ID, message))

	// Create trigger event
	event := &AlertTriggerEvent{
		Alert:       alert,
		CurrentRate: currentRate,
		TriggeredAt: time.Now(),
	}

	// Calculate change percentage if baseline exists
	if alert.BaselineRate != nil {
		changePercent := currentRate.Sub(*alert.BaselineRate).
			Div(*alert.BaselineRate).
			Mul(decimal.NewFromInt(100))
		event.ChangePercent = &changePercent
		event.PreviousRate = alert.BaselineRate
	}

	// Send notifications
	if err := s.sendAlertNotifications(ctx, event, message); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to send notifications for alert %s: %v", alert.ID, err))
	}

	// Update alert state
	return s.handleAlertTrigger(ctx, alert, currentRate)
}

// evaluateCondition checks if alert condition is met
func (s *PriceAlertService) evaluateCondition(alert *PriceAlert, currentRate decimal.Decimal) (bool, string) {
	switch AlertCondition(alert.AlertCondition) {
	case ConditionAbove:
		if alert.TargetRate != nil && currentRate.GreaterThan(*alert.TargetRate) {
			return true, fmt.Sprintf("Price (%s) is above target (%s)",
				currentRate.StringFixed(6), alert.TargetRate.StringFixed(6))
		}

	case ConditionBelow:
		if alert.TargetRate != nil && currentRate.LessThan(*alert.TargetRate) {
			return true, fmt.Sprintf("Price (%s) is below target (%s)",
				currentRate.StringFixed(6), alert.TargetRate.StringFixed(6))
		}

	case ConditionEquals:
		if alert.TargetRate != nil {
			// Use a small tolerance for equality (0.01%)
			tolerance := alert.TargetRate.Mul(decimal.NewFromFloat(0.0001))
			diff := currentRate.Sub(*alert.TargetRate).Abs()
			if diff.LessThanOrEqual(tolerance) {
				return true, fmt.Sprintf("Price (%s) equals target (%s)",
					currentRate.StringFixed(6), alert.TargetRate.StringFixed(6))
			}
		}

	case ConditionPercentUp:
		if alert.BaselineRate != nil && alert.PercentageChange != nil {
			changePercent := currentRate.Sub(*alert.BaselineRate).
				Div(*alert.BaselineRate).
				Mul(decimal.NewFromInt(100))
			if changePercent.GreaterThanOrEqual(*alert.PercentageChange) {
				return true, fmt.Sprintf("Price increased by %.2f%% (target: %.2f%%)",
					changePercent.InexactFloat64(), alert.PercentageChange.InexactFloat64())
			}
		}

	case ConditionPercentDown:
		if alert.BaselineRate != nil && alert.PercentageChange != nil {
			changePercent := alert.BaselineRate.Sub(currentRate).
				Div(*alert.BaselineRate).
				Mul(decimal.NewFromInt(100))
			if changePercent.GreaterThanOrEqual(*alert.PercentageChange) {
				return true, fmt.Sprintf("Price decreased by %.2f%% (target: %.2f%%)",
					changePercent.InexactFloat64(), alert.PercentageChange.InexactFloat64())
			}
		}

	case ConditionRange:
		if alert.RangeMin != nil && alert.RangeMax != nil {
			if currentRate.GreaterThanOrEqual(*alert.RangeMin) && currentRate.LessThanOrEqual(*alert.RangeMax) {
				return true, fmt.Sprintf("Price (%s) is within range [%s - %s]",
					currentRate.StringFixed(6), alert.RangeMin.StringFixed(6), alert.RangeMax.StringFixed(6))
			}
		}

	case ConditionBreakout:
		if alert.TargetRate != nil && alert.BaselineRate != nil {
			// Check if price crossed the target (breakout)
			wasBelow := alert.BaselineRate.LessThan(*alert.TargetRate)
			isAbove := currentRate.GreaterThanOrEqual(*alert.TargetRate)
			if wasBelow && isAbove {
				return true, fmt.Sprintf("Price broke above resistance at %s", alert.TargetRate.StringFixed(6))
			}

			wasAbove := alert.BaselineRate.GreaterThan(*alert.TargetRate)
			isBelow := currentRate.LessThanOrEqual(*alert.TargetRate)
			if wasAbove && isBelow {
				return true, fmt.Sprintf("Price broke below support at %s", alert.TargetRate.StringFixed(6))
			}
		}
	}

	return false, ""
}

// updateTrailingAlert updates trailing alert state based on price movement
func (s *PriceAlertService) updateTrailingAlert(ctx context.Context, alert *PriceAlert, currentRate decimal.Decimal) {
	if alert.TrailingDistance == nil {
		return
	}

	needsUpdate := false
	trailingPercent := alert.TrailingDistance.Div(decimal.NewFromInt(100))

	switch AlertCondition(alert.AlertCondition) {
	case ConditionAbove:
		// Trailing stop-loss: track highest price, trigger if drops by X%
		if alert.MaxTrailingRate == nil || currentRate.GreaterThan(*alert.MaxTrailingRate) {
			alert.MaxTrailingRate = &currentRate
			// Update trigger target
			newTarget := currentRate.Mul(decimal.NewFromInt(1).Sub(trailingPercent))
			alert.TargetRate = &newTarget
			needsUpdate = true
		}

	case ConditionBelow:
		// Trailing buy: track lowest price, trigger if rises by X%
		if alert.MinTrailingRate == nil || currentRate.LessThan(*alert.MinTrailingRate) {
			alert.MinTrailingRate = &currentRate
			// Update trigger target
			newTarget := currentRate.Mul(decimal.NewFromInt(1).Add(trailingPercent))
			alert.TargetRate = &newTarget
			needsUpdate = true
		}
	}

	if needsUpdate {
		// Update in database
		_ = s.store.UpdateTrailingAlert(ctx, db.UpdateTrailingAlertParams{
			ID:              alert.ID,
			MaxTrailingRate: s.decimalToNullString(alert.MaxTrailingRate),
			MinTrailingRate: s.decimalToNullString(alert.MinTrailingRate),
			TargetRate:      s.decimalToNullString(alert.TargetRate),
		})
	}
}

// sendAlertNotifications sends push and in-app notifications
func (s *PriceAlertService) sendAlertNotifications(ctx context.Context, event *AlertTriggerEvent, message string) error {
	alert := event.Alert

	// Prepare notification content
	title := s.formatAlertTitle(alert)
	body := s.formatAlertBody(alert, event, message)

	// Send in-app notification
	if alert.NotifyInApp {
		source := fmt.Sprintf("price_alert:%s", alert.ID)
		n, err := s.notificationService.Create(ctx, nil, title, body, source)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to create in-app notification: %v", err))
		} else {
			// Add recipient
			_ = s.notificationService.AddRecipent(ctx, alert.UserID, n.ID)
		}
	}

	// Send push notification
	if alert.NotifyPush && s.pushService != nil {
		if err := s.sendPushNotification(ctx, alert, title, body); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to send push notification: %v", err))
		}
	}

	return nil
}

// sendPushNotification sends push notification for alert
func (s *PriceAlertService) sendPushNotification(ctx context.Context, alert *PriceAlert, title, body string) error {
	// Get user's push tokens (assuming similar pattern to existing code)
	// This would need integration with user service to get tokens

	// For now, log the intention
	s.logger.Info(fmt.Sprintf("Would send push notification to user %d: %s", alert.UserID, title))

	// TODO: Implement actual push notification sending
	// tokens, err := s.pushService.getUserPushTokens(ctx, alert.UserID)
	// ... send to FCM/Expo

	return nil
}

// formatAlertTitle creates a notification title
func (s *PriceAlertService) formatAlertTitle(alert *PriceAlert) string {
	label := "Price Alert"
	if alert.Label != nil && *alert.Label != "" {
		label = *alert.Label
	}

	priorityEmoji := ""
	switch AlertPriority(alert.Priority) {
	case PriorityCritical:
		priorityEmoji = "🚨 "
	case PriorityHigh:
		priorityEmoji = "⚠️ "
	}

	return fmt.Sprintf("%s%s Triggered", priorityEmoji, label)
}

// formatAlertBody creates notification body
func (s *PriceAlertService) formatAlertBody(alert *PriceAlert, event *AlertTriggerEvent, message string) string {
	pair := fmt.Sprintf("%s/%s", alert.SourceCurrency, alert.TargetCurrency)
	rate := event.CurrentRate.StringFixed(6)

	body := fmt.Sprintf("%s is now %s. %s", pair, rate, message)

	if event.ChangePercent != nil {
		sign := "+"
		if event.ChangePercent.IsNegative() {
			sign = ""
		}
		body += fmt.Sprintf(" (%s%.2f%%)", sign, event.ChangePercent.InexactFloat64())
	}

	if alert.Description != nil && *alert.Description != "" {
		body += fmt.Sprintf("\n%s", *alert.Description)
	}

	return body
}

// handleAlertTrigger updates alert state after triggering
func (s *PriceAlertService) handleAlertTrigger(ctx context.Context, alert *PriceAlert, currentRate decimal.Decimal) error {
	now := time.Now()

	params := db.UpdateAlertTriggerParams{
		ID:              alert.ID,
		TriggeredCount:  int32(alert.TriggeredCount + 1),
		LastTriggeredAt: sql.NullTime{Time: now, Valid: true},
		LastCheckedAt:   sql.NullTime{Time: now, Valid: true},
	}

	// Handle different alert types
	switch AlertType(alert.AlertType) {
	case AlertTypeOneTime:
		// Deactivate one-time alerts
		params.IsActive = sql.NullBool{Bool: false, Valid: true}
		s.logger.Info(fmt.Sprintf("Deactivating one-time alert %s", alert.ID))

	case AlertTypeRecurring:
		// Keep recurring alerts active but update baseline for percentage-based alerts
		if alert.AlertCondition == ConditionPercentUp ||
			alert.AlertCondition == ConditionPercentDown {
			params.BaselineRate = s.decimalToNullString(&currentRate)
		}

	case AlertTypeTrailing:
		// Trailing alerts remain active and continue tracking
		// No special action needed here as tracking is handled elsewhere
	}

	return s.store.UpdateAlertTrigger(ctx, params)
}

// GetUserAlerts retrieves all alerts for a user
func (s *PriceAlertService) GetUserAlerts(ctx context.Context, userID uuid.UUID, activeOnly bool) ([]*PriceAlert, error) {
	var dbAlerts []db.PriceAlert
	var err error

	if activeOnly {
		dbAlerts, err = s.store.GetUserActiveAlerts(ctx, userID)
	} else {
		dbAlerts, err = s.store.GetUserAlerts(ctx, userID)
	}

	if err != nil {
		return nil, err
	}

	alerts := make([]*PriceAlert, len(dbAlerts))
	for i, dbAlert := range dbAlerts {
		alerts[i] = s.dbAlertToModel(&dbAlert)
	}

	return alerts, nil
}

// GetAlert retrieves a specific alert
func (s *PriceAlertService) GetAlert(ctx context.Context, alertID uuid.UUID, userID uuid.UUID) (*PriceAlert, error) {
	dbAlert, err := s.store.GetPriceAlert(ctx, alertID)
	if err != nil {
		return nil, err
	}

	if dbAlert.UserID != userID {
		return nil, fmt.Errorf("unauthorized access to alert")
	}

	return s.dbAlertToModel(&dbAlert), nil
}

// UpdateAlert updates alert configuration
func (s *PriceAlertService) UpdateAlert(ctx context.Context, alertID uuid.UUID, userID uuid.UUID, req *CreateAlertRequest) (*PriceAlert, error) {
	// Verify ownership
	_, err := s.GetAlert(ctx, alertID, userID)
	if err != nil {
		return nil, err
	}

	// Validate new configuration
	if err := s.validateAlertConfig(req); err != nil {
		return nil, err
	}

	// Update logic similar to create
	// This is a simplified version - full implementation would update specific fields
	params := db.UpdatePriceAlertParams{
		ID:               alertID,
		TargetRate:       s.decimalToNullString(req.TargetRate),
		PercentageChange: s.decimalToNullString(req.PercentageChange),
		Description:      s.stringToNullString(req.Description),
		Label:            s.stringToNullString(req.Label),
	}

	updatedAlert, err := s.store.UpdatePriceAlert(ctx, params)
	if err != nil {
		return nil, err
	}

	return s.dbAlertToModel(&updatedAlert), nil
}

// PauseAlert temporarily disables an alert
func (s *PriceAlertService) PauseAlert(ctx context.Context, alertID uuid.UUID, userID uuid.UUID) error {
	alert, err := s.GetAlert(ctx, alertID, userID)
	if err != nil {
		return err
	}

	if !alert.IsActive {
		return fmt.Errorf("alert is already inactive")
	}

	return s.store.UpdateAlertStatus(ctx, db.UpdateAlertStatusParams{
		ID:       alertID,
		IsActive: false,
	})
}

// ResumeAlert reactivates a paused alert
func (s *PriceAlertService) ResumeAlert(ctx context.Context, alertID uuid.UUID, userID uuid.UUID) error {
	alert, err := s.GetAlert(ctx, alertID, userID)
	if err != nil {
		return err
	}

	if alert.IsActive {
		return fmt.Errorf("alert is already active")
	}

	// Reset baseline for percentage-based alerts
	if alert.AlertCondition == ConditionPercentUp ||
		alert.AlertCondition == ConditionPercentDown {
		currentRate, err := s.exchangeRateService.GetExchangeRate(ctx, alert.SourceCurrency, alert.TargetCurrency)
		if err != nil {
			return fmt.Errorf("failed to get current rate: %w", err)
		}

		// rate, _ := utils.ToDecimal(currentRate.Rate)
		_ = s.store.UpdateAlertBaseline(ctx, db.UpdateAlertBaselineParams{
			ID:           alertID,
			BaselineRate: s.decimalToNullString(&currentRate.Rate),
		})
	}

	return s.store.UpdateAlertStatus(ctx, db.UpdateAlertStatusParams{
		ID:       alertID,
		IsActive: true,
	})
}

// DeactivateAlert permanently deactivates an alert
func (s *PriceAlertService) DeactivateAlert(ctx context.Context, alertID uuid.UUID, userID uuid.UUID) error {
	_, err := s.GetAlert(ctx, alertID, userID)
	if err != nil {
		return err
	}

	return s.store.UpdateAlertStatus(ctx, db.UpdateAlertStatusParams{
		ID:       alertID,
		IsActive: false,
	})
}

// DeleteAlert soft-deletes an alert
func (s *PriceAlertService) DeleteAlert(ctx context.Context, alertID uuid.UUID, userID uuid.UUID) error {
	_, err := s.GetAlert(ctx, alertID, userID)
	if err != nil {
		return err
	}

	return s.store.DeletePriceAlert(ctx, db.DeletePriceAlertParams{
		ID:     alertID,
		UserID: userID,
	})
}

// GetAlertStats returns statistics about user's alerts
func (s *PriceAlertService) GetAlertStats(ctx context.Context, userID uuid.UUID) (map[string]interface{}, error) {
	stats, err := s.store.GetUserAlertStats(ctx, userID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_alerts":       stats.TotalAlerts,
		"active_alerts":      stats.ActiveAlerts,
		"triggered_count":    stats.TriggeredCount,
		"alerts_by_type":     stats.AlertsByType,
		"alerts_by_priority": stats.AlertsByPriority,
	}, nil
}

// Helper conversion functions
func (s *PriceAlertService) dbAlertToModel(dbAlert *db.PriceAlert) *PriceAlert {
	return &PriceAlert{
		ID:               dbAlert.ID,
		UserID:           dbAlert.UserID,
		SourceCurrency:   dbAlert.SourceCurrency,
		TargetCurrency:   dbAlert.TargetCurrency,
		AlertCondition:   AlertCondition(dbAlert.AlertCondition),
		AlertType:        AlertType(dbAlert.AlertType),
		Priority:         AlertPriority(dbAlert.Priority),
		TargetRate:       s.nullStringToDecimal(dbAlert.TargetRate),
		PercentageChange: s.nullStringToDecimal(dbAlert.PercentageChange),
		RangeMin:         s.nullStringToDecimal(dbAlert.RangeMin),
		RangeMax:         s.nullStringToDecimal(dbAlert.RangeMax),
		BaselineRate:     s.nullStringToDecimal(dbAlert.BaselineRate),
		TrailingDistance: s.nullStringToDecimal(dbAlert.TrailingDistance),
		MaxTrailingRate:  s.nullStringToDecimal(dbAlert.MaxTrailingRate),
		MinTrailingRate:  s.nullStringToDecimal(dbAlert.MinTrailingRate),
		Description:      s.nullStringToString(dbAlert.Description),
		Label:            s.nullStringToString(dbAlert.Label),
		IsActive:         dbAlert.IsActive,
		TriggeredCount:   int(dbAlert.TriggeredCount),
		LastTriggeredAt:  s.nullTimeToTime(dbAlert.LastTriggeredAt),
		LastCheckedAt:    s.nullTimeToTime(dbAlert.LastCheckedAt),
		ExpiresAt:        s.nullTimeToTime(dbAlert.ExpiresAt),
		NotifyPush:       dbAlert.NotifyPush,
		NotifyInApp:      dbAlert.NotifyInApp,
		CreatedAt:        dbAlert.CreatedAt,
		UpdatedAt:        dbAlert.UpdatedAt,
	}
}

func (s *PriceAlertService) decimalToNullString(d *decimal.Decimal) sql.NullString {
	if d == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: d.String(), Valid: true}
}

func (s *PriceAlertService) stringToNullString(str *string) sql.NullString {
	if str == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *str, Valid: true}
}

func (s *PriceAlertService) timeToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func (s *PriceAlertService) nullStringToDecimal(ns sql.NullString) *decimal.Decimal {
	if !ns.Valid {
		return nil
	}
	d, _ := decimal.NewFromString(ns.String)
	return &d
}

func (s *PriceAlertService) nullStringToString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func (s *PriceAlertService) nullTimeToTime(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}
