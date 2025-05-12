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
	serverGroupV1.GET("/total-received", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalReceived)
	serverGroupV1.GET("/total-sent", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalSent)
	serverGroupV1.GET("/total-trade", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalTrade)
	serverGroupV1.GET("/crypto-transactions/counts", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetCryptoTransactionCounts)
	serverGroupV1.GET("/crypto-transactions/amount", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalCryptoTransactionAmount)
	serverGroupV1.GET("/crypto-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllCryptoTransactions)
	serverGroupV1.DELETE("/activity-logs", h.server.authMiddleware.AuthenticatedMiddleware(), h.DeleteOldActivityLogs)
	serverGroupV1.GET("/giftcard-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllGiftCardTransactions)
	// serverGroupV1.GET("/disputes", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetDisputes)
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

	volume, err := h.server.queries.GetTotalTransactionVolume(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching transaction volume: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transaction volume"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Transactions retrieved successfully", gin.H{
		"transactions": transactions,
		"count":        len(transactions),
		"volume":       volume,
	}))
}

func (h *ActivityLog) ListGiftCards(c *gin.Context) {
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

func (h *ActivityLog) GetTotalReceived(c *gin.Context) {
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

	totalReceived, err := h.server.queries.GetTotalReceived(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total received: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch total received"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total received retrieved successfully", gin.H{
		"total_received": totalReceived,
	}))
}

func (h *ActivityLog) GetTotalSent(c *gin.Context) {
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

	totalSent, err := h.server.queries.GetTotalSent(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total sent: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch total sent"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total sent retrieved successfully", gin.H{
		"total_sent": totalSent,
	}))
}

func (h *ActivityLog) GetTotalTrade(c *gin.Context) {
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

	totalTrade, err := h.server.queries.GetTotalTrade(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total trade: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch total trade"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total trade retrieved successfully", gin.H{
		"total_trade": totalTrade,
	}))
}

func (h *ActivityLog) GetCryptoTransactionCounts(c *gin.Context) {
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

	counts, err := h.server.queries.GetCryptoTransactionCounts(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching crypto transaction counts: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch crypto transaction counts",
		})
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Crypto transaction counts retrieved successfully", gin.H{
		"successful_transactions": counts.SuccessfulTransactions,
		"failed_transactions":     counts.FailedTransactions,
		"pending_transactions":    counts.PendingTransactions,
	}))
}

func (h *ActivityLog) GetTotalCryptoTransactionAmount(c *gin.Context) {
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

	totalAmount, err := h.server.queries.GetTotalCryptoTransactionAmount(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total crypto transaction amount: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch total crypto transaction amount",
		})
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total crypto transaction amount retrieved successfully", gin.H{
		"total_sent_amount":     totalAmount.TotalSentAmount,
		"total_received_amount": totalAmount.TotalReceivedAmount,
	}))
}

func (h *ActivityLog) ListAllCryptoTransactions(c *gin.Context) {
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

	transactions, err := h.server.queries.ListAllCryptoTransactions(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching all crypto transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch crypto transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Crypto transactions retrieved successfully", gin.H{
		"transactions": transactions,
	}))
}

func (h *ActivityLog) DeleteOldActivityLogs(c *gin.Context) {
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

	err = h.service.DeleteOldLogs(c.Request.Context())
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error deleting old activity logs: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to delete old activity logs"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Old activity logs deleted successfully", nil))
}

func (h *ActivityLog) ListAllGiftCardTransactions (c *gin.Context) {
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

	transactions, err := h.server.queries.ListGiftcardTransactions(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching all gift card transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch gift card transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Gift card transactions retrieved successfully", gin.H{
		"transactions": transactions,
	}))
}