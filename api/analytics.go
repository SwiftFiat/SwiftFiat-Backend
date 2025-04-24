package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	activitylogs "github.com/SwiftFiat/SwiftFiat-Backend/services/activity_logs"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type ActivityLog struct {
	server  *Server
	service activitylogs.ActivityLog
}

func (h ActivityLog) router(server *Server) {
	h.server = server
	h.service = *activitylogs.NewActivityLog(*h.server.queries)

	serverGroupV1 := server.router.Group("/api/v1/analytics")
	serverGroupV1.GET("/activity-log/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetUserActivity)
	serverGroupV1.GET("/activity-logs", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetRecentActivity)
	serverGroupV1.GET("/active-users-today", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetActiveUsersCount)
	serverGroupV1.GET("/transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllTransactions)
	serverGroupV1.GET("/gift-cards", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListGiftCards)
}

func (h *ActivityLog) GetUserActivity(c *gin.Context) {
	user, _ := utils.GetActiveUser(c)
	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	logs, err := h.service.GetByUser(c.Request.Context(), int32(userID), int32(limit), int32(offset))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get activity logs"})
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Activity logs retrieved successfully", logs))

}

func (h *ActivityLog) GetRecentActivity(c *gin.Context) {
	user, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	logs, err := h.service.GetRecent(c.Request.Context(), int32(limit), int32(offset))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get recent activity logs"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Activity logs retrieved successfully", logs))
}

func (h *ActivityLog) GetActiveUsersCount(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}
	// Parse date from query params (default to today)
	dateStr := c.DefaultQuery("date", time.Now().Format("2006-01-02"))
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYY-MM-DD"})
		return
	}

	// Calculate time range (whole day)
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)

	count, err := h.service.CountActiveUsers(c, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count active users"})
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Active Users count retrieved successfully", gin.H{
		"count": count,
		"date":  dateStr,
	}))

}

func (h *ActivityLog) ListAllTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	transactions, err := h.server.queries.ListAllTransactionsWithUsers(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching all transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transactions"))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Transactions retrieved successfully", gin.H{
		"transactions": transactions,
		"count":        len(transactions),
	}))
}


func (h *ActivityLog) ListGiftCards(c *gin.Context)  {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}
	
	giftCards, err := h.server.queries.ListGiftCards(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching all gift cards: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch gift cards"))
		return
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("Gift cards retrieved successfully", gin.H{
		"gift_cards": giftCards,
		"count":      len(giftCards),
	}))
}