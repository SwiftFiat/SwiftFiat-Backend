package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/subscriptions"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Subscriptions struct {
	server        *Server
	Subscriptions *subscriptions.Service
	audit         *audit.Service
}

func (v Subscriptions) router(server *Server) {
	v.server = server
	v.Subscriptions = server.subscriptions
	v.audit = server.auditService

	v1 := server.router.Group("/api/v1/subscriptions")
	v1.Use(server.authMiddleware.AuthenticatedMiddleware())
	{
		// User Story 5 & 6: Subscription Overview & Details
		v1.POST("/custom", v.CreateCustomSubscription)
		v1.PATCH("/custom/:id", v.UpdateCustomSubscription)
		v1.GET("/", v.GetUserSubscriptions)
		v1.GET("/:id", v.GetSubscriptionDetails)

		// User Story 7: Manage subscription preferences
		v1.PATCH("/:id/preferences", v.UpdateSubscriptionPreferences)
		v1.PATCH("/:id/status", v.UpdateSubscriptionStatus)

		// User Story 4: Analytics
		v1.GET("/summary", v.GetSubscriptionSummary)
		v1.GET("/analytics/spending", v.GetSpendingAnalytics)
		v1.GET("/analytics/category", v.GetCategoryBreakdown)

		// Reminder management
		v1.GET("/reminders", v.GetUserReminders)

		// admin
		// User Story 9: User Subscription Monitoring
		v1.GET("/admin/all", v.AdminGetAllSubscriptions)
		v1.GET("/admin/users/:user_id", v.AdminGetUserSubscriptions)
		v1.PATCH("/admin/:id/auto-topup", v.AdminToggleAutoTopup)
		v1.PATCH("/admin/:id/status", v.AdminSetSubscriptionStatus)

		// User Story 10: Merchant Insights
		v1.GET("/admin/merchants", v.AdminGetMerchantInsights)
		v1.GET("/admin/merchants/:merchant_id", v.AdminGetMerchantDetails)

		// User Story 11: System Health & Alerts
		v1.GET("/admin/alerts", v.AdminGetSystemAlerts)
		// v1.GET("/admin/stats", v.AdminGetPlatformStats)

		// Merchant management
		v1.POST("/admin/merchants", v.AdminCreateMerchant)
		v1.PUT("/admin/merchants/:id", v.AdminUpdateMerchant)
		v1.GET("/admin/merchants/list", v.AdminListMerchants)
		// Analytics (extended)
		v1.GET("/admin/stats", v.GetSubscriptionStats)
		v1.GET("/admin/auto-topup/success-rate", v.GetAutoTopupSuccessRate)
		v1.GET("/admin/settings", v.AdminListSystemSettings)
		v1.GET("/admin/settings/:key", v.AdminGetSystemSetting)
		v1.PUT("/admin/settings/:key", v.AdminUpdateSystemSetting)
		v1.POST("/admin/settings/bulk", v.AdminBulkUpdateSettings)
		v1.GET("/admin/settings/validate/billing-cycle/:cycle", v.AdminValidateBillingCycle)

	}
}

type CreateCustomSubscriptionRequest struct {
	CardID       string `json:"card_id" binding:"required"`
	MerchantName string `json:"merchant_name" binding:"required,min=1,max=255"`
	DisplayName  string `json:"display_name" binding:"required,min=1,max=255"`
	Category     string `json:"category" binding:"required,oneof=streaming cloud_storage gaming music productivity fitness news utilities other"`
	// Amount is provided and stored as whole dollars
	Amount                 int64    `json:"amount" binding:"required"`
	Currency               string   `json:"currency" binding:"required,oneof=USD"`
	BillingCycle           string   `json:"billing_cycle" binding:"required,oneof=daily monthly yearly"`
	FirstChargeDate        string   `json:"first_charge_date" binding:"required"` // ISO 8601 format
	ReminderEnabled        bool     `json:"reminder_enabled"`
	CustomReminderDays     *int     `json:"custom_reminder_days,omitempty"` // Optional override
	AutoTopupBufferPercent *float64 `json:"auto_topup_buffer_percent,omitempty"`
	Notes                  string   `json:"notes,omitempty"`
}

