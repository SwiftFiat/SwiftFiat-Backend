package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	exchangerate "github.com/SwiftFiat/SwiftFiat-Backend/services/exchange_rate"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	smartconversion "github.com/SwiftFiat/SwiftFiat-Backend/services/smart_conversion"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SmartConvertHandler struct {
	server          *Server
	logger          *logging.Logger
	exchangeRateSvc *exchangerate.ExchangeRateService
	conversionSvc   *smartconversion.ConversionService
	audit           *audit.Service
}

func (s SmartConvertHandler) router(server *Server) {
	s.server = server
	s.logger = s.server.logger
	s.exchangeRateSvc = s.server.scExchangeRateservice
	s.conversionSvc = s.server.smartConvertService
	s.audit = s.server.auditService

	v1 := server.router.Group("/api/v1/smart-convert")
	v1.GET("/rates", s.GetExchangeRate)
	v1.Use(s.server.authMiddleware.AuthenticatedMiddleware())
	{
		v1.POST("/rules", s.CreateConversionRule)
		v1.GET("/rules", s.GetConversionRules)
		v1.POST("/rules/:rule_id/pause", s.PauseConversionRule)
		v1.POST("/rules/:rule_id/resume", s.ResumeConversionRule)
		v1.DELETE("/rules/:rule_id", s.DeleteConversionRule)
		v1.POST("/execute", s.ExecuteManualConversion)
	}
}

// CreateConversionRule godoc
// @Summary Create a new conversion rule
// @Description Creates an automated conversion rule for a user
// @Tags Conversion
// @Accept json
// @Produce json
// @Param request body smartconversion.CreateConversionRuleRequest true "Conversion rule details"
// @Success 201 {object} smartconversion.ConversionRule
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 409 {object} basemodels.ErrorResponse "Rule already exists for currency pair"
// @Router /api/v1/smart-convert/rules [post]
// @Security BearerAuth
func (s *SmartConvertHandler) CreateConversionRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req smartconversion.CreateConversionRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Error("Invalid request", "error", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// Additional validation
	if req.SourceCurrency == req.TargetCurrency {
		c.JSON(http.StatusBadRequest, basemodels.NewError("source and target currencies must be different"))
		return
	}

	rule, err := s.conversionSvc.CreateConversionRule(c.Request.Context(), activeUser.UserID, &req)
	if err != nil {
		if err == smartconversion.ErrDuplicateRule {
			c.JSON(http.StatusConflict, basemodels.NewError("active rule already exists for this currency pair"))
			return
		}
		s.logger.Error("Failed to create conversion rule", "error", err)

		// audit log
		errMsg := err.Error()
		entry := audit.NewLog(
			c,
			audit.CategoryConversion,
			audit.EventCreateConversionRule,
			rule.ID.String(),
			"Failed to create conversion rule",
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		s.audit.Log(entry)

		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryConversion,
		audit.EventCreateConversionRule,
		rule.ID.String(),
		"Conversion rule created successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.Metadata = map[string]any{
		"CreatedAt":      rule.CreatedAt,
		"UpdatedAt":      rule.UpdatedAt,
		"FixedAmount":    rule.FixedAmount,
		"SourceCurrency": rule.SourceCurrency,
		"TargetCurrency": rule.TargetCurrency,
	}
	s.audit.Log(entry)

	c.JSON(http.StatusCreated, basemodels.NewSuccess("Conversion rule created successfully", rule))
}

// GetConversionRules godoc
// @Summary Get all conversion rules
// @Description Retrieves all conversion rules for the authenticated user
// @Tags Conversion
// @Produce json
// @Success 200 {object} []smartconversion.ConversionRuleResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/smart-convert/rules [get]
// @Security BearerAuth
func (s *SmartConvertHandler) GetConversionRules(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	rules, err := s.server.queries.GetActiveConversionRules(c.Request.Context(), activeUser.UserID)
	if err != nil {
		s.logger.Error("Failed to fetch conversion rules", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to fetch conversion rules"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", rules))
}

// PauseConversionRule godoc
// @Summary Pause a conversion rule
// @Description Temporarily pauses an active conversion rule
// @Tags Conversion
// @Produce json
// @Param rule_id path string true "Rule ID" format(uuid)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/smart-convert/rules/{rule_id}/pause [post]
// @Security BearerAuth
func (s *SmartConvertHandler) PauseConversionRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	ruleID, err := uuid.Parse(c.Param("rule_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid rule ID"))
		return
	}

	err = s.conversionSvc.PauseConversionRule(c, ruleID, activeUser.UserID)
	if err != nil {
		if err == smartconversion.ErrRuleNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError("conversion rule not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to pause conversion rule"))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryConversion,
		audit.EventPauseConversionRule,
		ruleID.String(),
		"Conversion rule paused successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.Metadata = map[string]any{
		"time": time.Now().Format(time.RFC3339),
	}
	s.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("conversion rule paused", nil))
}

