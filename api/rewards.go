package api

import (
	"net/http"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/rewards"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type Rewards struct {
	server        *Server
	rewardService *rewards.RewardService
}

func (r Rewards) router(server *Server) {
	r.server = server
	r.rewardService = r.server.rewardService
	rewards := server.router.Group("/rewards")
	rewards.Use(r.server.authMiddleware.AuthenticatedMiddleware())
	{
		// rewards.GET("/balance", server.getUserRewardBalance)
		// rewards.GET("/summary", server.getUserRewardSummary)
		// rewards.GET("/history", server.getUserRewardHistory)
		// rewards.GET("/activity", server.getRecentRewardActivity)
	}

	// Admin endpoints
	admin := server.router.Group("/admin/rewards")
	admin.Use(r.server.authMiddleware.AuthenticatedMiddleware())
	{
		// Configuration management
		// admin.POST("/configure", server.createRewardConfiguration)
		// admin.GET("/config", server.getRewardConfiguration)
		// admin.GET("/configurations", server.listRewardConfigurations)
		// admin.PUT("/configure/:id", server.updateRewardConfiguration)
		// admin.POST("/configure/:id/activate", server.activateRewardConfiguration)
		// admin.POST("/configure/:id/deactivate", server.deactivateRewardConfiguration)

		// // Analytics
		// admin.GET("/statistics", server.getRewardStatistics)
		// admin.GET("/top-users", server.getTopRewardUsers)
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
// @Success 200 {object} RewardBalanceResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
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
// @Success 200 {object} RewardSummaryResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
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
// @Success 200 {object} RewardHistoryResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
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

// ==============================================================
// ADMIN ENDPOINTS - CONFIGURATION MANAGEMENT
// ==============================================================

// createRewardConfiguration godoc
// @Summary Create reward configuration (Admin)
// @Description Create a new reward configuration to define earning rates
// @Tags admin,rewards
// @Accept json
// @Produce json
// @Param request body CreateRewardConfigRequest true "Configuration details"
// @Success 201 {object} RewardConfiguration
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/admin/rewards/configure [post]
// @Security BearerAuth
// func (r *Rewards) createRewardConfiguration(ctx *gin.Context) {
// 	// Verify admin role
// 	activeUser, err := utils.GetActiveUser(ctx)
// 	if err != nil {
// 		r.server.logger.Error(err.Error())
// 		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
// 		return
// 	}

// 	if activeUser.Role != models.USER{
// 		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
// 		return
// 	}

// 	var req rewards.CreateRewardConfigRequest
// 	if err := ctx.ShouldBindJSON(&req); err != nil {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request payload"))
// 		return
// 	}

// 	// Validate reward rate
// 	if req.RewardRate <= 0 || req.RewardRate > 1 {
// 		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Reward rate must be between 0 and 1 (0% to 100%)"})
// 		return
// 	}

// 	config, err := r.rewardService.CreateRewardConfiguration(ctx.Request.Context(), req, int32(activeUser.UserID))
// 	if err != nil {
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
// 		return
// 	}

// 	ctx.JSON(http.StatusCreated, config)
// }