// CreateCustomSubscription godoc
// @Summary Create custom subscription
// @Description Create a user-defined subscription with custom billing cycle
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param request body CreateCustomSubscriptionRequest true "Subscription details"
// @Success 201 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/custom [post]
func (v *Subscriptions) CreateCustomSubscription(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req CreateCustomSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// Parse card ID
	cardID, err := uuid.Parse(req.CardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid card id"))
		return
	}

	user, err := v.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		v.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("an error occurred, try again"))
		return
	}

	if !user.IsActive {
		c.JSON(http.StatusForbidden, basemodels.NewError(apistrings.DeactivatedAccount))
		return
	}

	// Verify card belongs to user
	card, err := v.server.queries.GetVirtualCard(c, cardID)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("card not found"))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusForbidden, basemodels.NewError("card does not belong to user"))
		return
	}

	// Parse first charge date
	firstChargeDate, err := time.Parse(time.RFC3339, req.FirstChargeDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid first_charge_date format (use ISO 8601)"))
		return
	}

	// Build service request
	serviceReq := &subscriptions.CreateCustomSubscriptionRequest{
		CardID:          cardID,
		MerchantName:    req.MerchantName,
		DisplayName:     req.DisplayName,
		Category:        req.Category,
		Amount:          req.Amount,
		Currency:        req.Currency,
		BillingCycle:    req.BillingCycle,
		FirstChargeDate: firstChargeDate,
		ReminderEnabled: req.ReminderEnabled,
		Notes:           req.Notes,
	}

	if req.CustomReminderDays != nil {
		serviceReq.CustomReminderTiming = req.CustomReminderDays
	}

	if req.AutoTopupBufferPercent != nil {
		bufferDecimal := decimal.NewFromFloat(*req.AutoTopupBufferPercent)
		serviceReq.AutoTopupBufferPercent = &bufferDecimal
	}

	// Create subscription
	subscription, err := v.Subscriptions.CreateCustomSubscription(c, activeUser.UserID, *serviceReq)
	if err != nil {
		errMsg := err.Error()
		entry := audit.NewLog(
			c,
			audit.CategorySubscription,
			audit.EventCreateCustomSubscription,
			"",
			"custom subscription creation failed",
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		v.audit.Log(entry)

		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventCreateCustomSubscription,
		subscription.ID.String(),
		fmt.Sprintf("custom subscription created: %s", req.DisplayName),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.NewValues = map[string]any{
		"merchant_name":     req.MerchantName,
		"display_name":      req.DisplayName,
		"category":          req.Category,
		"amount":            req.Amount,
		"currency":          req.Currency,
		"billing_cycle":     req.BillingCycle,
		"first_charge_date": firstChargeDate,
		"reminder_enabled":  req.ReminderEnabled,
		"notes":             req.Notes,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusCreated, basemodels.NewSuccess("custom subscription created", subscription))
}

// GetCustomSubscriptions godoc
// @Summary Get custom subscriptions
// @Description Get all user-created custom subscriptions
// @Tags Subscriptions
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/custom [get]
func (v *Subscriptions) GetCustomSubscriptions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subscriptions, err := v.server.queries.GetUserCustomSubscriptions(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("custom subscriptions retrieved", gin.H{
		"subscriptions": subscriptions,
		"count":         len(subscriptions),
	}))
}

type UpdateCustomSubscriptionRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	// Amount is specified in whole dollars
	Amount                 *int64   `json:"amount,omitempty"`
	BillingCycle           *string  `json:"billing_cycle,omitempty" binding:"omitempty,oneof=daily monthly yearly"`
	ReminderEnabled        *bool    `json:"reminder_enabled,omitempty"`
	CustomReminderDays     *int     `json:"custom_reminder_days,omitempty"`
	AutoTopupBufferPercent *float64 `json:"auto_topup_buffer_percent,omitempty"`
	Notes                  *string  `json:"notes,omitempty"`
}

type UpdateSystemSettingRequest struct {
	Value string `json:"value" binding:"required"`
}

type BulkUpdateSettingsRequest struct {
	Settings map[string]string `json:"settings" binding:"required"`
}

