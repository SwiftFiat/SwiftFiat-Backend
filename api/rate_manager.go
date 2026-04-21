package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	ratemanager "github.com/SwiftFiat/SwiftFiat-Backend/services/rate_manager"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type RateManagerHandler struct {
	server  *Server
	service *ratemanager.Service
}

func (r RateManagerHandler) router(server *Server) {
	r.server = server
	r.service = server.rateManager

	v := server.router.Group("/api/v1/rate-manager")
	v.Use(server.authMiddleware.AuthenticatedMiddleware())
	v.GET("/my-vip-level", r.GetUserVIPLevel)
	v.GET("/current-rate", r.GetCurrentRateWithAdjustment)

	v.POST("/admin/vip-levels", r.CreateVIPLevel)
	v.GET("/admin/vip-levels/:id", r.GetVIPLevel)
	v.GET("/admin/vip-levels", r.ListVIPLevels)
	v.PUT("/admin/vip-levels/:id", r.UpdateVIPLevel)
	v.DELETE("/admin/vip-levels/:id", r.DeleteVIPLevel)

	// rules
	v.POST("/admin/rules", r.CreateRateAdjustmentRule)
	v.GET("/admin/rules/:id", r.GetRateAdjustmentRule)
	v.GET("/admin/rules", r.ListRateAdjustmentRules)
	v.PUT("/admin/rules/:id", r.UpdateRateAdjustmentRule)
	v.DELETE("/admin/rules/:id", r.DeleteRateAdjustmentRule)
	v.POST("/admin/simulate", r.SimulateRateAdjustment)
	v.POST("/admin/vip-assignments", r.AssignUserToVIPLevel)
}

// CreateVIPLevel godoc
// @Summary Create VIP level
// @Description Create a new VIP level with transaction thresholds
// @Tags Rate Manager - VIP Levels
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ratemanager.CreateVIPLevelRequest true "VIP Level creation request"
// @Success 201 {object} ratemanager.VIPLevel
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 409 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/vip-levels [post]
func (r *RateManagerHandler) CreateVIPLevel(c *gin.Context) {
	var req ratemanager.CreateVIPLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(500, basemodels.NewError("Failed to get user"))
		return
	}

	vipLevel, err := r.service.CreateVIPLevel(c.Request.Context(), &req, &user)
	if err != nil {
		r.server.logger.Errorf("failed to create vip level: %v", err)
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusCreated, basemodels.NewSuccess("vip level created", vipLevel))
}