// ResumeConversionRule godoc
// @Summary Resume a conversion rule
// @Description Resumes a paused conversion rule
// @Tags Conversion
// @Produce json
// @Param rule_id path string true "Rule ID" format(uuid)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/smart-convert/rules/{rule_id}/resume [post]
// @Security BearerAuth
func (s *SmartConvertHandler) ResumeConversionRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	ruleID, err := uuid.Parse(c.Param("rule_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid rule ID"))
		return
	}

	err = s.conversionSvc.ResumeConversionRule(c, ruleID, activeUser.UserID)
	if err != nil {
		if err == smartconversion.ErrRuleNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError("conversion rule not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to resume conversion rule"))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryConversion,
		audit.EventResumeConversionRule,
		ruleID.String(),
		"Conversion rule resumed successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.Metadata = map[string]any{
		"time": time.Now().Format(time.RFC3339),
	}
	s.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("conversion rule resumed", nil))
}

// DeleteConversionRule godoc
// @Summary Delete a conversion rule
// @Description Permanently deletes a conversion rule
// @Tags Conversion
// @Produce json
// @Param rule_id path string true "Rule ID" format(uuid)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/smart-convert/rules/{rule_id} [delete]
// @Security BearerAuth
func (s *SmartConvertHandler) DeleteConversionRule(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	ruleID, err := uuid.Parse(c.Param("rule_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid rule ID"))
		return
	}

	err = s.conversionSvc.DeleteConversionRule(c, ruleID, activeUser.UserID)
	if err != nil {
		if err == smartconversion.ErrRuleNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError("conversion rule not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to delete conversion rule"))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryConversion,
		audit.EventDeleteConversionRule,
		ruleID.String(),
		"Conversion rule deleted successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.Metadata = map[string]any{
		"time": time.Now().Format(time.RFC3339),
	}
	s.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("conversion rule deleted", nil))
}