// UpdateCustomSubscription godoc
// @Summary Update custom subscription
// @Description Update a user-created custom subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Param request body UpdateCustomSubscriptionRequest true "Update details"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/custom/{id} [patch]
func (v *Subscriptions) UpdateCustomSubscription(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subscriptionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid subscription id"))
		return
	}

	var req UpdateCustomSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// Build service request
	serviceReq := &subscriptions.UpdateCustomSubscriptionRequest{
		DisplayName:          req.DisplayName,
		Amount:               req.Amount,
		BillingCycle:         req.BillingCycle,
		ReminderEnabled:      req.ReminderEnabled,
		CustomReminderTiming: req.CustomReminderDays,
		Notes:                req.Notes,
	}

	// Convert buffer percent if provided
	if req.AutoTopupBufferPercent != nil {
		bufferDecimal := decimal.NewFromFloat(*req.AutoTopupBufferPercent)
		serviceReq.AutoTopupBufferPercent = &bufferDecimal
	}

	// Update subscription
	updated, err := v.Subscriptions.UpdateCustomSubscription(c, activeUser.UserID, subscriptionID, serviceReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventUpdateCustomSubscription,
		updated.ID.String(),
		"custom subscription updated",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscription updated", updated))
}

// AdminListSystemSettings godoc
// @Summary List system settings (Admin)
// @Description Get all subscription system settings
// @Tags Admin
// @Produce json
// @Param category query string false "Filter by category"
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/settings [get]
func (v *Subscriptions) AdminListSystemSettings(c *gin.Context) {
	category := c.Query("category")

	settings, err := v.server.queries.ListSystemSettings(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Filter by category if provided
	if category != "" {
		var filtered []db.SubscriptionSystemSetting
		for _, s := range settings {
			if s.Category == category {
				filtered = append(filtered, s)
			}
		}
		settings = filtered
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("system settings", gin.H{
		"settings": settings,
		"count":    len(settings),
	}))
}

// AdminGetSystemSetting godoc
// @Summary Get system setting (Admin)
// @Description Get a specific system setting by key
// @Tags Admin
// @Produce json
// @Param key path string true "Setting Key"
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/settings/{key} [get]
func (v *Subscriptions) AdminGetSystemSetting(c *gin.Context) {
	key := c.Param("key")

	setting, err := v.server.queries.GetSystemSetting(c, key)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("setting not found"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("setting retrieved", setting))
}

// AdminUpdateSystemSetting godoc
// @Summary Update system setting (Admin)
// @Description Update a subscription system setting
// @Tags Admin
// @Accept json
// @Produce json
// @Param key path string true "Setting Key"
// @Param request body UpdateSystemSettingRequest true "New Value"
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/settings/{key} [put]
func (v *Subscriptions) AdminUpdateSystemSetting(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	key := c.Param("key")

	var req UpdateSystemSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	updated, err := v.server.queries.UpdateSystemSetting(c, db.UpdateSystemSettingParams{
		SettingKey:   key,
		SettingValue: req.Value,
		UpdatedBy:    sql.NullInt64{Int64: activeUser.UserID, Valid: true},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventUpdateSubscriptionSystemSetting,
		key,
		fmt.Sprintf("system setting updated: %s", key),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("setting updated", updated))
}

// AdminBulkUpdateSettings godoc
// @Summary Bulk update settings (Admin)
// @Description Update multiple system settings at once
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body BulkUpdateSettingsRequest true "Settings to update"
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/settings/bulk [post]
func (v *Subscriptions) AdminBulkUpdateSettings(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req BulkUpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	updated := make(map[string]interface{})
	failed := make(map[string]string)

	for key, value := range req.Settings {
		result, err := v.server.queries.UpdateSystemSetting(c, db.UpdateSystemSettingParams{
			SettingKey:   key,
			SettingValue: value,
			UpdatedBy:    sql.NullInt64{Int64: activeUser.UserID, Valid: true},
		})

		if err != nil {
			failed[key] = err.Error()
		} else {
			updated[key] = result
		}
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("bulk update completed", gin.H{
		"updated": updated,
		"failed":  failed,
	}))
}

// AdminValidateBillingCycle godoc
// @Summary Validate billing cycle (Admin)
// @Description Check if a billing cycle is currently allowed
// @Tags Admin
// @Produce json
// @Param cycle path string true "Billing Cycle" Enums(daily, monthly, yearly)
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/settings/validate/billing-cycle/{cycle} [get]
func (v *Subscriptions) AdminValidateBillingCycle(c *gin.Context) {
	cycle := c.Param("cycle")

	err := v.Subscriptions.ValidateBillingCycle(c, cycle)
	if err != nil {
		c.JSON(http.StatusOK, basemodels.NewSuccess("validation result", gin.H{
			"cycle":   cycle,
			"allowed": false,
			"reason":  err.Error(),
		}))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("validation result", gin.H{
		"cycle":   cycle,
		"allowed": true,
	}))
}

type UpdateSubscriptionPreferencesRequest struct {
	ReminderEnabled    *bool  `json:"reminder_enabled"`
	ReminderDaysBefore *int   `json:"reminder_days_before"`
	CustomName         string `json:"custom_name"`
	UserConfirmed      *bool  `json:"user_confirmed"`
}

type UpdateStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active cancelled paused"`
}

type CreateMerchantRequest struct {
	MerchantName     string   `json:"merchant_name" binding:"required"`
	DisplayName      string   `json:"display_name" binding:"required"`
	Aliases          []string `json:"aliases"`
	Category         string   `json:"category" binding:"required"`
	Subcategory      string   `json:"subcategory"`
	LogoURL          string   `json:"logo_url"`
	Website          string   `json:"website"`
	Description      string   `json:"description"`
	TypicalIntervals []int32  `json:"typical_intervals"`
	TypicalAmounts   []int64  `json:"typical_amounts"`
	MCCCodes         []string `json:"mcc_codes"`
	MatchConfidence  string   `json:"match_confidence"`
	AutoDetect       bool     `json:"auto_detect"`
}

type UpdateMerchantRequest struct {
	DisplayName      string   `json:"display_name"`
	Aliases          []string `json:"aliases"`
	Category         string   `json:"category"`
	Subcategory      string   `json:"subcategory"`
	LogoURL          string   `json:"logo_url"`
	TypicalIntervals []int32  `json:"typical_intervals"`
	TypicalAmounts   []int64  `json:"typical_amounts"`
	AutoDetect       bool     `json:"auto_detect"`
}

type UserSubscriptionsByCardRow struct {
	ID                      uuid.UUID      `json:"id"`
	UserID                  int64          `json:"user_id"`
	CardID                  uuid.UUID      `json:"card_id"`
	MerchantID              sql.NullInt64  `json:"merchant_id"`
	MerchantName            string         `json:"merchant_name"`
	DisplayName             string         `json:"display_name"`
	Category                sql.NullString `json:"category"`
	Amount                  int64          `json:"amount"`
	Currency                string         `json:"currency"`
	BillingIntervalDays     int32          `json:"billing_interval_days"`
	FirstChargeDate         time.Time      `json:"first_charge_date"`
	LastChargeDate          time.Time      `json:"last_charge_date"`
	NextEstimatedChargeDate time.Time      `json:"next_estimated_charge_date"`
	Status                  string         `json:"status"`
	ConfidenceScore         string         `json:"confidence_score"`
	TotalCharges            int32          `json:"total_charges"`
	FailedCharges           int32          `json:"failed_charges"`
	LastFailedDate          sql.NullTime   `json:"last_failed_date"`
	LastFailureReason       sql.NullString `json:"last_failure_reason"`
	ReminderEnabled         bool           `json:"reminder_enabled"`
	ReminderDaysBefore      int32          `json:"reminder_days_before"`
	UserConfirmed           bool           `json:"user_confirmed"`
	CustomName              sql.NullString `json:"custom_name"`
	IsCustom                bool           `json:"is_custom"`
	CustomBillingCycle      sql.NullString `json:"custom_billing_cycle"`
	CustomAmountOverride    bool           `json:"custom_amount_override"`
	AutoTopupBufferPercent  sql.NullString `json:"auto_topup_buffer_percent"`
	CustomReminderTiming    sql.NullInt32  `json:"custom_reminder_timing"`
	Notes                   sql.NullString `json:"notes"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
	CancelledAt             sql.NullTime   `json:"cancelled_at"`
	LogoUrl                 sql.NullString `json:"logo_url"`
	Website                 sql.NullString `json:"website"`
}

func mapUserSubscriptionsByCardRow(row db.GetUserSubscriptionsByCardRow) UserSubscriptionsByCardRow {
	return UserSubscriptionsByCardRow{
		ID:                      row.ID,
		UserID:                  row.UserID,
		CardID:                  row.CardID,
		MerchantID:              sql.NullInt64{Int64: row.MerchantID.Int64, Valid: row.MerchantID.Valid},
		MerchantName:            row.MerchantName,
		DisplayName:             row.DisplayName,
		Category:                sql.NullString{String: row.Category.String, Valid: row.Category.Valid},
		Amount:                  row.Amount,
		Currency:                row.Currency,
		BillingIntervalDays:     row.BillingIntervalDays,
		FirstChargeDate:         row.FirstChargeDate,
		LastChargeDate:          row.LastChargeDate,
		NextEstimatedChargeDate: row.NextEstimatedChargeDate,
		Status:                  row.Status,
		ConfidenceScore:         row.ConfidenceScore,
		TotalCharges:            row.TotalCharges,
		FailedCharges:           row.FailedCharges,
		LastFailedDate:          sql.NullTime{Time: row.LastFailedDate.Time, Valid: row.LastFailedDate.Valid},
		LastFailureReason:       sql.NullString{String: row.LastFailureReason.String, Valid: row.LastFailureReason.Valid},
		ReminderEnabled:         row.ReminderEnabled,
		ReminderDaysBefore:      row.ReminderDaysBefore,
		UserConfirmed:           row.UserConfirmed,
		IsCustom:                row.IsCustom,
		CustomName:              sql.NullString{String: row.CustomName.String, Valid: row.CustomName.Valid},
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
		CancelledAt:             sql.NullTime{Time: row.CancelledAt.Time, Valid: row.CancelledAt.Valid},
		LogoUrl:                 sql.NullString{String: row.LogoUrl.String, Valid: row.LogoUrl.Valid},
		Website:                 sql.NullString{String: row.Website.String, Valid: row.Website.Valid},
	}
}

type ToggleAutoTopupRequest struct {
	Enabled bool `json:"enabled" binding:"required"`
}

type AdminUpdateStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active inactive"`
}

// AdminToggleAutoTopup godoc
// @Summary Toggle subscription auto topup
// @Description Toggle subscription auto topup
// @Tags subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Param request body ToggleAutoTopupRequest true "Toggle auto topup"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/{id}/auto-topup [post]
func (v *Subscriptions) AdminToggleAutoTopup(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	subID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid subscription id"))
		return
	}

	var req ToggleAutoTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	card, err := v.server.queries.AdminToggleCardAutoTopup(c, db.AdminToggleCardAutoTopupParams{
		ID:               subID,
		AutoTopupEnabled: req.Enabled,
	})
	if err != nil {
		errMsg := err.Error()
		entry := audit.NewLog(
			c,
			audit.CategorySubscription,
			audit.EventUpdateAutoTopup,
			card.ID.String(),
			fmt.Sprintf("auto topup updated to %t", req.Enabled),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventUpdateAutoTopup,
		card.ID.String(),
		fmt.Sprintf("auto topup updated to %t", req.Enabled),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("auto topup updated", req.Enabled))
}

// AdminSetSubscriptionStatus godoc
// @Summary Set subscription status
// @Description Set subscription status
// @Tags subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Param request body AdminUpdateStatusRequest true "Set subscription status"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/{id}/status [post]
func (v *Subscriptions) AdminSetSubscriptionStatus(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid subscription id"))
		return
	}

	var req AdminUpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	updated, err := v.server.queries.UpdateSubscriptionStatus(c, db.UpdateSubscriptionStatusParams{
		ID:     subID,
		Status: req.Status,
	})
	if err != nil {
		errMsg := err.Error()
		entry := audit.NewLog(
			c,
			audit.CategorySubscription,
			audit.EventUpdateSubscriptionStatus,
			updated.ID.String(),
			fmt.Sprintf("subscription status updated to %s", req.Status),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventUpdateSubscriptionStatus,
		updated.ID.String(),
		fmt.Sprintf("subscription status updated to %s", req.Status),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscription status updated", updated))
}

// GetSubscriptionStats godoc
// @Summary Get subscription stats
// @Description Total subscriptions, active/inactive count, monthly spend
// @Tags Subscriptions
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/stats [get]
func (v *Subscriptions) GetSubscriptionStats(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	stats, err := v.Subscriptions.GetSubscriptionStats(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscription stats", stats))
}

// GetAutoTopupSuccessRate godoc
// @Summary Get auto topup success rate
// @Description Auto topup success metrics
// @Tags Subscriptions
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/subscriptions/admin/auto-topup/success-rate [get]
func (v *Subscriptions) GetAutoTopupSuccessRate(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	rate, err := v.Subscriptions.GetAutoTopupSuccessRate(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("auto topup success rate", rate))
}

// GetUserSubscriptions godoc
// @Summary Get user subscriptions
// @Description Get all active subscriptions for the authenticated user
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param card_id query string false "Filter by card ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions [get]
func (v *Subscriptions) GetUserSubscriptions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subscriptions, err := v.server.queries.GetUserSubscriptions(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var response []UserSubscriptionsByCardRow
	for _, subscription := range subscriptions {
		response = append(response, mapUserSubscriptionsByCardRow(db.GetUserSubscriptionsByCardRow(subscription)))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscriptions retrieved", gin.H{
		"subscriptions": response,
		"count":         len(response),
	}))
}

// GetSubscriptionDetails godoc
// @Summary Get subscription details
// @Description Get detailed information about a specific subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/{id} [get]
func (v *Subscriptions) GetSubscriptionDetails(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subscriptionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid subscription id"))
		return
	}

	subscription, err := v.server.queries.GetUserSubscription(c, subscriptionID)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("subscription not found"))
		return
	}

	// Verify ownership
	if subscription.UserID != activeUser.UserID {
		c.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Get recent transactions for this subscription
	transactions, _ := v.server.queries.GetCardTransactions(c, db.GetCardTransactionsParams{
		CardID: subscription.CardID,
		Limit:  5,
		Offset: 0,
	})

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscription details", gin.H{
		"subscription":        subscription,
		"recent_transactions": transactions,
	}))
}

