package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/rewards"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type Rewards struct {
	server        *Server
	rewardService *rewards.RewardService
}

func (r Rewards) router(server *Server) {
	r.server = server
	r.rewardService = server.rewardService

	// User endpoints
	rewards := server.router.Group("/api/v1/rewards")
	rewards.Use(r.server.authMiddleware.AuthenticatedMiddleware())
	{
		rewards.GET("/balance", r.getUserRewardBalance)
		rewards.GET("/summary", r.getUserRewardSummary)
		rewards.GET("/history", r.getUserRewardHistory)
		rewards.GET("/activity", r.getRecentRewardActivity)
	}

	// Admin endpoints
	admin := server.router.Group("/api/v1/admin/rewards")
	admin.Use(r.server.authMiddleware.AuthenticatedMiddleware())
	{
		// Configuration management
		admin.POST("/configure", r.createRewardConfiguration)
		admin.GET("/config", r.getRewardConfiguration)
		admin.GET("/configurations", r.listRewardConfigurations)
		admin.PUT("/configure/:id", r.updateRewardConfiguration)
		admin.POST("/configure/:id/activate", r.activateRewardConfiguration)
		admin.POST("/configure/:id/deactivate", r.deactivateRewardConfiguration)

		// Analytics
		admin.GET("/statistics", r.getRewardStatistics)
		admin.GET("/top-users", r.getTopRewardUsers)
	}
}

// ============================================================================
// REWARD POINTS API HANDLERS
// ============================================================================
// These handlers provide REST API endpoints for the reward points system
// Add these routes to your server.go setupRouter() function
// ============================================================================

// ==============================================================
// USER ENDPOINTS
// ==============================================================

// getUserRewardBalance godoc
// @Summary Get user reward balance
// @Description Get current reward points balance and summary for authenticated user
// @Tags rewards
// @Accept json
// @Produce json
// @Success 200 {object} rewards.RewardBalanceResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/rewards/balance [get]
// @Security BearerAuth
func (r *Rewards) getUserRewardBalance(ctx *gin.Context) {
	// Get authenticated user from context
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	balance, err := r.rewardService.GetUserRewardBalance(ctx.Request.Context(), int32(activeUser.UserID))
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("balance fetched successfully", balance))
}

// getUserRewardSummary godoc
// @Summary Get comprehensive reward summary
// @Description Get detailed reward summary including balance, totals, and transaction counts
// @Tags rewards
// @Accept json
// @Produce json
// @Success 200 {object} rewards.RewardSummaryResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/rewards/summary [get]
// @Security BearerAuth
func (r *Rewards) getUserRewardSummary(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	summary, err := r.rewardService.GetUserRewardSummary(ctx.Request.Context(), activeUser.UserID)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, summary)
}

// getUserRewardHistory godoc
// @Summary Get reward transaction history
// @Description Get paginated list of reward transactions with optional filters
// @Tags rewards
// @Accept json
// @Produce json
// @Param type query string false "Transaction type (earned/redeemed)"
// @Param date_from query string false "Start date (RFC3339)"
// @Param date_to query string false "End date (RFC3339)"
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Success 200 {object} rewards.RewardHistoryResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/rewards/history [get]
// @Security BearerAuth
func (r *Rewards) getUserRewardHistory(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Parse query parameters
	var filter rewards.RewardHistoryFilter
	if err := ctx.ShouldBindQuery(&filter); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid query parameters"))
		return
	}

	// Parse date filters if provided
	if dateFromStr := ctx.Query("date_from"); dateFromStr != "" {
		dateFrom, err := time.Parse(time.RFC3339, dateFromStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date_from format. Use RFC3339"})
			return
		}
		filter.DateFrom = &dateFrom
	}

	if dateToStr := ctx.Query("date_to"); dateToStr != "" {
		dateTo, err := time.Parse(time.RFC3339, dateToStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date_to format. Use RFC3339"})
			return
		}
		filter.DateTo = &dateTo
	}

	// Validate transaction type if provided
	if filter.Type != "" && filter.Type != "earned" && filter.Type != "redeemed" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid type. Must be 'earned' or 'redeemed'"})
		return
	}

	history, err := r.rewardService.GetUserRewardHistory(ctx.Request.Context(), int32(activeUser.UserID), filter)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, history)
}

func (r *Rewards) getRecentRewardActivity(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	limit := 10
	if limitStr := ctx.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
			limit = l
		}
	}

	activity, err := r.rewardService.GetRecentRewardActivity(ctx.Request.Context(), int32(activeUser.UserID), limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, activity)
}

// ==============================================================
// ADMIN ENDPOINTS - CONFIGURATION MANAGEMENT
// ==============================================================