// GetVIPLevel godoc
// @Summary Get VIP level
// @Description Get VIP level details by ID
// @Tags Rate Manager - VIP Levels
// @Produce json
// @Security BearerAuth
// @Param id path string true "VIP Level ID"
// @Success 200 {object} ratemanager.VIPLevelResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/vip-levels/{id} [get]
func (r *RateManagerHandler) GetVIPLevel(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	vipLevel, err := r.service.GetVIPLevel(c.Request.Context(), id)
	if err != nil {
		r.server.logger.Errorf("failed to get vip level: %v", err)
		c.JSON(500, basemodels.NewError("Failed to get vip levels"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", vipLevel))
}

// ListVIPLevels godoc
// @Summary List VIP levels
// @Description Get all VIP levels with optional filtering
// @Tags Rate Manager - VIP Levels
// @Produce json
// @Security BearerAuth
// @Param active_only query bool false "Filter active levels only"
// @Success 200 {array} ratemanager.VIPLevelResponse
// @Router /admin/rate-manager/vip-levels [get]
func (r *RateManagerHandler) ListVIPLevels(c *gin.Context) {
	// activeUser, err := utils.GetActiveUser(c)
	// if err != nil {
	// 	r.server.logger.Error(err.Error())
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	activeOnly := c.Query("active_only") == "true"

	vipLevels, err := r.service.ListVIPLevels(c.Request.Context(), activeOnly)
	if err != nil {
		r.server.logger.Errorf("failed to list VIP levels: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to list VIP levels"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", vipLevels))
}

// UpdateVIPLevel godoc
// @Summary Update VIP level
// @Description Update VIP level details
// @Tags Rate Manager - VIP Levels
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "VIP Level ID"
// @Param request body ratemanager.UpdateVIPLevelRequest true "VIP Level update request"
// @Success 200 {object} ratemanager.VIPLevel
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/vip-levels/{id} [put]
func (r *RateManagerHandler) UpdateVIPLevel(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid VIP level ID"))
		return
	}

	var req ratemanager.UpdateVIPLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user"))
		return
	}

	vipLevel, err := r.service.UpdateVIPLevel(c.Request.Context(), id, &req, &user)
	if err != nil {
		r.server.logger.Errorf("failed to update vip level: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update vip level"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("VIP level updated successfully", vipLevel))
}

// DeleteVIPLevel godoc
// @Summary Delete VIP level
// @Description Soft delete a VIP level
// @Tags Rate Manager - VIP Levels
// @Produce json
// @Security BearerAuth
// @Param id path string true "VIP Level ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/vip-levels/{id} [delete]
func (r *RateManagerHandler) DeleteVIPLevel(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid VIP level ID"))
		return
	}

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user"))
		return
	}

	if err := r.service.DeleteVIPLevel(c.Request.Context(), id, &user); err != nil {
		r.server.logger.Errorf("failed to dalete vip level: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to delete vi level"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("VIP level deleted successfully", nil))
}

// CreateRateAdjustmentRule godoc
// @Summary Create rate adjustment rule
// @Description Create a new rate adjustment rule for VIP levels or global
// @Tags Rate Manager - Rules
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ratemanager.CreateRateAdjustmentRuleRequest true "Rule creation request"
// @Success 201 {object} ratemanager.RateAdjustmentRule
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 409 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/rules [post]
func (r *RateManagerHandler) CreateRateAdjustmentRule(c *gin.Context) {
	var req ratemanager.CreateRateAdjustmentRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user"))
		return
	}

	rule, err := r.service.CreateRateAdjustmentRule(c.Request.Context(), &req, &user)
	if err != nil {
		r.server.logger.Errorf("failed to create adjustment rule: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create adjustment rule"))
		return
	}

	c.JSON(http.StatusCreated, basemodels.NewSuccess("Rate adjustment rule created successfully", rule))
}

// GetRateAdjustmentRule godoc
// @Summary Get rate adjustment rule
// @Description Get rate adjustment rule details by ID
// @Tags Rate Manager - Rules
// @Produce json
// @Security BearerAuth
// @Param id path string true "Rule ID"
// @Success 200 {object} ratemanager.RateAdjustmentRuleResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/rules/{id} [get]
func (r *RateManagerHandler) GetRateAdjustmentRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	rule, err := r.service.GetRateAdjustmentRule(c.Request.Context(), id)
	if err != nil {
		r.server.logger.Errorf("failed to fetch rule adjustment: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch rule adjustment"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", rule))
}

// ListRateAdjustmentRules godoc
// @Summary List rate adjustment rules
// @Description Get all rate adjustment rules with pagination
// @Tags Rate Manager - Rules
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Number of items per page" default(20)
// @Success 200 {object} ratemanager.PaginatedResponse
// @Router /api/v1/rate-manager/admin/rules [get]
func (r *RateManagerHandler) ListRateAdjustmentRules(c *gin.Context) {
	// activeUser, err := utils.GetActiveUser(c)
	// if err != nil {
	// 	r.server.logger.Error(err.Error())
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	limit, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || limit <= 0 {
		limit = 20
	}

	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	rules, err := r.server.queries.ListRateAdjustmentRules(c.Request.Context(), db.ListRateAdjustmentRulesParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		r.server.logger.Errorf("failed to list rate adjustment rules: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to list rate adjustment rules"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", rules))
}

// UpdateRateAdjustmentRule godoc
// @Summary Update rate adjustment rule
// @Description Update rate adjustment rule details
// @Tags Rate Manager - Rules
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Rule ID"
// @Param request body ratemanager.UpdateRateAdjustmentRuleRequest true "Rule update request"
// @Success 200 {object} ratemanager.RateAdjustmentRule
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/rules/{id} [put]
func (r *RateManagerHandler) UpdateRateAdjustmentRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	var req ratemanager.UpdateRateAdjustmentRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	r.server.logger.Info(fmt.Sprintf("UpdateRateAdjustmentRule request: %+v", req))

	rule, err := r.service.GetRateAdjustmentRule(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, basemodels.NewError("Rule not found"))
		return
	}

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user"))
		return
	}
	updatedRule, err := r.service.UpdateRateAdjustmentRule(c.Request.Context(), rule.ID, &req, &user)
	if err != nil {
		r.server.logger.Errorf("failed to update adjustment rule: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update adjustment rule"))
		return
	}

	r.server.logger.Info(fmt.Sprintf("UpdateRateAdjustmentRule updated rule: %+v", updatedRule))
	c.JSON(http.StatusOK, basemodels.NewSuccess("Rule updated successfully", updatedRule))
}

