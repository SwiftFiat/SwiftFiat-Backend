package api

import (
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type Currency struct {
	server          *Server
	currencyService *currency.CurrencyService
}

// TODO: This route will be wrapped with an administrative middleware
func (c Currency) router(server *Server) {
	c.server = server
	c.currencyService = currency.NewCurrencyService(c.server.queries, c.server.logger)

	serverGroupV1 := server.router.Group("/api/v1/currency")
	serverGroupV1.GET("get", c.server.authMiddleware.AuthenticatedMiddleware(), c.getPairRate)
	serverGroupV1.GET("all", c.server.authMiddleware.AuthenticatedMiddleware(), c.getAllRates)

	serverGroupV1Admin := server.router.Group("/api/v1/currency")
	serverGroupV1Admin.POST("set", c.server.authMiddleware.AuthenticatedMiddleware(), c.setPairRate)
	serverGroupV1Admin.PUT("rate-source/toggle", c.server.authMiddleware.AuthenticatedMiddleware(), c.toggleRateSource)
}

func (c *Currency) setPairRate(ctx *gin.Context) {
	user, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}
	if user.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	request := struct {
		BaseCurrency  string `json:"base" binding:"required"`
		QuoteCurrency string `json:"quote" binding:"required"`
		Rate          string `json:"rate" binding:"required"`
	}{}

	err = ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please check currency pair and rate"))
		return
	}

	exchObj, err := c.currencyService.SetExchangeRate(ctx, nil, request.BaseCurrency, request.QuoteCurrency, request.Rate)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please check currency pair and rate"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pair set successfully", exchObj))
}

func (c *Currency) getPairRate(ctx *gin.Context) {
	user, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}
	if user.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	baseCurrency := ctx.Query("base")
	quoteCurrency := ctx.Query("quote")

	if baseCurrency == "" || quoteCurrency == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("base and quote currency are required"))
		return
	}

	exchObj, err := c.currencyService.GetExchangeRate(ctx, baseCurrency, quoteCurrency)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please check currency pair and rate"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pair fetched successfully", exchObj))
}

func (c *Currency) getAllRates(ctx *gin.Context) {
	user, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}
	if user.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	rates, err := c.currencyService.GetAllExchangeRates(ctx)
	if err != nil {
		c.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("hmmmmm"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("all rates fetched successfully", rates))
}

// toggleRateSource allows admin to toggle between manual rates and exchange service rates
// godoc
// @Summary Toggle Rate Source
// @Description Switch between manual rates set by admin or rates from the exchange rate service
// @Tags currency
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body object{currency_pair=string,rate_source=string} true "Toggle Request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/currency/rate-source/toggle [put]
func (c *Currency) toggleRateSource(ctx *gin.Context) {
	user, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}
	if user.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("only admins can toggle rate sources"))
		return
	}

	request := struct {
		CurrencyPair string `json:"currency_pair" binding:"required" example:"USD/NGN"`
		RateSource   string `json:"rate_source" binding:"required" example:"manual"`
	}{}

	err = ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("currency_pair and rate_source are required"))
		return
	}

	// Validate rate source
	if request.RateSource != "manual" && request.RateSource != "exchange_service" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("rate_source must be 'manual' or 'exchange_service'"))
		return
	}

	// Convert to proper type
	var preference any = request.RateSource
	if request.RateSource == "manual" {
		preference = "manual"
	} else {
		preference = "exchange_service"
	}

	// Set the preference
	err = c.server.rateManager.SetRateSourcePreference(ctx, request.CurrencyPair, interface{}(preference).(string))
	if err != nil {
		c.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to toggle rate source"))
		return
	}

	response := map[string]any{
		"currency_pair": request.CurrencyPair,
		"rate_source":   request.RateSource,
		"message":       fmt.Sprintf("Rate source for %s switched to %s", request.CurrencyPair, request.RateSource),
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("rate source toggled successfully", response))
}