// createRewardConfiguration godoc
// @Summary Create reward configuration (Admin)
// @Description Create a new reward configuration to define earning rates
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Param request body rewards.CreateRewardConfigRequest true "Configuration details"
// @Success 201 {object} rewards.RewardConfigurationResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/configure [post]
// @Security BearerAuth
func (r *Rewards) createRewardConfiguration(ctx *gin.Context) {
	// Verify admin role
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req rewards.CreateRewardConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request payload"))
		return
	}

	// Validate reward rate
	if req.RewardRate <= 0 || req.RewardRate > 1 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Reward rate must be between 0 and 1 (0% to 100%)"})
		return
	}

	config, err := r.rewardService.CreateRewardConfiguration(ctx.Request.Context(), req, int32(activeUser.UserID))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusCreated, config)
}

// getRewardConfiguration godoc
// @Summary Get reward configuration (Admin)
// @Description Get current active reward configuration
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Success 200 {object} rewards.RewardConfigurationResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/config [get]
// @Security BearerAuth
func (r *Rewards) getRewardConfiguration(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// Get transaction type from query, default to bill_payment
	transactionType := ctx.DefaultQuery("transaction_type", "bill_payment")

	config, err := r.server.queries.GetActiveRewardConfiguration(ctx.Request.Context(), transactionType)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "No active configuration found"})
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, rewards.MapRewardConfigToResponse(&config))
}

// listRewardConfigurations godoc
// @Summary List all reward configurations (Admin)
// @Description Get paginated list of all reward configurations
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Success 200 {array} []rewards.RewardConfigurationResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/configurations [get]
// @Security BearerAuth
func (r *Rewards) listRewardConfigurations(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	page := 1
	pageSize := 20

	if pageStr := ctx.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if pageSizeStr := ctx.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	offset := (page - 1) * pageSize

	configs, err := r.server.queries.ListRewardConfigurations(ctx.Request.Context(), db.ListRewardConfigurationsParams{
		Limit:  int32(pageSize),
		Offset: int32(offset),
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var responseConfigs []rewards.RewardConfigurationResponse
	for _, config := range configs {
		responseConfigs = append(responseConfigs, *rewards.MapRewardConfigToResponse(&config))
	}

	ctx.JSON(http.StatusOK, responseConfigs)
}

// updateRewardConfiguration godoc
// @Summary Update reward configuration (Admin)
// @Description Update an existing reward configuration
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Param id path int true "Configuration ID"
// @Param request body rewards.UpdateRewardConfigRequest true "Updated configuration"
// @Success 200 {object} rewards.RewardConfigurationResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/configure/{id} [put]
// @Security BearerAuth
func (r *Rewards) updateRewardConfiguration(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	configID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid configuration ID"})
		return
	}

	var req rewards.UpdateRewardConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	rewardRate, err := decimal.NewFromString(*req.RewardRate)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// Validate reward rate if provided
	if req.RewardRate != nil && (rewardRate.LessThanOrEqual(decimal.Zero) || rewardRate.GreaterThan(decimal.NewFromInt(1))) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Reward rate must be between 0 and 1 (0% to 100%)"})
		return
	}

	config, err := r.rewardService.UpdateRewardConfiguration(ctx.Request.Context(), configID, req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("", config))
}

// activateRewardConfiguration godoc
// @Summary Activate reward configuration (Admin)
// @Description Activate a reward configuration
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Param id path int true "Configuration ID"
// @Success 200 {object} rewards.RewardConfigurationResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/configure/{id}/activate [post]
// @Security BearerAuth
func (r *Rewards) activateRewardConfiguration(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	configID, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid configuration ID"})
		return
	}

	config, err := r.server.queries.ActivateRewardConfiguration(ctx.Request.Context(), int64(configID))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, rewards.MapRewardConfigToResponse(&config))
}

// deactivateRewardConfiguration godoc
// @Summary Deactivate reward configuration (Admin)
// @Description Deactivate a reward configuration
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Param id path int true "Configuration ID"
// @Success 200 {object} rewards.RewardConfigurationResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/configure/{id}/deactivate [post]
// @Security BearerAuth
func (r *Rewards) deactivateRewardConfiguration(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	configID, err := strconv.Atoi(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid configuration ID"})
		return
	}

	config, err := r.server.queries.DeactivateRewardConfiguration(ctx.Request.Context(), int64(configID))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, rewards.MapRewardConfigToResponse(&config))
}

// ==============================================================
// ADMIN ENDPOINTS - ANALYTICS
// ==============================================================

// getRewardStatistics godoc
// @Summary Get reward system statistics (Admin)
// @Description Get comprehensive statistics about the reward system
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Success 200 {object} []rewards.GetTopUsersByRewardsEarnedRow
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/rewards/statistics [get]
// @Security BearerAuth
func (r *Rewards) getTopRewardUsers(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	limit := 10
	if limitStr := ctx.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	users, err := r.server.queries.GetTopUsersByRewardsEarned(ctx.Request.Context(), int32(limit))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var usersResponse []rewards.GetTopUsersByRewardsEarnedRow
	for _, user := range users {
		usersResponse = append(usersResponse, *rewards.MapGetTopUsersByRewardsEarnedRowToResponse(&user))
	}

	ctx.JSON(http.StatusOK, usersResponse)
}

func (r *Rewards) getRewardStatistics(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		r.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	stats, err := r.server.rewardService.GetRewardStatistics(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, stats)
}
