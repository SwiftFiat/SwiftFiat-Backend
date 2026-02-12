package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	pricealert "github.com/SwiftFiat/SwiftFiat-Backend/services/price_alert"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PriceAlertHandler struct {
	server         *Server
	logger         *logging.Logger
	alertService   *pricealert.PriceAlertService
	alertScheduler *pricealert.AlertScheduler
	audit          *audit.Service
}

func (h PriceAlertHandler) router(server *Server) {
	h.server = server
	h.logger = server.logger
	h.alertService = server.priceAlertSvc
	h.alertScheduler = server.priceAlertScheduler
	h.audit = server.auditService

	v1 := server.router.Group("/api/v1/price-alerts")
	v1.Use(h.server.authMiddleware.AuthenticatedMiddleware())
	{
		// Alert CRUD operations
		v1.POST("", h.CreateAlert)
		v1.GET("", h.GetAlerts)
		v1.GET("/:alert_id", h.GetAlert)
		v1.PUT("/:alert_id", h.UpdateAlert)
		v1.DELETE("/:alert_id", h.DeleteAlert)

		// Alert control operations
		v1.POST("/:alert_id/pause", h.PauseAlert)
		v1.POST("/:alert_id/resume", h.ResumeAlert)

		// Statistics and monitoring
		v1.GET("/stats", h.GetAlertStats)
		v1.GET("/history/:alert_id", h.GetAlertHistory)

		// Admin-only endpoints
		v1.GET("/admin/all", h.GetAllAlerts)
		v1.GET("/admin/scheduler/stats", h.GetSchedulerStats)
		v1.POST("/admin/scheduler/trigger", h.TriggerManualCheck)
		v1.GET("/admin/metrics", h.GetSystemMetrics)

	}
}