// UpdateSubscriptionPreferences godoc
// @Summary Update subscription preferences
// @Description Update reminder settings and custom name for a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Param request body UpdateSubscriptionPreferencesRequest true "Preferences"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/{id}/preferences [patch]
func (v *Subscriptions) UpdateSubscriptionPreferences(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subscriptionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid subscription id"))
		return
	}

	var req UpdateSubscriptionPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	params := db.UpdateSubscriptionPreferencesParams{
		ID:     subscriptionID,
		UserID: activeUser.UserID,
	}

	if req.ReminderEnabled != nil {
		params.ReminderEnabled = sql.NullBool{Bool: *req.ReminderEnabled, Valid: true}
	}
	if req.ReminderDaysBefore != nil {
		params.ReminderDaysBefore = sql.NullInt32{Int32: int32(*req.ReminderDaysBefore), Valid: true}
	}
	if req.CustomName != "" {
		params.CustomName = sql.NullString{String: req.CustomName, Valid: true}
	}
	if req.UserConfirmed != nil {
		params.UserConfirmed = sql.NullBool{Bool: *req.UserConfirmed, Valid: true}
	}

	updated, err := v.server.queries.UpdateSubscriptionPreferences(c, params)

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventUpdateSubscriptionPreferences,
		updated.ID.String(),
		"subscription preferences updated",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.OldValues = map[string]interface{}{
		"reminder_enabled":     updated.ReminderEnabled,
		"reminder_days_before": updated.ReminderDaysBefore,
		"custom_name":          updated.CustomName,
		"user_confirmed":       updated.UserConfirmed,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("preferences updated", updated))
}

