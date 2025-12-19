package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/subscriptions"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Subscriptions struct {
	server        *Server
	Subscriptions *subscriptions.Service
}

func (v Subscriptions) router(server *Server) {
	v.server = server
	v.Subscriptions = server.subscriptions

	v1 := server.router.Group("/api/v1/subscriptions")
	v1.Use(server.authMiddleware.AuthenticatedMiddleware())
	{
		// User Story 5 & 6: Subscription Overview & Details
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
		v1.GET("/all", v.AdminGetAllSubscriptions)
		v1.GET("/users/:user_id", v.AdminGetUserSubscriptions)

		// User Story 10: Merchant Insights
		v1.GET("/merchants", v.AdminGetMerchantInsights)
		v1.GET("/merchants/:merchant_id", v.AdminGetMerchantDetails)

		// User Story 11: System Health & Alerts
		v1.GET("/alerts", v.AdminGetSystemAlerts)
		v1.GET("/stats", v.AdminGetPlatformStats)

		// Merchant management
		v1.POST("/merchants", v.AdminCreateMerchant)
		v1.PATCH("/merchants/:id", v.AdminUpdateMerchant)
		v1.GET("/merchants/list", v.AdminListMerchants)
	}
}

type UpdateSubscriptionPreferencesRequest struct {
	ReminderEnabled    *bool  `json:"reminder_enabled"`
	ReminderDaysBefore *int   `json:"reminder_days_before"`
	CustomName         string `json:"custom_name"`
	UserConfirmed      *bool  `json:"user_confirmed"`
}

type UpdateStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active cancelled paused failed"`
}

type CreateMerchantRequest struct {
	MerchantName        string   `json:"merchant_name" binding:"required"`
	DisplayName         string   `json:"display_name" binding:"required"`
	Aliases             []string `json:"aliases"`
	Category            string   `json:"category" binding:"required"`
	Subcategory         string   `json:"subcategory"`
	LogoURL             string   `json:"logo_url"`
	Website             string   `json:"website"`
	Description         string   `json:"description"`
	TypicalIntervals    []int32  `json:"typical_intervals"`
	TypicalAmountsCents []int64  `json:"typical_amounts_cents"`
	MCCCodes            []string `json:"mcc_codes"`
	MatchConfidence     string   `json:"match_confidence"`
	AutoDetect          bool     `json:"auto_detect"`
}