// DeleteRateAdjustmentRule godoc
// @Summary Delete rate adjustment rule
// @Description Soft delete a rate adjustment rule
// @Tags Rate Manager - Rules
// @Produce json
// @Security BearerAuth
// @Param id path string true "Rule ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/rules/{id} [delete]
func (r *RateManagerHandler) DeleteRateAdjustmentRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("apistrings.UnauthorizedAccess"))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user"))
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid rule id"))
		return
	}

	err = r.service.DeleteRateAdjustmentRule(c.Request.Context(), id, &user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("Rule deleted successfully", nil))
}

// SimulateRateAdjustment godoc
// @Summary Simulate rate adjustment
// @Description Preview rate adjustment before applying
// @Tags Rate Manager - Simulation
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ratemanager.RateSimulationRequest true "Simulation request"
// @Success 200 {object} ratemanager.RateSimulationResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /admin/rate-manager/simulate [post]
func (r *RateManagerHandler) SimulateRateAdjustment(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req ratemanager.RateSimulationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	simulation, err := r.service.SimulateRateAdjustment(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to simulate rate adjustment"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", simulation))
}

// GetCurrentRateWithAdjustment godoc
// @Summary Get current rate with adjustment
// @Description Get current exchange rate with VIP adjustment applied
// @Tags Rate Manager - Rates
// @Produce json
// @Security BearerAuth
// @Param from query string true "Source currency"
// @Param to query string true "Target currency"
// @Param amount query string true "Amount to convert"
// @Success 200 {object} ratemanager.RateSimulationResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /rate-manager/current-rate [get]
func (r *RateManagerHandler) GetCurrentRateWithAdjustment(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	from := c.Query("from")
	to := c.Query("to")
	amount := c.Query("amount")
	user_id := activeUser.UserID

	if from == "" || to == "" || amount == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Missing required parameters: from, to, amount"))
		return
	}

	rate, err := r.service.GetAdjustedRateForUser(c.Request.Context(), user_id, from, to, amount)
	if err != nil {
		r.server.logger.Errorf("failed to get adjusted rate: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to get adjusted rate"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", rate))
}

// AssignUserToVIPLevel godoc
// @Summary Assign user to VIP level
// @Description Manually assign a user to a VIP level
// @Tags Rate Manager - VIP Assignments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ratemanager.AssignVIPLevelRequest true "Assignment request"
// @Success 200 {object} ratemanager.UserVIPAssignmentResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router/rate-manager/admin/vip-assignments [post]
func (r *RateManagerHandler) AssignUserToVIPLevel(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	var req ratemanager.AssignVIPLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	user, err := r.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user"))
		return
	}

	assignment, err := r.service.AssignUserToVIPLevel(c.Request.Context(), &req, &user)
	if err != nil {
		r.server.logger.Errorf("failed to assign user tto vip level: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to assign user tto vip level"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("User assigned to VIP level successfully", assignment))
}

// GetUserVIPStatus godoc
// @Summary Get user VIP status
// @Description Get current VIP level and benefits for a user
// @Tags Rate Manager - VIP Assignments
// @Produce json
// @Security BearerAuth
// @Success 200 {object} ratemanager.UserVIPAssignmentResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /rate-manager/my-vip-level [get]
func (r *RateManagerHandler) GetUserVIPLevel(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		r.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// Use the service method which handles the new users table fields and auto-updates VIP levels
	vipStatus, err := r.service.GetUserVIPStatus(c.Request.Context(), activeUser.UserID)
	if err != nil {
		r.server.logger.Errorf("failed to get user vip status: %v", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user vip status"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("User vip status retrieved successfully", vipStatus))
}