// UpdateSubscriptionStatus godoc
// @Summary Update subscription status
// @Description Cancel or pause a subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Param request body UpdateStatusRequest true "Status update"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/{id}/status [patch]
func (v *Subscriptions) UpdateSubscriptionStatus(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	subscriptionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid subscription id"))
		return
	}

	// Verify ownership
	subscription, err := v.server.queries.GetUserSubscription(c, subscriptionID)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("subscription not found"))
		return
	}

	if subscription.UserID != activeUser.UserID {
		c.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	updated, err := v.server.queries.UpdateSubscriptionStatus(c, db.UpdateSubscriptionStatusParams{
		ID:     subscriptionID,
		Status: req.Status,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySubscription,
		audit.EventUpdateSubscriptionStatus,
		updated.ID.String(),
		"subscription status updated",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.OldValues = map[string]interface{}{
		"status": subscription.Status,
	}
	entry.NewValues = map[string]interface{}{
		"status": updated.Status,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("status updated", updated))
}

// GetSubscriptionSummary godoc
// @Summary Get subscription summary
// @Description Get subscription analytics summary for the user
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/summary [get]
func (v *Subscriptions) GetSubscriptionSummary(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	summary, err := v.Subscriptions.GetUserSubscriptionSummary(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscription summary", summary))
}

// GetSpendingAnalytics godoc
// @Summary Get spending analytics
// @Description Get detailed spending analytics for subscriptions
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param from query string false "Start date (YYYY-MM-DD)"
// @Param to query string false "End date (YYYY-MM-DD)"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/analytics/spending [get]
func (v *Subscriptions) GetSpendingAnalytics(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Get all user cards to aggregate spending
	cards, err := v.server.queries.GetUserCards(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var totalSpending int64
	cardSpending := make(map[string]int64)

	for _, card := range cards {
		// Get transactions for each card
		txs, _ := v.server.queries.GetRecurringTransactions(c, db.GetRecurringTransactionsParams{
			CardID: card.ID,
			Limit:  100,
			Offset: 0,
		})

		var cardTotal int64
		for _, tx := range txs {
			cardTotal += tx.Amount
		}

		cardSpending[card.CardName] = cardTotal
		totalSpending += cardTotal
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("spending analytics", gin.H{
		// All amounts are in dollars
		"total_spending": totalSpending,
		"card_breakdown": cardSpending,
	}))
}

// GetCategoryBreakdown godoc
// @Summary Get category breakdown
// @Description Get subscription spending breakdown by category
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/analytics/category [get]
func (h *Subscriptions) GetCategoryBreakdown(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	breakdown, err := h.server.queries.GetSubscriptionsByCategory(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("category breakdown", breakdown))
}

// GetUserReminders godoc
// @Summary Get user reminders
// @Description Get subscription reminders for the user
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/reminders [get]
func (v *Subscriptions) GetUserReminders(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset := (page - 1) * limit

	reminders, err := v.server.queries.GetUserReminders(c, db.GetUserRemindersParams{
		UserID: activeUser.UserID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("reminders retrieved", gin.H{
		"reminders": reminders,
		"page":      page,
		"limit":     limit,
	}))
}

// AdminGetAllSubscriptions godoc
// @Summary Get all subscriptions (Admin)
// @Description Get all subscriptions across the platform
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param status query string false "Filter by status"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/all [get]
func (h *Subscriptions) AdminGetAllSubscriptions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	subs, err := h.server.queries.ListAllSubscriptions(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("all subscriptions", gin.H{
		"subscriptions": subs,
		"page":          page,
		"limit":         limit,
	}))
}

// AdminGetUserSubscriptions godoc
// @Summary Get user subscriptions (Admin)
// @Description Get all subscriptions for a specific user
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param user_id path int true "User ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/users/{user_id} [get]
func (v *Subscriptions) AdminGetUserSubscriptions(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid user id"))
		return
	}

	subscriptions, err := v.server.queries.GetUserSubscriptions(c, int64(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	summary, _ := v.Subscriptions.GetUserSubscriptionSummary(c, int64(userID))

	c.JSON(http.StatusOK, basemodels.NewSuccess("user subscriptions", gin.H{
		"subscriptions": subscriptions,
		"summary":       summary,
	}))
}

// AdminGetMerchantInsights godoc
// @Summary Get merchant insights (Admin)
// @Description Get spending analytics per merchant
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/merchants [get]
func (v *Subscriptions) AdminGetMerchantInsights(c *gin.Context) {
	merchants, err := v.server.queries.ListSubscriptionMerchants(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// TODO: Add actual spend analytics per merchant
	c.JSON(http.StatusOK, basemodels.NewSuccess("merchant insights", gin.H{
		"merchants": merchants,
		"total":     len(merchants),
	}))
}

// AdminGetMerchantDetails godoc
// @Summary Get merchant details (Admin)
// @Description Get detailed information about a specific merchant
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param merchant_id path int true "Merchant ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/merchants/{merchant_id} [get]
func (v *Subscriptions) AdminGetMerchantDetails(c *gin.Context) {
	merchantID, err := strconv.ParseInt(c.Param("merchant_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid merchant id"))
		return
	}

	merchant, err := v.server.queries.GetSubscriptionMerchant(c, merchantID)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("merchant not found"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("merchant details", merchant))
}

// AdminGetSystemAlerts godoc
// @Summary Get system alerts (Admin)
// @Description Get system health alerts and failed renewals
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/alerts [get]
func (v *Subscriptions) AdminGetSystemAlerts(c *gin.Context) {
	// Get failed reminders
	failedReminders, _ := v.server.queries.GetPendingReminders(c, 100)

	c.JSON(http.StatusOK, basemodels.NewSuccess("system alerts", gin.H{
		"pending_reminders": len(failedReminders),
		"reminders":         failedReminders,
	}))
}

// AdminCreateMerchant godoc
// @Summary Create subscription merchant (Admin)
// @Description Add a new subscription merchant to the database
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param request body CreateMerchantRequest true "Merchant details"
// @Success 201 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/merchants [post]
func (v *Subscriptions) AdminCreateMerchant(c *gin.Context) {
	var req CreateMerchantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	merchant, err := v.server.queries.CreateSubscriptionMerchant(c, db.CreateSubscriptionMerchantParams{
		MerchantName:     req.MerchantName,
		DisplayName:      req.DisplayName,
		Aliases:          req.Aliases,
		Category:         req.Category,
		Subcategory:      sql.NullString{String: req.Subcategory, Valid: req.Subcategory != ""},
		LogoUrl:          sql.NullString{String: req.LogoURL, Valid: req.LogoURL != ""},
		Website:          sql.NullString{String: req.Website, Valid: req.Website != ""},
		Description:      sql.NullString{String: req.Description, Valid: req.Description != ""},
		TypicalIntervals: req.TypicalIntervals,
		TypicalAmounts:   req.TypicalAmounts,
		MccCodes:         req.MCCCodes,
		MatchConfidence:  req.MatchConfidence,
		AutoDetect:       req.AutoDetect,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusCreated, basemodels.NewSuccess("merchant created", merchant))
}

// AdminUpdateMerchant godoc
// @Summary Update subscription merchant (Admin)
// @Description Update an existing subscription merchant
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path int true "Merchant ID"
// @Param request body UpdateMerchantRequest true "Merchant updates"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/merchants/{id} [put]
func (v *Subscriptions) AdminUpdateMerchant(c *gin.Context) {
	merchantID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid merchant id"))
		return
	}

	var req UpdateMerchantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	merchant, err := v.server.queries.UpdateSubscriptionMerchant(c, db.UpdateSubscriptionMerchantParams{
		ID:               merchantID,
		DisplayName:      sql.NullString{String: req.DisplayName, Valid: req.DisplayName != ""},
		Aliases:          req.Aliases,
		Category:         sql.NullString{String: req.Category, Valid: req.Category != ""},
		Subcategory:      sql.NullString{String: req.Subcategory, Valid: req.Subcategory != ""},
		LogoUrl:          sql.NullString{String: req.LogoURL, Valid: req.LogoURL != ""},
		TypicalIntervals: req.TypicalIntervals,
		TypicalAmounts:   req.TypicalAmounts,
		AutoDetect:       sql.NullBool{Bool: req.AutoDetect, Valid: true},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("merchant updated", merchant))
}

// AdminListMerchants godoc
// @Summary List all merchants (Admin)
// @Description Get list of all subscription merchants
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param category query string false "Filter by category"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/subscriptions/admin/merchants/list [get]
func (v *Subscriptions) AdminListMerchants(c *gin.Context) {
	category := c.Query("category")

	var merchants []db.SubscriptionMerchant
	var err error

	if category != "" {
		merchants, err = v.server.queries.ListSubscriptionMerchantsByCategory(c, category)
	} else {
		merchants, err = v.server.queries.ListSubscriptionMerchants(c)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("merchants retrieved", gin.H{
		"merchants": merchants,
		"count":     len(merchants),
	}))
}