type UpdateMerchantRequest struct {
	DisplayName         string   `json:"display_name"`
	Aliases             []string `json:"aliases"`
	Category            string   `json:"category"`
	Subcategory         string   `json:"subcategory"`
	LogoURL             string   `json:"logo_url"`
	TypicalIntervals    []int32  `json:"typical_intervals"`
	TypicalAmountsCents []int64  `json:"typical_amounts_cents"`
	AutoDetect          bool     `json:"auto_detect"`
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
	// activeUser, err := utils.GetActiveUser(c)
	// if err != nil {
	// 	v.server.logger.Error(err.Error())
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	cardIDStr := c.Query("card_id")

	var subscriptions []db.GetUserSubscriptionsByCardRow

	if cardIDStr != "" {
		cardID, err := uuid.Parse(cardIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, basemodels.NewError("invalid card_id"))
			return
		}

		subscriptions, err = v.server.queries.GetUserSubscriptionsByCard(c, cardID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}
	}
	// } else {
	// 	subscriptions, err = v.server.queries.GetUserSubscriptions(c, activeUser.UserID)
	// 	if err != nil {
	// 		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
	// 		return
	// 	}
	// }

	c.JSON(http.StatusOK, basemodels.NewSuccess("subscriptions retrieved", gin.H{
		"subscriptions": subscriptions,
		"count":         len(subscriptions),
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

	updated, err := v.server.queries.UpdateSubscriptionPreferences(c, db.UpdateSubscriptionPreferencesParams{
		ID:                 subscriptionID,
		UserID:             activeUser.UserID,
		ReminderEnabled:    sql.NullBool{Bool: *req.ReminderEnabled, Valid: req.ReminderEnabled != nil},
		ReminderDaysBefore: sql.NullInt32{Int32: int32(*req.ReminderDaysBefore), Valid: req.ReminderDaysBefore != nil},
		CustomName:         sql.NullString{String: req.CustomName, Valid: req.CustomName != ""},
		UserConfirmed:      sql.NullBool{Bool: *req.UserConfirmed, Valid: req.UserConfirmed != nil},
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

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
		"total_spending_cents": totalSpending,
		"total_spending":       float64(totalSpending) / 100,
		"card_breakdown":       cardSpending,
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
// @Router /api/v1/admin/subscriptions/all [get]
func (h *Subscriptions) AdminGetAllSubscriptions(c *gin.Context) {
	// TODO: Add admin role verification
	
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	// This would need a new SQL query to get all subscriptions
	// For now, return placeholder
	c.JSON(http.StatusOK, basemodels.NewSuccess("all subscriptions", gin.H{
		"message": "Admin endpoint - implementation pending",
		"page": page,
		"limit": limit,
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
// @Router /api/v1/admin/subscriptions/users/{user_id} [get]
func (v *Subscriptions) AdminGetUserSubscriptions(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid user id"))
		return
	}

	subscriptions, err := v.server.queries.GetUserSubscriptions(c, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	summary, _ := v.Subscriptions.GetUserSubscriptionSummary(c, userID)

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
// @Router /api/v1/admin/subscriptions/merchants [get]
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
// @Router /api/v1/admin/subscriptions/merchants/{merchant_id} [get]
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
// @Router /api/v1/admin/subscriptions/alerts [get]
func (v *Subscriptions) AdminGetSystemAlerts(c *gin.Context) {
	// Get failed reminders
	failedReminders, _ := v.server.queries.GetPendingReminders(c, 100)
	
	c.JSON(http.StatusOK, basemodels.NewSuccess("system alerts", gin.H{
		"pending_reminders": len(failedReminders),
		"reminders":         failedReminders,
	}))
}

// AdminGetPlatformStats godoc
// @Summary Get platform statistics (Admin)
// @Description Get overall platform subscription statistics
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/subscriptions/stats [get]
func (v *Subscriptions) AdminGetPlatformStats(c *gin.Context) {
	// TODO: Implement comprehensive platform stats
	c.JSON(http.StatusOK, basemodels.NewSuccess("platform stats", gin.H{
		"message": "Stats endpoint - implementation pending",
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
// @Router /api/v1/admin/subscriptions/merchants [post]
func (v *Subscriptions) AdminCreateMerchant(c *gin.Context) {
	var req CreateMerchantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	merchant, err := v.server.queries.CreateSubscriptionMerchant(c, db.CreateSubscriptionMerchantParams{
		MerchantName:          req.MerchantName,
		DisplayName:           req.DisplayName,
		Aliases:               req.Aliases,
		Category:              req.Category,
		Subcategory:           sql.NullString{String: req.Subcategory, Valid: req.Subcategory != ""},
		LogoUrl:               sql.NullString{String: req.LogoURL, Valid: req.LogoURL != ""},
		Website:               sql.NullString{String: req.Website, Valid: req.Website != ""},
		Description:           sql.NullString{String: req.Description, Valid: req.Description != ""},
		TypicalIntervals:      req.TypicalIntervals,
		TypicalAmountsCents:   req.TypicalAmountsCents,
		MccCodes:              req.MCCCodes,
		MatchConfidence:       req.MatchConfidence,
		AutoDetect:            req.AutoDetect,
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
// @Router /api/v1/admin/subscriptions/merchants/{id} [patch]
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
		ID:                  merchantID,
		DisplayName:         sql.NullString{String: req.DisplayName, Valid: req.DisplayName != ""},
		Aliases:             req.Aliases,
		Category:            sql.NullString{String: req.Category, Valid: req.Category != ""},
		Subcategory:         sql.NullString{String: req.Subcategory, Valid: req.Subcategory != ""},
		LogoUrl:             sql.NullString{String: req.LogoURL, Valid: req.LogoURL != ""},
		TypicalIntervals:    req.TypicalIntervals,
		TypicalAmountsCents: req.TypicalAmountsCents,
		AutoDetect:          sql.NullBool{Bool: req.AutoDetect, Valid: true},
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
// @Router /api/v1/admin/subscriptions/merchants/list [get]
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