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
	chatsupport "github.com/SwiftFiat/SwiftFiat-Backend/services/chat_support"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type SupportAdmin struct {
	server       *Server
	adminService *chatsupport.SupportAdminService
	audit        *audit.Service
}

func (sa SupportAdmin) router(server *Server) {
	sa.server = server
	sa.adminService = server.supportService
	sa.audit = server.auditService

	// Support Admin Management endpoints (super_admin/admin only)
	adminGroup := server.router.Group("/api/v1/supports")
	adminGroup.Use(sa.server.authMiddleware.AuthenticatedMiddleware())
	{
		adminGroup.POST("/", sa.createSupportAdmin)
		adminGroup.GET("/", sa.listAllSupportAdmins)
		adminGroup.GET("/:adminId", sa.getSupportAdmin)
		adminGroup.PATCH("/:adminId/status", sa.updateAdminStatus)
		adminGroup.GET("/workload", sa.getAdminWorkload)
		adminGroup.GET("/available", sa.getAvailableAdmins)

		// Support Agent Self-Service endpoints
		adminGroup.GET("/profile", sa.getMyProfile)
		adminGroup.PATCH("/status", sa.updateMyStatus)
		adminGroup.GET("/metrics", sa.getMyMetrics)
	}
}

// createSupportAdmin godoc
// @Summary Create Support Admin Profile
// @Description Create a support admin profile for a user (admin only)
// @Tags support
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param adminRequest body object{user_id=int64,max_concurrent_tickets=int32} true "Support Admin Creation Request"
// @Success 201 {object} basemodels.SuccessResponse{data=db.SupportAdmin}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/supports [post]
func (sa *SupportAdmin) createSupportAdmin(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.SUPER_ADMIN && activeUser.Role != models.ADMIN {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("only super_admin or admin can create support admin profiles"))
		return
	}

	var req struct {
		UserID               int64 `json:"user_id" binding:"required"`
		MaxConcurrentTickets int32 `json:"max_concurrent_tickets"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	// Validate user exists and has appropriate role
	user, err := sa.server.queries.GetUserByID(ctx, req.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("user not found"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to validate user"))
		return
	}

	// Check if user has admin or customer_rep role
	if user.Role != models.CUSTOMER_REP {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("user must have admin or customer_rep role"))
		return
	}

	// Set default max concurrent tickets if not provided
	maxTickets := req.MaxConcurrentTickets
	if maxTickets == 0 {
		maxTickets = 5
	}

	supportAdmin, err := sa.adminService.CreateSupportAdmin(ctx, &chatsupport.CreateSupportAdminParams{
		UserID:               req.UserID,
		MaxConcurrentTickets: maxTickets,
	})
	if err != nil {
		if err == chatsupport.ErrAdminAlreadyExists {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("support admin profile already exists"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create support admin"))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventSupportAdminCreated,
		fmt.Sprint(supportAdmin.ID),
		fmt.Sprintf("%s %d created support admin profile for admin %d", activeUser.Role, activeUser.UserID, req.UserID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	sa.audit.Log(logentry)

	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("support admin created successfully", supportAdmin))
}

// listAllSupportAdmins godoc
// @Summary List All Support Admins
// @Description Retrieve all support admin profiles with user details
// @Tags support
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=[]chatsupport.ListAllSupportAdminsRowResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/supports [get]
func (sa *SupportAdmin) listAllSupportAdmins(ctx *gin.Context) {
	// activeUser, err := utils.GetActiveUser(ctx)
	// if err != nil {
	// 	sa.server.logger.Error(err.Error())
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	// if activeUser.Role == models.USER || activeUser.Role == models.CUSTOMER_REP {
	// 	ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
	// 	return
	// }

	admins, err := sa.adminService.ListAllAdmins(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve support admins"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("support admins retrieved", gin.H{
		"admins": admins,
		"count":  len(admins),
	}))
}

// getSupportAdmin godoc
// @Summary Get Support Admin Details
// @Description Retrieve detailed information about a specific support admin
// @Tags support
// @Produce json
// @Security BearerAuth
// @Param adminId path string true "Support Admin ID"
// @Success 200 {object} basemodels.SuccessResponse{data=db.SupportAdmin}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/supports/{adminId} [get]
func (sa *SupportAdmin) getSupportAdmin(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	adminIDStr := ctx.Param("adminId")
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid admin ID"))
		return
	}

	admin, err := sa.adminService.GetSupportAdminByID(ctx, adminID)
	if err != nil {
		if err == chatsupport.ErrAdminNotFound {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("support admin not found"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve support admin"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("support admin retrieved", admin))
}

// updateAdminStatus godoc
// @Summary Update Support Admin Status
// @Description Update the online/offline/busy status of a support admin
// @Tags support
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param adminId path string true "Support Admin ID"
// @Param statusRequest body object{status=string} true "Status update request (online, offline, busy)"
// @Success 200 {object} basemodels.SuccessResponse{data=db.SupportAdmin}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/supports/{adminId}/status [patch]
func (sa *SupportAdmin) updateAdminStatus(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.SUPER_ADMIN && activeUser.Role != models.ADMIN {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	adminIDStr := ctx.Param("adminId")
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid admin ID"))
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	admin, err := sa.adminService.UpdateAdminStatus(ctx, adminID, req.Status)
	if err != nil {
		if err == chatsupport.ErrInvalidStatus {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid status. Must be: online, offline, or busy"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update status"))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventAdminStatusUpdated,
		fmt.Sprint(adminID),
		fmt.Sprintf("Admin %d updated support admin %d status to %s", activeUser.UserID, adminID, req.Status),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	sa.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("status updated successfully", admin))
}

// getAdminWorkload godoc
// @Summary Get Admin Workload
// @Description Retrieve workload distribution across all support admins
// @Tags support
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=[]chatsupport.GetAdminWorkloadRowResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/supports/workload [get]
func (sa *SupportAdmin) getAdminWorkload(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	workload, err := sa.adminService.GetAdminWorkload(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve workload"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("workload retrieved", workload))
}

// getAvailableAdmins godoc
// @Summary Get Available Support Admins
// @Description Retrieve support admins available to take new tickets
// @Tags support
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Limit" default(10)
// @Success 200 {object} basemodels.SuccessResponse{data=[]db.SupportAdmin}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/supports/available [get]
func (sa *SupportAdmin) getAvailableAdmins(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))

	admins, err := sa.adminService.ListAvailableAdmins(ctx, int32(limit))
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve available admins"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("available admins retrieved", gin.H{
		"admins": admins,
		"count":  len(admins),
	}))
}

// getMyProfile godoc
// @Summary Get My Support Agent Profile
// @Description Retrieve the authenticated user's support admin profile
// @Tags support-agent
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=db.SupportAdmin}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/support-agent/profile [get]
func (sa *SupportAdmin) getMyProfile(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.CUSTOMER_REP {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("only support agents can access this endpoint"))
		return
	}

	profile, err := sa.adminService.GetSupportAdminByUserID(ctx, activeUser.UserID)
	if err != nil {
		if err == chatsupport.ErrAdminNotFound {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("support admin profile not found"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve profile"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("profile retrieved", profile))
}

// updateMyStatus godoc
// @Summary Update My Status
// @Description Update the authenticated support agent's online/offline/busy status
// @Tags support-agent
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param statusRequest body object{status=string} true "Status update request (online, offline, busy)"
// @Success 200 {object} basemodels.SuccessResponse{data=db.SupportAdmin}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/support-agent/status [patch]
func (sa *SupportAdmin) updateMyStatus(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.CUSTOMER_REP {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("only support agents can access this endpoint"))
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	// Get support admin profile
	profile, err := sa.adminService.GetSupportAdminByUserID(ctx, activeUser.UserID)
	if err != nil {
		if err == chatsupport.ErrAdminNotFound {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("support admin profile not found"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve profile"))
		return
	}

	updatedProfile, err := sa.adminService.UpdateAdminStatus(ctx, profile.ID, req.Status)
	if err != nil {
		if err == chatsupport.ErrInvalidStatus {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid status. Must be: online, offline, or busy"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update status"))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventAdminStatusUpdated,
		strconv.FormatInt(profile.ID, 10),
		fmt.Sprintf("Agent %d updated status to %s", activeUser.UserID, req.Status),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	sa.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("status updated successfully", updatedProfile))
}

// getMyMetrics godoc
// @Summary Get My Performance Metrics
// @Description Retrieve performance metrics for the authenticated support agent
// @Tags support-agent
// @Produce json
// @Security BearerAuth
// @Param start_date query string false "Start date (YYYY-MM-DD)" default("2024-01-01")
// @Param end_date query string false "End date (YYYY-MM-DD)" default("2024-12-31")
// @Success 200 {object} basemodels.SuccessResponse{data=[]chatsupport.AgentMetricResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/support-agent/metrics [get]
func (sa *SupportAdmin) getMyMetrics(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.ADMIN && activeUser.Role != models.CUSTOMER_REP {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("only support agents can access this endpoint"))
		return
	}

	// Get support admin profile
	profile, err := sa.adminService.GetSupportAdminByUserID(ctx, activeUser.UserID)
	if err != nil {
		if err == chatsupport.ErrAdminNotFound {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("support admin profile not found"))
			return
		}
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve profile"))
		return
	}

	// Parse date range
	startDate := ctx.DefaultQuery("start_date", "2024-01-01")
	endDate := ctx.DefaultQuery("end_date", "2024-12-31")

	startTime, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid start_date format"))
		return
	}

	endTime, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid end_date format"))
		return
	}

	metrics, err := sa.server.queries.ListAgentMetricsByDateRange(ctx, db.ListAgentMetricsByDateRangeParams{
		SupportAdminID: profile.ID,
		Date:           startTime,
		Date_2:         endTime,
	})
	if err != nil {
		sa.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve metrics"))
		return
	}

	var metricsResponse []chatsupport.AgentMetricResponse
	for _, metric := range metrics {
		metricsResponse = append(metricsResponse, chatsupport.MapAgentMetricToResponse(metric))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("metrics retrieved", gin.H{
		"metrics": metricsResponse,
		"period": gin.H{
			"start": startDate,
			"end":   endDate,
		},
	}))
}
