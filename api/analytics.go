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
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

type Analytics struct {
	server *Server
	audit  *audit.Service
	notif  *service.Notification
}

func (h Analytics) router(server *Server) {
	h.server = server
	h.audit = server.auditService
	h.notif = server.inAppnotificationService

	serverGroupV1 := server.router.Group("/api/v1/analytics")
	serverGroupV1.GET("/transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllTransactions)
	serverGroupV1.GET("/transaction/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTransaction) // not done
	serverGroupV1.GET("/gift-cards", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListGiftCards)
	serverGroupV1.GET("/total-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalTransactions)
	serverGroupV1.GET("/crypto-transactions/counts", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetCryptoTransactionCounts)
	serverGroupV1.GET("/crypto-transactions/amount", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalCryptoTransactionAmount)
	serverGroupV1.GET("/crypto-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllCryptoTransactions)
	serverGroupV1.GET("/giftcard-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllGiftCardTransactions)
	serverGroupV1.GET("/user-wallets/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetUserWallets)
	serverGroupV1.PUT("/edit-user/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.AdminEditUser)
	serverGroupV1.GET("/transaction-with-metadata/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTransactionWithMetadata)
	// serverGroupV1.POST("/send-notification-to-all", h.server.authMiddleware.AuthenticatedMiddleware(), h.SendNotificationToAll)
	serverGroupV1.GET("/get-total-reward-paid", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalRewardPaid)
	serverGroupV1.GET("/get-total-reward-earned", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalRewardEarned)
	serverGroupV1.GET("/get-total-outflow-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalOutflowTransactions)
	serverGroupV1.GET("/get-total-inflow-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalInflowTransactions)
	serverGroupV1.GET("/get-total-inplatform-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTotalInplatformTransactions)
	serverGroupV1.GET("/transactions-by-type", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListTransactionsByType)
	serverGroupV1.GET("/transactions-with-metadata/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetTransactionWithMetadata)
	serverGroupV1.PUT("/update-system-settings", h.server.authMiddleware.AuthenticatedMiddleware(), h.UpdateSystemSettings)
	serverGroupV1.GET("/get-system-settings", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetSystemSettings)
	serverGroupV1.GET("/schedulers/stats", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetSchedulerStats)
	serverGroupV1.POST("/schedulers/trigger", h.server.authMiddleware.AuthenticatedMiddleware(), h.TriggerSchedulerTask)
	serverGroupV1.GET("/daily-transactions-summary", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetDailyTransactions)
	serverGroupV1.POST("/create-notification", h.server.authMiddleware.AuthenticatedMiddleware(), h.createNotification)
	serverGroupV1.GET("/list-admin-alerts", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAdminAlerts)
	serverGroupV1.GET("/mark-alert-as-read/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.MarkAlertAsRead)
	serverGroupV1.GET("/list-unread-alerts", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListUnReadAlerts)
	serverGroupV1.GET("/list-card-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.ListAllVirtualCardTransactions)
	serverGroupV1.GET("/list-rapid-ramp-users", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetRapidRampUsers)
	serverGroupV1.GET("/list-rapid-ramp-transactions", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetRapidRampTransactions)
}

type UpdateSystemSettingsRequest struct {
	RewardsEnabled          *bool    `json:"rewards_enabled"`
	VaultsEnabled           *bool    `json:"vaults_enabled"`
	SmartConversionsEnabled *bool    `json:"smart_conversions_enabled"`
	RapidRampEnabled        *bool    `json:"rapid_ramp_enabled"`
	CardDeclineFee          *float32 `json:"card_decline_fee"`
	MaxCardFailedTxns       *int32   `json:"max_card_failed_txns"`
	TwoFACode               string   `json:"two_fa_code" binding:"required"`
}

func toNullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{Valid: false}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func toNullInt32(i *int32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: *i, Valid: true}
}

func toNullFloat64(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{Valid: false}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

type SystemSetting struct {
	ID                      int32     `json:"id"`
	RewardsEnabled          bool      `json:"rewards_enabled"`
	VaultsEnabled           bool      `json:"vaults_enabled"`
	SmartConversionsEnabled bool      `json:"smart_conversions_enabled"`
	RapidRampEnabled        bool      `json:"rapid_ramp_enabled"`
	MaxCardFailedTxns       int32     `json:"max_card_failed_txns"`
	CardDeclineFee          float64   `json:"card_decline_fee"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func (h *Analytics) GetSystemSettings(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
		return
	}

	settings, err := h.server.queries.GetSystemSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	response := SystemSetting{
		ID:                      settings.ID,
		RewardsEnabled:          settings.RewardsEnabled.Bool,
		VaultsEnabled:           settings.VaultsEnabled.Bool,
		SmartConversionsEnabled: settings.SmartConversionsEnabled.Bool,
		CardDeclineFee:          settings.CardDeclineFee.Float64,
		MaxCardFailedTxns:       settings.MaxCardFailedTxns.Int32,
		CreatedAt:               settings.CreatedAt.Time,
		UpdatedAt:               settings.UpdatedAt.Time,
	}

	c.JSON(http.StatusOK, response)
}

// UpdateSystemSettings godoc
// @Summary Update system settings
// @Tags Analytics
// @Accept json
// @Produce json
// @Param rewards_enabled query bool false "Enable rewards"
// @Param vaults_enabled query bool false "Enable vaults"
// @Param smart_conversions_enabled query bool false "Enable smart conversions"
// @Param rapid_ramp_enabled query bool false "Enable rapid ramp"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/v1/analytics/update-system-settings [put]
func (h *Analytics) UpdateSystemSettings(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusForbidden, err.Error())
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
		return
	}

	var req UpdateSystemSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	admin, err := h.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !admin.TwofaEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("2FA must be enabled to perform this action"))
		return
	}

	valid := totp.Validate(req.TwoFACode, admin.TwofaSecret.String)
	if !valid {
		c.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid 2FA code"))
		return
	}

	// Optional guard
	if req.RewardsEnabled == nil &&
		req.VaultsEnabled == nil &&
		req.SmartConversionsEnabled == nil &&
		req.CardDeclineFee == nil &&
		req.MaxCardFailedTxns == nil &&
		req.RapidRampEnabled == nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("no fields provided for update"))
		return
	}

	var cardDeclineFee *float64
	if req.CardDeclineFee != nil {
		f64 := float64(*req.CardDeclineFee)
		cardDeclineFee = &f64
	}

	params := db.UpdateSystemSettingsParams{
		RewardsEnabled:          toNullBool(req.RewardsEnabled),
		VaultsEnabled:           toNullBool(req.VaultsEnabled),
		SmartConversionsEnabled: toNullBool(req.SmartConversionsEnabled),
		RapidRampEnabled:        toNullBool(req.RapidRampEnabled),
		MaxCardFailedTxns:       toNullInt32(req.MaxCardFailedTxns),
		CardDeclineFee:          toNullFloat64(cardDeclineFee),
	}

	if err := h.server.queries.UpdateSystemSettings(
		c.Request.Context(),
		params,
	); err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategorySystem,
		audit.EventUpdateSystemSettings,
		"",
		fmt.Sprintf("System settings updated by admin %d", activeUser.UserID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	h.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess(
		"System settings updated successfully",
		nil,
	))
}

type GetTransactionByIDRow struct {
	ID                   uuid.UUID  `json:"id"`
	UserID               *int64     `json:"user_id"`
	Type                 string     `json:"type"`
	Description          *string    `json:"description"`
	TransactionFlow      *string    `json:"transaction_flow"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	DeletedFromAccountID *uuid.UUID `json:"deleted_from_account_id"`
	DeletedToAccountID   *uuid.UUID `json:"deleted_to_account_id"`
	SourceWallet         *uuid.UUID `json:"source_wallet"`
	DestinationWallet    *uuid.UUID `json:"destination_wallet"`
	Currency             string     `json:"currency"`
	Rate                 *string    `json:"rate"`
	Fees                 *string    `json:"fees"`
	ReceivedAmount       *string    `json:"received_amount"`
	SentAmount           *string    `json:"sent_amount"`
}

func mapGetTransactionByIDRow(row db.GetTransactionByIDRow) GetTransactionByIDRow {
	return GetTransactionByIDRow{
		ID:                   row.ID,
		UserID:               &row.UserID,
		Type:                 row.Type,
		Description:          &row.Description.String,
		TransactionFlow:      &row.TransactionFlow,
		Status:               row.Status,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		DeletedFromAccountID: &row.DeletedFromAccountID.UUID,
		DeletedToAccountID:   &row.DeletedToAccountID.UUID,
		SourceWallet:         &row.SourceWallet.UUID,
		DestinationWallet:    &row.DestinationWallet.UUID,
		Currency:             row.Currency,
		Rate:                 &row.Rate.String,
		Fees:                 &row.Fees.String,
	}
}

// GetTransaction godoc
// @Summary      Get Transaction
// @Description  Retrieve a specific transaction by ID. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param        id   path     string  true  "Transaction ID"
// @Success      200  {object}  GetTransactionByIDRow
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Router       /api/v1/analytics/transactions/{id} [get]
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
func (h *Analytics) GetTransaction(c *gin.Context) {
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

	transactionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error parsing transaction ID: %v", err))
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid transaction ID"))
		return
	}

	transaction, err := h.server.queries.GetTransactionByID(c, transactionID)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching transaction: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transaction"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Transaction retrieved successfully", mapGetTransactionByIDRow(transaction)))
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
// @Router       /api/v1/analytics/transactions [get]
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
func (h *Analytics) ListAllTransactions(c *gin.Context) {
	// activeUser, err := utils.GetActiveUser(c)
	// if err != nil {
	// 	h.server.logger.Error(err.Error())
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
	// 	return
	// }

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

// ListTransactionsByType godoc
// @Summary      List Transactions By Type
// @Description  Retrieve all transactions by type. Accessible only by admin.
// @Description  Transaction type can be 'swap', 'transfer', 'crypto', 'giftcard', 'vault', 'airtime', 'data', 'tv_subscription', 'utility_payment', 'electricity', 'qr_code', 'card', 'rewards', 'service'.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param        type   query     string  true  "Transaction Type"
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Router       /api/v1/analytics/transactions-by-type [get]
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
func (h *Analytics) ListTransactionsByType(c *gin.Context) {
	transactionType := c.Query("type")
	if transactionType == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("transaction type is required"))
		return
	}

	transactions, err := h.server.queries.ListTransactionsByType(c, transactionType)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transactions"))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Transactions retrieved successfully", transactions))
}

// GetTransactionWithMetadata godoc
// @Summary      Get Transaction With Metadata
// @Description  Retrieve a specific transaction by ID with metadata. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param        id   path     string  true  "Transaction ID"
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Router       /api/v1/analytics/transactions-with-metadata/{id} [get]
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
func (h *Analytics) GetTransactionWithMetadata(c *gin.Context) {
	transactionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error parsing transaction ID: %v", err))
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid transaction ID"))
		return
	}

	transaction, err := h.server.queries.GetTransactionWithMetadata(c, transactionID)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching transaction: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transaction"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Transaction retrieved successfully", transaction))
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
func (h *Analytics) ListGiftCards(c *gin.Context) {
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

// GetTotalSent godoc
// @Summary      Get Total Sent
// @Description  Retrieve total transaction made on the platform. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/total-transactions [get]
func (h *Analytics) GetTotalTransactions(c *gin.Context) {
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

	totalTransactions, err := h.server.queries.GetTotalTransactions(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch total transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total transactions retrieved successfully", gin.H{
		"total_transactions": totalTransactions,
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
func (h *Analytics) GetCryptoTransactionCounts(c *gin.Context) {
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
func (h *Analytics) GetTotalCryptoTransactionAmount(c *gin.Context) {
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
func (h *Analytics) ListAllCryptoTransactions(c *gin.Context) {
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
func (h *Analytics) ListAllGiftCardTransactions(c *gin.Context) {
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
func (h *Analytics) GetUserWallets(c *gin.Context) {
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
func (h *Analytics) AdminEditUser(c *gin.Context) {
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

type SendNotificationToAllUsersRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// SendNotificationToAll godoc
// @Summary      Send Notification To All
// @Description  Send a notification to all users.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param body      body      SendNotificationToAllUsersRequest  true  "Send Notification To All Users Request"
// @Success      200  {object}  models.NotificationResponse
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/send-notification-to-all [post]
// func (h *Analytics) SendNotificationToAll(c *gin.Context) {
// 	activeUser, err := utils.GetActiveUser(c)
// 	if err != nil || activeUser.Role == models.USER {
// 		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
// 		return
// 	}

// 	var req SendNotificationToAllUsersRequest

// 	if err := c.ShouldBindJSON(&req); err != nil {
// 		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
// 		return
// 	}

// 	notification := db.SendNotificationToAllUsersParams{
// 		Title:   req.Title,
// 		Message: req.Message,
// 	}

// 	err = h.server.queries.SendNotificationToAllUsers(c, notification)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to send notification"))
// 		return
// 	}

// 	c.JSON(http.StatusOK, basemodels.NewSuccess("Notification sent successfully", nil))
// }

// GetTotalRewardPaid godoc
// @Summary      Get Total Reward Paid
// @Description  Retrieve the total reward paid by the platform.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  int
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/get-total-reward-paid [get]
func (h *Analytics) GetTotalRewardPaid(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	totalRewardPaid, err := h.server.queries.GetTotalRewardPaid(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get total reward paid"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total reward paid retrieved successfully", totalRewardPaid))
}

// GetTotalRewardEarned godoc
// @Summary      Get Total Reward Earned
// @Description  Retrieve the total reward earned by all users.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  int
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/get-total-reward-earned [get]
func (h *Analytics) GetTotalRewardEarned(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	totalRewardEarned, err := h.server.queries.GetTotalRewardEarned(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total reward earned: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get total reward earned"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total reward earned retrieved successfully", totalRewardEarned))
}

// GetTotalOutflowTransactions godoc
// @Summary      Get Total Outflow Transactions
// @Description  Retrieve the total number of outflow transactions. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  int
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/get-total-outflow-transactions [get]
func (h *Analytics) GetTotalOutflowTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	totalOutflowTransactions, err := h.server.queries.GetTotalOutflowTransactions(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get total outflow transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total outflow transactions retrieved successfully", totalOutflowTransactions))
}

// GetTotalInflowTransactions godoc
// @Summary      Get Total Inflow Transactions
// @Description  Retrieve the total number of inflow transactions. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  int
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/get-total-inflow-transactions [get]
func (h *Analytics) GetTotalInflowTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	totalInflowTransactions, err := h.server.queries.GetTotalInflowTransactions(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get total inflow transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total inflow transactions retrieved successfully", totalInflowTransactions))
}

// GetTotalInplatformTransactions godoc
// @Summary      Get Total Inplatform Transactions
// @Description  Retrieve the total number of inplatform transactions. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  int
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/get-total-inplatform-transactions [get]
func (h *Analytics) GetTotalInplatformTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	totalInplatformTransactions, err := h.server.queries.GetTotalInplatformTransactions(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching total inplatform transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get total inplatform transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total inplatform transactions amount retrieved successfully", totalInplatformTransactions))
}

// GetSchedulerStats godoc
// @Summary      Get Scheduler Stats
// @Description  Retrieve statistics and status for all system schedulers. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/schedulers/stats [get]
func (h *Analytics) GetSchedulerStats(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	stats := make(map[string]interface{})

	if h.server.vaultScheduler != nil {
		if s, err := h.server.vaultScheduler.GetStats(c); err == nil {
			stats["vault_recurring"] = s
		}
	}

	if h.server.yieldScheduler != nil {
		if s, err := h.server.yieldScheduler.GetStats(c); err == nil {
			stats["vault_yield"] = s
		}
	}

	if h.server.qrcodeScheduler != nil {
		stats["rapid_ramp"] = h.server.qrcodeScheduler.GetStats(c)
	}

	if h.server.smartConversionScheduler != nil {
		stats["smart_conversion"] = h.server.smartConversionScheduler.GetStats(c)
	}

	if h.server.subscriptionScheduler != nil {
		stats["subscriptions"] = h.server.subscriptionScheduler.HealthCheck()
	}

	if h.server.streakScheduler != nil {
		stats["streaks"] = h.server.streakScheduler.GetStats(c)
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Scheduler statistics retrieved successfully", stats))
}

type TriggerSchedulerTaskRequest struct {
	TaskName string `json:"task_name" binding:"required"`
}

// TriggerSchedulerTask godoc
// @Summary      Trigger Scheduler Task
// @Description  Manually trigger a specific scheduler task. Accessible only by admin.
// @Description  Task names must be one of the following:
// @Description  process_vault_recurring, process_vault_yield, rapid_ramp_conversions, rapid_ramp_payouts, smart_conversion_scheduled, smart_conversion_rate, subscription_renewal_reminders, subscription_auto_topup, streak_reset, streak_reminders, streak_weekly_analytics
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Param        body  body      TriggerSchedulerTaskRequest  true  "Trigger Task Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      401   {object}  basemodels.ErrorResponse
// @Failure      403   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/schedulers/trigger [post]
func (h *Analytics) TriggerSchedulerTask(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	var req TriggerSchedulerTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	var triggerErr error
	ctx := c.Request.Context()

	switch req.TaskName {
	case "process_vault_recurring":
		if h.server.vaultScheduler != nil {
			triggerErr = h.server.vaultScheduler.ProcessAllDueNow(ctx)
		}
	case "process_vault_yield":
		if h.server.yieldScheduler != nil {
			triggerErr = h.server.yieldScheduler.ProcessAllYieldsNow(ctx)
		}
	case "rapid_ramp_conversions":
		if h.server.qrcodeScheduler != nil {
			triggerErr = h.server.qrcodeScheduler.TriggerConversions(ctx)
		}
	case "rapid_ramp_payouts":
		if h.server.qrcodeScheduler != nil {
			triggerErr = h.server.qrcodeScheduler.TriggerPayouts(ctx)
		}
	case "smart_conversion_scheduled":
		if h.server.smartConversionScheduler != nil {
			triggerErr = h.server.smartConversionScheduler.TriggerScheduledConversions(ctx)
		}
	case "smart_conversion_rate":
		if h.server.smartConversionScheduler != nil {
			triggerErr = h.server.smartConversionScheduler.TriggerRateBasedConversions(ctx)
		}
	case "subscription_renewal_reminders":
		if h.server.subscriptionScheduler != nil {
			triggerErr = h.server.subscriptionScheduler.RunRenewalRemindersNow()
		}
	case "subscription_auto_topup":
		if h.server.subscriptionScheduler != nil {
			triggerErr = h.server.subscriptionScheduler.RunAutoTopUpNow()
		}
	case "streak_reset":
		if h.server.streakScheduler != nil {
			triggerErr = h.server.streakScheduler.TriggerResetBrokenStreaks(ctx)
		}
	case "streak_reminders":
		if h.server.streakScheduler != nil {
			triggerErr = h.server.streakScheduler.TriggerStreakReminders(ctx)
		}
	case "streak_weekly_analytics":
		if h.server.streakScheduler != nil {
			triggerErr = h.server.streakScheduler.TriggerWeeklyAnalytics(ctx)
		}
	default:
		c.JSON(http.StatusBadRequest, basemodels.NewError("unknown task name"))
		return
	}

	if triggerErr != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(triggerErr.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("Task '%s' triggered successfully", req.TaskName), nil))
}

// GetDailyTransactions godoc
// @Summary      Get Daily Transactions
// @Description  Retrieve daily transaction summaries. Accessible only by admin.
// @Tags         Analytics
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      403  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Security     BearerAuth
// @Router       /api/v1/analytics/daily-transactions-summary [get]
func (h *Analytics) GetDailyTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	dailyTransactions, err := h.server.queries.GetDailyTransactionSummary(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Daily transactions retrieved successfully", dailyTransactions))
}

type CreateNotif struct {
	Title      string  `json:"title" binding:"required"`
	Message    string  `json:"message" binding:"required"`
	Recipients []int64 `json:"recipients" binding:"required"`
}

func (h *Analytics) createNotification(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	var req *CreateNotif
	if err = c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, basemodels.NewError(err.Error()))
		return
	}

	_, err = h.notif.CreateWithRecipients(
		c,
		&activeUser.UserID,
		req.Title,
		req.Message,
		"admin",
		req.Recipients,
	)
	if err != nil {
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(200, basemodels.NewSuccess("Notification sent", nil))
}

func (h *Analytics) ListAdminAlerts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.ADMIN {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	alerts, err := h.notif.ListAdminAlerts(c, 10, 0) // You can adjust the limit and offset as needed
	if err != nil {
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(200, basemodels.NewSuccess("Admin alerts retrieved successfully", alerts))
}

func (h *Analytics) MarkAlertAsRead(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	alertIDStr := c.Param("id")
	alertID, err := strconv.Atoi(alertIDStr)
	if err != nil {
		c.JSON(400, basemodels.NewError("invalid alert ID"))
		return
	}

	err = h.notif.AcknowledgeAdminAlert(c, int64(alertID))
	if err != nil {
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(200, basemodels.NewSuccess("Alert marked as read", nil))
}

func (h *Analytics) ListUnReadAlerts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	alerts, err := h.notif.ListUnacknowledgedAdminAlerts(c)
	if err != nil {
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(200, basemodels.NewSuccess("Read admin alerts retrieved successfully", alerts))
}

func (h *Analytics) ListAllVirtualCardTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.ADMIN {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	transactions, err := h.server.queries.ListCardTransactions(c, db.ListCardTransactionsParams{
		Limit:  50, // Set to nil to fetch all transactions
		Offset: 0,
	}) // You can implement pagination by passing limit and offset instead of nil
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching all virtual card transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch virtual card transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Virtual card transactions retrieved successfully", gin.H{
		"transactions": transactions,
	}))
}

func (h Analytics) GetRapidRampUsers(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.ADMIN {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	users, err := h.server.queries.ListRapidRampUsers(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching rapid ramp users: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch rapid ramp users"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Rapid Ramp users retrieved successfully", gin.H{
		"users": users,
		"count": len(users),
	}))
}

func (h Analytics) GetRapidRampTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil || activeUser.Role == models.ADMIN {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	transactions, err := h.server.queries.ListRapidRampTransactions(c)
	if err != nil {
		h.server.logger.Error(fmt.Sprintf("error fetching rapid ramp transactions: %v", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch rapid ramp transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Rapid Ramp transactions retrieved successfully", gin.H{
		"transactions": transactions,
		"count":        len(transactions),
	}))
}
