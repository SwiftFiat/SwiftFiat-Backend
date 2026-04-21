package api

import (
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/streaks"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Streaks handles all streak-related API endpoints
type Streaks struct {
	server        *Server
	streakService *streaks.StreakService
}

// router sets up all streak routes
func (st Streaks) router(server *Server) {
	st.server = server
	st.streakService = streaks.NewStreakService(st.server.queries, st.server.logger)

	// API v1 routes
	serverGroupV1 := server.router.Group("/api/v1/streaks")
	serverGroupV1.Use(st.server.authMiddleware.AuthenticatedMiddleware())

	// User streak endpoints
	serverGroupV1.GET("/dashboard", st.getStreakDashboard)
	serverGroupV1.GET("/stats", st.getUserStreakStats)
	serverGroupV1.GET("/history", st.getStreakHistory)

	// Badge endpoints
	serverGroupV1.GET("/badges", st.getUserBadges)
	serverGroupV1.GET("/badges/available", st.getAvailableBadges)
	serverGroupV1.GET("/badges/progress", st.getBadgeProgress)

	// Leaderboard endpoints
	serverGroupV1.GET("/leaderboard", st.getStreakLeaderboard)
	serverGroupV1.GET("/leaderboard/badges", st.getBadgeLeaderboard)

	// Platform statistics (public or authenticated)
	serverGroupV1.GET("/platform-stats", st.getPlatformStatistics)

	// Admin endpoints
	adminGroup := server.router.Group("/api/v1/admin/streaks")
	adminGroup.Use(st.server.authMiddleware.AuthenticatedMiddleware())

	adminGroup.POST("/recalculate/:user_id", st.recalculateUserStreak)
	adminGroup.POST("/reset-broken", st.resetBrokenStreaks)
	adminGroup.GET("/health", st.getSystemHealth)
	adminGroup.GET("/analytics", st.getAnalytics)
}

// ===============================================
// USER STREAK ENDPOINTS
// ===============================================

// getStreakDashboard returns comprehensive streak dashboard
// @Summary Get user's streak dashboard
// @Description Retrieves complete streak information including current streak, badges, and next milestones
// @Tags Streaks
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse{data=streaks.StreakDashboard}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/dashboard [get]
func (st *Streaks) getStreakDashboard(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	dashboard, err := st.streakService.GetUserStreakDashboard(ctx, activeUser.UserID)
	if err != nil {
		st.server.logger.Error("failed to fetch streak dashboard", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("streak dashboard fetched successfully", dashboard))
}

// getUserStreakStats returns basic streak statistics
// @Summary Get user's streak stats
// @Description Returns simplified streak statistics (current, best, total days)
// @Tags Streaks
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/stats [get]
func (st *Streaks) getUserStreakStats(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	streak, err := st.server.queries.GetUserStreak(ctx, activeUser.UserID)
	if err != nil {
		st.server.logger.Error("failed to fetch streak stats", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	response := map[string]interface{}{
		"current_streak":         streak.CurrentStreak,
		"best_streak":            streak.BestStreak,
		"total_transaction_days": streak.TotalTransactionDays,
		"last_transaction_date":  streak.LastTransactionDate,
		"streak_started_at":      streak.StreakStartedAt,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("streak stats fetched successfully", response))
}

// getStreakHistory returns historical streak changes
// @Summary Get streak history
// @Description Returns paginated history of streak changes and events
// @Tags Streaks
// @Produce json
// @Param limit query int false "Number of records" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/history [get]
func (st *Streaks) getStreakHistory(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "20"), 10, 32)
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 32)

	history, err := st.server.queries.GetUserStreakHistory(ctx, db.GetUserStreakHistoryParams{
		UserID: activeUser.UserID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		st.server.logger.Error("failed to fetch streak history", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("streak history fetched successfully", history))
}

// ===============================================
// BADGE ENDPOINTS
// ===============================================

// getUserBadges returns all badges earned by user
// @Summary Get user's badges
// @Description Returns all badges earned by the authenticated user
// @Tags Streaks/Badges
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/badges [get]
func (st *Streaks) getUserBadges(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	badges, err := st.server.queries.GetUserBadges(ctx, activeUser.UserID)
	if err != nil {
		st.server.logger.Error("failed to fetch user badges", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	response := map[string]interface{}{
		"total_badges": len(badges),
		"badges":       badges,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user badges fetched successfully", response))
}

// getAvailableBadges returns all badges in the system
// @Summary Get all available badges
// @Description Returns list of all badges that can be earned
// @Tags Streaks/Badges
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/badges/available [get]
func (st *Streaks) getAvailableBadges(ctx *gin.Context) {
	badges, err := st.streakService.GetAllBadges(ctx)
	if err != nil {
		st.server.logger.Error("failed to fetch available badges", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("available badges fetched successfully", badges))
}

// getBadgeProgress returns user's progress on all badges
// @Summary Get badge progress
// @Description Returns all badges with lock/unlock status and progress percentage
// @Tags Streaks/Badges
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse{data=[]streaks.BadgeWithStatus}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/badges/progress [get]
func (st *Streaks) getBadgeProgress(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	badges, err := st.streakService.GetUserBadgesWithProgress(ctx, activeUser.UserID)
	if err != nil {
		st.server.logger.Error("failed to fetch badge progress", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Separate locked and unlocked
	locked := []streaks.BadgeWithStatus{}
	unlocked := []streaks.BadgeWithStatus{}

	for _, badge := range badges {
		if badge.IsUnlocked {
			unlocked = append(unlocked, badge)
		} else {
			locked = append(locked, badge)
		}
	}

	response := map[string]interface{}{
		"unlocked_count": len(unlocked),
		"locked_count":   len(locked),
		"unlocked":       unlocked,
		"locked":         locked,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("badge progress fetched successfully", response))
}

// ===============================================
// LEADERBOARD ENDPOINTS
// ===============================================

// getStreakLeaderboard returns top users by streak
// @Summary Get streak leaderboard
// @Description Returns ranked list of users with highest current streaks
// @Tags Streaks/Leaderboards
// @Produce json
// @Param limit query int false "Number of users" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]streaks.LeaderboardEntry}
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/leaderboard [get]
func (st *Streaks) getStreakLeaderboard(ctx *gin.Context) {
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "50"), 10, 32)
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 32)

	leaderboard, err := st.streakService.GetStreakLeaderboard(ctx, int32(limit), int32(offset))
	if err != nil {
		st.server.logger.Error("failed to fetch streak leaderboard", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Get authenticated user's rank if logged in
	activeUser, _ := utils.GetActiveUser(ctx)
	var userRank *streaks.LeaderboardEntry
	if activeUser.UserID != uuid.Nil {
		// Find user in leaderboard or calculate separately
		for _, entry := range leaderboard {
			if entry.UserID == activeUser.UserID {
				userRank = &entry
				break
			}
		}
	}

	response := map[string]interface{}{
		"leaderboard": leaderboard,
		"user_rank":   userRank,
		"total_shown": len(leaderboard),
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("streak leaderboard fetched successfully", response))
}

// getBadgeLeaderboard returns users with most badges
// @Summary Get badge leaderboard
// @Description Returns ranked list of users with most badges earned
// @Tags Streaks/Leaderboards
// @Produce json
// @Param limit query int false "Number of users" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/leaderboard/badges [get]
func (st *Streaks) getBadgeLeaderboard(ctx *gin.Context) {
	limit, _ := strconv.ParseInt(ctx.DefaultQuery("limit", "50"), 10, 32)
	offset, _ := strconv.ParseInt(ctx.DefaultQuery("offset", "0"), 10, 32)

	leaderboard, err := st.server.queries.GetBadgeLeaderboard(ctx, db.GetBadgeLeaderboardParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		st.server.logger.Error("failed to fetch badge leaderboard", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("badge leaderboard fetched successfully", leaderboard))
}

// ===============================================
// STATISTICS ENDPOINTS
// ===============================================

// getPlatformStatistics returns platform-wide metrics
// @Summary Get platform statistics
// @Description Returns aggregate statistics about streaks across all users
// @Tags Streaks/Statistics
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse{data=streaks.StreakStatistics}
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/streaks/platform-stats [get]
func (st *Streaks) getPlatformStatistics(ctx *gin.Context) {
	stats, err := st.streakService.GetPlatformStatistics(ctx)
	if err != nil {
		st.server.logger.Error("failed to fetch platform statistics", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Get badge distribution
	badgeDistribution, err := st.server.queries.GetBadgeDistribution(ctx)
	if err != nil {
		st.server.logger.Warn("failed to fetch badge distribution", err)
		badgeDistribution = []db.GetBadgeDistributionRow{}
	}

	response := map[string]interface{}{
		"streak_stats":       stats,
		"badge_distribution": badgeDistribution,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("platform statistics fetched successfully", response))
}

// ===============================================
// ADMIN ENDPOINTS
// ===============================================

// recalculateUserStreak recalculates streak from transaction history
// @Summary Recalculate user streak (Admin)
// @Description Recalculates streak values from transaction history (for fixing data issues)
// @Tags Streaks/Admin
// @Produce json
// @Param user_id path int true "User ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/streaks/recalculate/{user_id} [post]
func (st *Streaks) recalculateUserStreak(ctx *gin.Context) {
	userIDStr := ctx.Param("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid user ID"))
		return
	}

	streak, err := st.streakService.RecalculateUserStreak(ctx, userID)
	if err != nil {
		st.server.logger.Error("failed to recalculate streak", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("streak recalculated successfully", streak))
}

// resetBrokenStreaks resets all broken streaks
// @Summary Reset broken streaks (Admin)
// @Description Manually trigger reset of all broken streaks (normally done by cron)
// @Tags Streaks/Admin
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/streaks/reset-broken [post]
func (st *Streaks) resetBrokenStreaks(ctx *gin.Context) {
	err := st.streakService.ResetBrokenStreaks(ctx)
	if err != nil {
		st.server.logger.Error("failed to reset broken streaks", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("broken streaks reset successfully", nil))
}

// getSystemHealth returns system health metrics
// @Summary Get system health (Admin)
// @Description Returns health metrics for the streaks system
// @Tags Streaks/Admin
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/streaks/health [get]
func (st *Streaks) getSystemHealth(ctx *gin.Context) {
	health, err := st.streakService.GetSystemHealthCheck(ctx)
	if err != nil {
		st.server.logger.Error("failed to fetch system health", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("system health fetched successfully", health))
}

// getAnalytics returns detailed analytics
// @Summary Get streak analytics (Admin)
// @Description Returns comprehensive analytics about streak engagement
// @Tags Streaks/Admin
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/streaks/analytics [get]
func (st *Streaks) getAnalytics(ctx *gin.Context) {
	// Get streak distribution
	distribution, err := st.server.queries.GetStreakDistribution(ctx)
	if err != nil {
		st.server.logger.Error("failed to fetch streak distribution", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Get retention rate
	retention, err := st.server.queries.GetStreakRetentionRate(ctx)
	if err != nil {
		st.server.logger.Warn("failed to fetch retention rate", err)
	}

	// Get daily active users
	dau, err := st.server.queries.GetDailyActiveUsers(ctx)
	if err != nil {
		st.server.logger.Warn("failed to fetch daily active users", err)
	}

	response := map[string]interface{}{
		"streak_distribution": distribution,
		"retention_rate":      retention,
		"daily_active_users":  dau,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("analytics fetched successfully", response))
}
