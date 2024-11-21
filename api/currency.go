package api

import (
	"net/http"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/gin-gonic/gin"
)

type Currency struct {
	server          *Server
	currencyService *currency.CurrencyService
}

// / TODO: This route will be wrapped with an administrative middleware
func (c Currency) router(server *Server) {
	c.server = server
	c.currencyService = currency.NewCurrencyService(c.server.queries, c.server.logger)

	serverGroupV1 := server.router.Group("/api/v1/currency")
	serverGroupV1.POST("set", AuthenticatedMiddleware(), c.setPairRate)
	serverGroupV1.GET("get", AuthenticatedMiddleware(), c.getPairRate)
}

func (c *Currency) setPairRate(ctx *gin.Context) {
	request := struct {
		BaseCurrency  string `json:"base" binding:"required"`
		QuoteCurrency string `json:"quote" binding:"required"`
		Rate          string `json:"rate" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
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
	request := struct {
		BaseCurrency  string `json:"base" binding:"required"`
		QuoteCurrency string `json:"quote" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please check currency pair and rate"))
		return
	}

	exchObj, err := c.currencyService.GetExchangeRate(ctx, request.BaseCurrency, request.QuoteCurrency)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please check currency pair and rate"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pair set successfully", exchObj))
}
