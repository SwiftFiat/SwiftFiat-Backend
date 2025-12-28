package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/coindesk"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type MarketInsights struct {
	server                *Server
	marketInsightsService *coindesk.MarketInsightsService
}

func (m MarketInsights) router(server *Server) {
	m.server = server
	m.marketInsightsService = server.marketInsightsService

	serverGroupV1 := server.router.Group("/api/v1/market-insights")
	serverGroupV1.Use(m.server.authMiddleware.AuthenticatedMiddleware())

	// Simple news endpoints
	serverGroupV1.GET("/news", m.GetNews)
	serverGroupV1.GET("/news/:id", m.GetNewsArticle)
}

// GetNews godoc
// @Summary Get Crypto News
// @Description Retrieve latest cryptocurrency news articles
// @Tags market-insights
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Number of articles to retrieve (default: 10, max: 50)"
// @Success 200 {object} basemodels.SuccessResponse{data=object{news=[]coindesk.NewsArticle,count=int}}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/market-insights/news [get]
func (m *MarketInsights) GetNews(ctx *gin.Context) {
	_, err := utils.GetActiveUser(ctx)
	if err != nil {
		m.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	if limit > 50 {
		limit = 50
	}
	if limit < 1 {
		limit = 10
	}

	news, err := m.marketInsightsService.GetNews(ctx.Request.Context(), limit)
	if err != nil {
		m.server.logger.Error(fmt.Sprintf("Failed to fetch news: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch crypto news"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("news retrieved successfully", gin.H{
		"news":  news,
		"count": len(news),
	}))
}

// GetNewsArticle godoc
// @Summary Get Single News Article
// @Description Retrieve details of a specific news article by ID
// @Tags market-insights
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Article ID"
// @Success 200 {object} basemodels.SuccessResponse{data=coindesk.NewsArticle}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/market-insights/news/{id} [get]
func (m *MarketInsights) GetNewsArticle(ctx *gin.Context) {
	_, err := utils.GetActiveUser(ctx)
	if err != nil {
		m.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	idStr := ctx.Param("id")
	articleID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid article ID"))
		return
	}

	// Fetch all news and find the specific article
	news, err := m.marketInsightsService.GetNews(ctx.Request.Context(), 50)
	if err != nil {
		m.server.logger.Error(fmt.Sprintf("Failed to fetch news: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch news article"))
		return
	}

	for _, article := range news {
		if article.ID == articleID {
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("article retrieved successfully", article))
			return
		}
	}

	ctx.JSON(http.StatusNotFound, basemodels.NewError("article not found"))
}