// ExecuteManualConversion godoc
// @Summary Execute a manual conversion
// @Description Executes an immediate currency conversion
// @Tags Conversion
// @Accept json
// @Produce json
// @Param request body smartconversion.ManualConversionRequest true "Conversion details"
// @Success 200 {object} smartconversion.ManualConversionResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /api/v1/smart-convert/execute [post]
// @Security BearerAuth
func (s *SmartConvertHandler) ExecuteManualConversion(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// make admin check
	var req smartconversion.ManualConversionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	user, err := s.server.queries.GetUserByID(c.Request.Context(), activeUser.UserID)
	if err != nil {
		s.logger.Error("Failed to fetch user", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	result, err := s.conversionSvc.ExecuteManualConversion(c.Request.Context(), &req, &user)
	if err != nil {
		if err == smartconversion.ErrInsufficientBalance {
			c.JSON(http.StatusBadRequest, basemodels.NewError("insufficient balance for conversion"))
			return
		}
		if err == exchangerate.ErrRateNotAvailable {
			c.JSON(http.StatusServiceUnavailable, basemodels.NewError("exchange rate not available for the requested currency pair"))
			return
		}
		s.logger.Error("Failed to execute conversion", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryConversion,
		audit.EventManualConversion,
		result.ID.String(),
		"Manual conversion executed successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.Metadata = map[string]any{
		"time":           time.Now().Format(time.RFC3339),
		"executed_rate":  result.ExecutedRate,
		"net_amount":     result.NetAmount,
		"source_amount":  result.SourceAmount,
		"target_amount":  result.TargetAmount,
		"status":         result.Status,
	}
	s.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Conversion executed successfully", result))
}

// GetExchangeRate godoc
// @Summary Get current exchange rate
// @Description Retrieves real-time exchange rate between two currencies
// @Tags Conversion
// @Produce json
// @Param from query string true "Source currency" Enums(USD, NGN, USDT, USDC)
// @Param to query string true "Target currency" Enums(USD, NGN, USDT, USDC)
// @Success 200 {object} exchangerate.ExchangeRateResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /api/v1/smart-convert/rates [get]
// @Security BearerAuth
func (s *SmartConvertHandler) GetExchangeRate(c *gin.Context) {
	from := c.Query("from")
	to := c.Query("to")

	if from == "" || to == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("both 'from' and 'to' currency parameters are required"))
		return
	}

	// Validate currencies
	if err := s.exchangeRateSvc.ValidateCurrencyPair(from, to); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	rate, err := s.exchangeRateSvc.GetExchangeRate(c.Request.Context(), from, to)
	if err != nil {
		s.logger.Error("Failed to fetch exchange rate", "error", err)
		c.JSON(http.StatusServiceUnavailable, basemodels.NewError("failed to fetch exchange rate"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"from":         from,
		"to":           to,
		"exchangeRate": rate,
		"lastUpdated":  rate.Time,
	}))
}

// @Summary Get conversion history
// @Description Retrieves conversion history for the authenticated user
// @Tags Conversion
// @Produce json
// @Param limit query int false "Number of records" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} []smartconversion.ConversionHistoryResponse
// @Router /api/v1/smart-convert/history [get]
// @Security BearerAuth
func (s *SmartConvertHandler) GetConversionHistory(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	history, err := s.conversionSvc.GetConversionHistory(c.Request.Context(), activeUser.UserID, int32(limit), int32(offset))
	if err != nil {
		s.logger.Error("Failed to fetch conversion history", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch conversion history"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"history": history,
		"limit":   limit,
		"offset":  offset,
	}))
}

// GetConversionStats godoc
// @Summary Get conversion statistics
// @Description Retrieves conversion statistics for the authenticated user
// @Tags Conversion
// @Produce json
// @Param period query string false "Time period" default(30d) Enums(7d, 30d, 90d, 1y, all)
// @Success 200 {object} smartconversion.ConversionStats
// @Router /api/v1/smart-convert/stats [get]
// @Security BearerAuth
func (s *SmartConvertHandler) GetConversionStats(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}
	period := c.DefaultQuery("period", "30d")
	since := s.getPeriodStartTime(period)

	stats, err := s.conversionSvc.GetConversionStats(c.Request.Context(), activeUser.UserID, since)
	if err != nil {
		s.logger.Error("Failed to fetch conversion stats", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch conversion stats"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"data":   stats,
		"period": period,
	}))
}

func (s *SmartConvertHandler) getPeriodStartTime(period string) time.Time {
	now := time.Now()

	switch period {
	case "7d":
		return now.AddDate(0, 0, -7)
	case "30d":
		return now.AddDate(0, 0, -30)
	case "90d":
		return now.AddDate(0, 0, -90)
	case "1y":
		return now.AddDate(-1, 0, 0)
	case "all":
		return time.Time{} // Beginning of time
	default:
		return now.AddDate(0, 0, -30) // Default to 30 days
	}
}
