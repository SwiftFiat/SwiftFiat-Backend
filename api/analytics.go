package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type ActivityLog struct {
	server *Server
}

func (h ActivityLog) router(server *Server) {
	h.server = server

	serverGroupV1 := server.router.Group("/api/v1/analytics")
	serverGroupV1.GET("/transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllTransactions)
	serverGroupV1.GET("/gift-cards", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListGiftCards)
	serverGroupV1.GET("/total-received", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalReceived)
	serverGroupV1.GET("/total-sent", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalSent)
	serverGroupV1.GET("/total-trade", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalTrade)
	serverGroupV1.GET("/crypto-transactions/counts", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetCryptoTransactionCounts)
	serverGroupV1.GET("/crypto-transactions/amount", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalCryptoTransactionAmount)
	serverGroupV1.GET("/crypto-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllCryptoTransactions)
	serverGroupV1.GET("/giftcard-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllGiftCardTransactions)
	serverGroupV1.GET("/user-wallets/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetUserWallets)
	serverGroupV1.PUT("/edit-user/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.AdminEditUser)
}

// ListAllTransactions godoc
// @Summary      List All Transactions
// @Description  Retrieve all transactions with user details. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/transactions [get]
func (h *ActivityLog) ListAllTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// ListGiftCards godoc
// @Summary      List Gift Cards
// @Description  Retrieve all gift cards. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/gift-cards [get]
func (h *ActivityLog) ListGiftCards(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// ListGiftCards godoc
// @Summary      Get Total Received
// @Description  Retrieve total received amount. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/total-received [get]
func (h *ActivityLog) GetTotalReceived(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// GetTotalSent godoc
// @Summary      Get Total Sent
// @Description  Retrieve total sent amount. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/total-sent [get]
func (h *ActivityLog) GetTotalSent(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// GetTotalSent godoc
// @Summary      Get Total Sent
// @Description  Retrieve total sent amount. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/total-trade [get]
func (h *ActivityLog) GetTotalTrade(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// GetCryptoTransactionCounts godoc
// @Summary      Get Crypto Transaction Counts
// @Description  Retrieve counts of crypto transactions by status. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/crypto-transactions/counts [get]
func (h *ActivityLog) GetCryptoTransactionCounts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// GetTotalCryptoTransactionAmount godoc
// @Summary      Get Total Crypto Transaction Amount
// @Description  Retrieve total sent and received crypto transaction amounts. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/crypto-transactions/amount [get]
func (h *ActivityLog) GetTotalCryptoTransactionAmount(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
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

// ListAllCryptoTransactions godoc
// @Summary      List All Crypto Transactions
// @Description  Retrieve all crypto transactions. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/crypto-transactions [get]
func (h *ActivityLog) ListAllCryptoTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// ListAllGiftCardTransactions godoc
// @Summary      List All Gift Card Transactions
// @Description  Retrieve all gift card transactions. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/giftcard-transactions [get]
func (h *ActivityLog) ListAllGiftCardTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
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

// GetUserWallets godoc
// @Summary      Get User Wallets
// @Description  Retrieve wallets for a specific user by their ID. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param        id     path      int  true  "User ID"
// @Success      200  {object}  []models.WalletResponse
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/user-wallets/{id} [get]
func (h *ActivityLog) GetUserWallets(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
		return
	}

	ID := c.Param("id")
	userID, err := strconv.Atoi(ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	wallets, err := h.server.queries.GetWalletByCustomerID(c, int64(userID))
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching wallets: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get wallets"})
		return
	}

	var filteredWallets []models.WalletResponse
	for _, wallet := range wallets {
		filteredWallets = append(filteredWallets, *models.ToWalletResponse(&wallet))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Wallets retrieved successfully", filteredWallets))
}

type AdminEditUserRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone_number"`
	Role      string `json:"role"`
}

// AdminEditUser godoc
// @Summary      Admin Edit User
// @Description  Admin can edit user details such as first name, last name, email, phone number, and role.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param body      body      AdminEditUserRequest  true  "Admin Edit User Request"
// @Success      200  {object}  models.UserResponse
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/edit-user/{id} [put]
func (h *ActivityLog) AdminEditUser(c *gin.Context) {
	id := c.Param("id")
	userID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}
	var req AdminEditUserRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	param := db.AdminUpdateUserParams{
		ID:          int64(userID),
		FirstName:   sql.NullString{String: req.FirstName, Valid: req.FirstName != ""},
		LastName:    sql.NullString{String: req.LastName, Valid: req.LastName != ""},
		Email:       req.Email,
		PhoneNumber: req.Phone,
		Role:        req.Role,
	}

	user, err := h.server.queries.AdminUpdateUser(c, param)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update user"))
		return
	}

	mappedUser := models.UserResponse{}.ToUserResponse(&user)

	c.JSON(http.StatusOK, basemodels.NewSuccess("User updated successfully", mappedUser))
}