// CreateAlert godoc
// @Summary Create a new price alert
// @Description Creates a custom price alert for crypto-to-fiat rate monitoring
// @Tags PriceAlerts
// @Accept json
// @Produce json
// @Param request body pricealert.CreateAlertRequest true "Alert configuration"
// @Success 201 {object} pricealert.PriceAlert
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts [post]
// @Security BearerAuth
func (h *PriceAlertHandler) CreateAlert(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req pricealert.CreateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request", "error", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// Additional validation
	if req.SourceCurrency == req.TargetCurrency {
		c.JSON(http.StatusBadRequest, basemodels.NewError("source and target currencies must be different"))
		return
	}

	alert, err := h.alertService.CreateAlert(c.Request.Context(), activeUser.UserID, &req)
	if err != nil {
		h.logger.Error("Failed to create price alert", "error", err)

		// Audit log
		errMsg := err.Error()
		entry := audit.NewLog(
			c,
			audit.CategoryPriceAlert,
			audit.EventCreatePriceAlert,
			"",
			"Failed to create price alert",
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		h.audit.Log(entry)

		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Audit log
	entry := audit.NewLog(
		c,
		audit.CategoryPriceAlert,
		audit.EventCreatePriceAlert,
		alert.ID.String(),
		"Price alert created successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.Metadata = map[string]any{
		"source_currency": alert.SourceCurrency,
		"target_currency": alert.TargetCurrency,
		"alert_condition": alert.AlertCondition,
		"alert_type":      alert.AlertType,
		"priority":        alert.Priority,
	}
	h.audit.Log(entry)

	c.JSON(http.StatusCreated, basemodels.NewSuccess("Price alert created successfully", alert))
}

// GetAlerts godoc
// @Summary Get user's price alerts
// @Description Retrieves all price alerts for the authenticated user
// @Tags PriceAlerts
// @Produce json
// @Param active_only query bool false "Return only active alerts" default(false)
// @Success 200 {object} []pricealert.PriceAlert
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetAlerts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	activeOnly, _ := strconv.ParseBool(c.DefaultQuery("active_only", "false"))

	alerts, err := h.alertService.GetUserAlerts(c.Request.Context(), activeUser.UserID, activeOnly)
	if err != nil {
		h.logger.Error("Failed to fetch price alerts", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch price alerts"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", alerts))
}

// GetAlert godoc
// @Summary Get a specific price alert
// @Description Retrieves details of a specific price alert
// @Tags PriceAlerts
// @Produce json
// @Param alert_id path string true "Alert ID (UUID)"
// @Success 200 {object} pricealert.PriceAlert
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/{alert_id} [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetAlert(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	alertID, err := uuid.Parse(c.Param("alert_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid alert ID"))
		return
	}

	alert, err := h.alertService.GetAlert(c.Request.Context(), alertID, activeUser.UserID)
	if err != nil {
		h.logger.Error("Failed to fetch price alert", "error", err)
		c.JSON(http.StatusNotFound, basemodels.NewError("alert not found"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", alert))
}

// UpdateAlert godoc
// @Summary Update a price alert [fix needed]
// @Description Updates configuration of an existing price alert
// @Tags PriceAlerts
// @Accept json
// @Produce json
// @Param alert_id path string true "Alert ID (UUID)"
// @Param request body pricealert.CreateAlertRequest true "Updated alert configuration"
// @Success 200 {object} pricealert.PriceAlert
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/{alert_id} [put]
// @Security BearerAuth
func (h *PriceAlertHandler) UpdateAlert(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	alertID, err := uuid.Parse(c.Param("alert_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid alert ID"))
		return
	}

	var req pricealert.CreateAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Invalid request", "error", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	alert, err := h.alertService.UpdateAlert(c.Request.Context(), alertID, activeUser.UserID, &req)
	if err != nil {
		h.logger.Error("Failed to update price alert", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Audit log
	entry := audit.NewLog(
		c,
		audit.CategoryPriceAlert,
		audit.EventUpdatePriceAlert,
		alert.ID.String(),
		"Price alert updated successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	h.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Price alert updated successfully", alert))
}

// DeleteAlert godoc
// @Summary Delete a price alert
// @Description Soft-deletes a price alert
// @Tags PriceAlerts
// @Produce json
// @Param alert_id path string true "Alert ID (UUID)"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/{alert_id} [delete]
// @Security BearerAuth
func (h *PriceAlertHandler) DeleteAlert(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	alertID, err := uuid.Parse(c.Param("alert_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid alert ID"))
		return
	}

	if err := h.alertService.DeleteAlert(c.Request.Context(), alertID, activeUser.UserID); err != nil {
		h.logger.Error("Failed to delete price alert", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Audit log
	entry := audit.NewLog(
		c,
		audit.CategoryPriceAlert,
		audit.EventDeletePriceAlert,
		alertID.String(),
		"Price alert deleted successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	h.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Price alert deleted successfully", nil))
}

// PauseAlert godoc
// @Summary Pause a price alert
// @Description Temporarily disables a price alert
// @Tags PriceAlerts
// @Produce json
// @Param alert_id path string true "Alert ID (UUID)"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/{alert_id}/pause [post]
// @Security BearerAuth
func (h *PriceAlertHandler) PauseAlert(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	alertID, err := uuid.Parse(c.Param("alert_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid alert ID"))
		return
	}

	if err := h.alertService.PauseAlert(c.Request.Context(), alertID, activeUser.UserID); err != nil {
		h.logger.Error("Failed to pause price alert", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Audit log
	entry := audit.NewLog(
		c,
		audit.CategoryPriceAlert,
		audit.EventPausePriceAlert,
		alertID.String(),
		"Price alert paused",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	h.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Price alert paused successfully", nil))
}

// ResumeAlert godoc
// @Summary Resume a price alert
// @Description Reactivates a paused price alert
// @Tags PriceAlerts
// @Produce json
// @Param alert_id path string true "Alert ID (UUID)"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/{alert_id}/resume [post]
// @Security BearerAuth
func (h *PriceAlertHandler) ResumeAlert(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	alertID, err := uuid.Parse(c.Param("alert_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid alert ID"))
		return
	}

	if err := h.alertService.ResumeAlert(c.Request.Context(), alertID, activeUser.UserID); err != nil {
		h.logger.Error("Failed to resume price alert", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Audit log
	entry := audit.NewLog(
		c,
		audit.CategoryPriceAlert,
		audit.EventResumePriceAlert,
		alertID.String(),
		"Price alert resumed",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	h.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Price alert resumed successfully", nil))
}

// GetAlertStats godoc
// @Summary Get alert statistics [fix needed]
// @Description Retrieves statistics about user's price alerts
// @Tags PriceAlerts
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/stats [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetAlertStats(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	stats, err := h.alertService.GetAlertStats(c.Request.Context(), activeUser.UserID)
	if err != nil {
		h.logger.Error("Failed to fetch alert stats", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch alert statistics"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", stats))
}

// GetAlertHistory godoc
// @Summary Get alert trigger history
// @Description Retrieves the history of when an alert was triggered
// @Tags PriceAlerts
// @Produce json
// @Param alert_id path string true "Alert ID (UUID)"
// @Param limit query int false "Number of records" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/history/{alert_id} [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetAlertHistory(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	alertID, err := uuid.Parse(c.Param("alert_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid alert ID"))
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	// Verify ownership
	_, err = h.alertService.GetAlert(c.Request.Context(), alertID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("alert not found"))
		return
	}

	// Get trigger history from database
	history, err := h.server.queries.GetAlertTriggerHistory(c.Request.Context(), db.GetAlertTriggerHistoryParams{
		AlertID: alertID,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
	if err != nil {
		h.logger.Error("Failed to fetch alert history", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch alert history"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"history": history,
		"limit":   limit,
		"offset":  offset,
	}))
}

// Admin endpoints

// GetAllAlerts godoc
// @Summary Get all alerts (Admin)
// @Description Retrieves all price alerts across all users
// @Tags PriceAlerts
// @Produce json
// @Param limit query int false "Number of records" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/admin/all [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetAllAlerts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("unauthorized access"))
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	alerts, err := h.server.queries.GetAllPriceAlerts(c.Request.Context(), db.GetAllPriceAlertsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		h.logger.Error("Failed to fetch all alerts", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch alerts"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"alerts": alerts,
		"limit":  limit,
		"offset": offset,
	}))
}

// GetSchedulerStats godoc
// @Summary Get scheduler statistics (Admin)
// @Description Retrieves performance metrics for the alert scheduler
// @Tags PriceAlerts
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/admin/scheduler/stats [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetSchedulerStats(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("unauthorized access"))
		return
	}

	stats := h.alertScheduler.GetStats(c.Request.Context())
	c.JSON(http.StatusOK, basemodels.NewSuccess("", stats))
}

// TriggerManualCheck godoc
// @Summary Trigger manual alert check (Admin)
// @Description Manually triggers the alert checking process
// @Tags PriceAlerts
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/admin/scheduler/trigger [post]
// @Security BearerAuth
func (h *PriceAlertHandler) TriggerManualCheck(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("unauthorized access"))
		return
	}

	if err := h.alertScheduler.TriggerManualCheck(c.Request.Context()); err != nil {
		h.logger.Error("Failed to trigger manual check", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to trigger manual check"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Manual alert check triggered successfully", nil))
}

// GetSystemMetrics godoc
// @Summary Get system-wide metrics (Admin)
// @Description Retrieves comprehensive metrics about the alert system
// @Tags PriceAlerts
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/price-alerts/admin/metrics [get]
// @Security BearerAuth
func (h *PriceAlertHandler) GetSystemMetrics(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("unauthorized access"))
		return
	}

	metrics := h.alertScheduler.GetMetrics()

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"scheduler_metrics": metrics,
		"timestamp":         time.Now(),
	}))
}
