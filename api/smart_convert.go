package api

import (
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	smartconversion "github.com/SwiftFiat/SwiftFiat-Backend/services/smart_conversion"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type SmartConvertHandler struct {
	server          *Server
	logger          *logging.Logger
	exchangeRateSvc *smartconversion.ExchangeRateService
	conversionSvc   *smartconversion.ConversionService
}

func (s SmartConvertHandler) router(server *Server) {
	s.server = server
	s.logger = s.server.logger
	s.exchangeRateSvc = s.server.scExchangeRateservice
	s.conversionSvc = s.server.smartConvertService

	v1 := server.router.Group("/api/v1/smart-convert")
	v1.Use(s.server.authMiddleware.AuthenticatedMiddleware())
	{
		v1.POST("/rules", s.CreateConversionRule)
		v1.GET("/rules", s.GetConversionRules)
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
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

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
