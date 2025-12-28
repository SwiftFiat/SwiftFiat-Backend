package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	bankaccounts "github.com/SwiftFiat/SwiftFiat-Backend/services/bank_accounts"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	rapidramp "github.com/SwiftFiat/SwiftFiat-Backend/services/rapid_ramp"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type QRCodeHandler struct {
	server             *Server
	qrCodeService      *rapidramp.QRCodeService
	bankAccountService *bankaccounts.BankAccountService
	logger             *logging.Logger
	audit              *audit.Service
}

func (q QRCodeHandler) router(server *Server) {
	q.server = server
	q.bankAccountService = q.server.bankAccountService
	q.logger = q.server.logger
	q.qrCodeService = q.server.qrcodeService
	q.audit = server.auditService

	v1 := server.router.Group("/api/v1/qr-codes")
	v1.GET("/public/:token", q.GetQRCodeByToken)
	v1.Use(q.server.authMiddleware.AuthenticatedMiddleware())
	{
		v1.POST("", q.CreateQRCode)
		v1.GET("", q.GetQRCodes)
		v1.DELETE("/:qr_id", q.DeleteQRCode)
		v1.GET("/transactions", q.GetQRTransactions)
		v1.GET("/stats", q.GetQRTransactionStats)
		v1.GET("/stats/admin", q.GetQRTransactionStatsAdmin)
		v1.GET("/admin", q.GetQRCodesAdmin)
		v1.PUT("/admin/update-status", q.AdminUpdateQRCodeStatus)
	}
}

// CreateQRCode godoc
// @Summary Generate a new QR code
// @Description Creates a new QR code for receiving crypto payments
// @Tags QR Codes
// @Accept json
// @Produce json
// @Param request body rapidramp.CreateQRCodeRequest true "QR code details"
// @Success 201 {object} rapidramp.QRCodeResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /api/v1/qr-codes [post]
// @Security BearerAuth
func (q *QRCodeHandler) CreateQRCode(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req rapidramp.CreateQRCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	_, err = q.server.queries.GetBankAccount(c, *req.BankAccountID)
	if err != nil {
		q.server.logger.Error("Failed to get bank account", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get bank account"))
		return
	}

	qrCode, err := q.qrCodeService.CreateQRCode(c.Request.Context(), activeUser.UserID, &req)
	if err != nil {
		q.server.logger.Error("Failed to create QR code", "error", err)

		errMsg := err.Error()
		e := audit.NewLog(
			c,
			audit.CategoryRapidRamp,
			audit.EventCreateQrCode,
			qrCode.ID.String(),
			"Failed to create QR code",
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		q.audit.Log(e)

		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// audit log
	e := audit.NewLog(
		c,
		audit.CategoryRapidRamp,
		audit.EventCreateQrCode,
		qrCode.ID.String(),
		"Qrcode created successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	q.audit.Log(e)

	c.JSON(http.StatusCreated, basemodels.NewSuccess("QR code generated successfully", qrCode))
}

// GetQRCodes godoc
// @Summary Get all QR codes
// @Description Retrieves all QR codes for the authenticated user
// @Tags QR Codes
// @Produce json
// @Success 200 {object} []rapidramp.QRCodeResponse
// @Router /api/v1/qr-codes [get]
// @Security BearerAuth
func (q *QRCodeHandler) GetQRCodes(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	qrCodes, err := q.qrCodeService.GetQRCodes(c.Request.Context(), activeUser.UserID)
	if err != nil {
		q.logger.Error("Failed to fetch QR codes", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch QR codes"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", qrCodes))
}

// GetQRCodeByToken godoc
// @Summary Get QR code by token
// @Description Retrieves a specific QR code by its token (public endpoint for payers)
// @Tags QR Codes
// @Produce json
// @Param token path string true "QR Code Token" format(uuid)
// @Success 200 {object} rapidramp.QRCodeResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/qr-codes/public/{token} [get]
func (q *QRCodeHandler) GetQRCodeByToken(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	token, err := uuid.Parse(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}

	qrcode, err := q.server.queries.GetQRCodeByToken(c, token)
	if err != nil {
		q.logger.Errorf("GetQRCodeByToken error: %s", err)
		c.JSON(500, basemodels.NewError("failed to get qrcode from token"))
		return
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("qrcode details", gin.H{
		"qr-code":     qrcode,
		"crypto_info": "Public payment information",
	}))
}

// DeleteQRCode godoc
// @Summary Delete a QR code
// @Description Soft deletes a QR code
// @Tags QR Codes
// @Produce json
// @Param qr_id path string true "QR Code ID" format(uuid)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/qr-codes/{qr_id} [delete]
// @Security BearerAuth
func (q *QRCodeHandler) DeleteQRCode(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	qrID, err := uuid.Parse(c.Param("qr_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid qr-code id"))
		return
	}

	err = q.qrCodeService.DeleteQRCode(c.Request.Context(), qrID, activeUser.UserID)
	if err != nil {
		if err == utils.ErrQRCodeNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError("QR code not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to delete QR code"))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryRapidRamp,
		audit.EventDeleteQrCode,
		qrID.String(),
		"QR code deleted successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	q.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("QR code deleted successfully", nil))
}

// GetQRCodesAdmin godoc
// @Summary Get all QR codes for admin
// @Description Retrieves all QR codes for the admin
// @Tags QR Codes
// @Produce json
// @Success 200 {object} []rapidramp.QRCodeResponse
// @Router /api/v1/qr-codes/admin [get]
// @Security BearerAuth
func (q *QRCodeHandler) GetQRCodesAdmin(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	qrCodes, err := q.server.queries.GetQRCodes(c)
	if err != nil {
		q.logger.Error("Failed to fetch QR codes", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch QR codes"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", qrCodes))
}

// AdminUpdateQRCodeStatus godoc
// @Summary Update QR code status for admin
// @Description Updates the status of a QR code for the admin
// @Tags QR Codes
// @Produce json
// @Param qr_id query string true "QR Code ID" format(uuid)
// @Param status query string true "New Status" enum(active, disabled)
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/qr-codes/admin/update-status [put]
// @Security BearerAuth
func (q *QRCodeHandler) AdminUpdateQRCodeStatus(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	qrId, err := uuid.Parse(c.Query("qr_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid qr id"))
		return
	}

	status := c.Query("status")

	_, err = q.server.queries.UpdateQRCodeStatus(c, db.UpdateQRCodeStatusParams{
		ID:     qrId,
		Status: status,
	})
	if err != nil {
		q.logger.Error("Failed to update QR code status", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update QR code status"))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategoryRapidRamp,
		audit.EventUpdateQrCodeStatus,
		qrId.String(),
		"QR code status updated successfully",
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	q.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("QR code status updated successfully", nil))
}

// ============================================================
// QR TRANSACTION ENDPOINTS
// ============================================================

// GetQRTransactions godoc
// @Summary Get QR transactions
// @Description Retrieves QR transaction history for the authenticated user
// @Tags QR Codes
// @Produce json
// @Param limit query int false "Number of records" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} []rapidramp.QRTransactionResponse
// @Router /api/v1/qr-codes/transactions [get]
// @Security BearerAuth
func (q *QRCodeHandler) GetQRTransactions(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	lt := c.DefaultQuery("limit", "20")
	os := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(lt)
	offset, _ := strconv.Atoi(os)

	txs, err := q.server.queries.GetQRTransactionsByUser(c, db.GetQRTransactionsByUserParams{
		UserID: activeUser.UserID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		q.logger.Errorf("failed to get qr-code transactions: %s", err)
		c.JSON(500, basemodels.NewError(apistrings.ServerError))
		return
	}

	var m []rapidramp.QRTransactionResponse
	for _, tx := range txs {
		m = append(m, rapidramp.QRTransactionResponse{
			ID:     tx.ID,
			Status: tx.Status,
			Crypto: rapidramp.CryptoDetails{
				Currency:        tx.CryptoCurrency,
				Amount:          tx.CryptoAmount,
				Network:         tx.CryptoNetwork,
				AmountUSD:       &tx.CryptoAmountUsd.String,
				TransactionHash: &tx.TransactionHash.String,
			},
			Conversion: &rapidramp.ConversionDetails{
				Rate:           tx.ConversionRate.String,
				FiatCurrency:   tx.FiatCurrency.String,
				FiatAmount:     tx.FiatAmount.String,
				ConversionFees: tx.ConversionFees,
				PlatformFees:   tx.PlatformFees,
				NetworkFees:    tx.NetworkFees,
				TotalFees:      tx.TotalFees,
				NetAmount:      tx.NetAmount.String,
			},
			Payout: &rapidramp.PayoutDetails{
				BankAccountNumber: tx.BankAccountNumber.String,
				BankAccountName:   tx.BankAccountName.String,
				// BankName: tx.,
				Reference: &tx.PayoutReference.String,
				Provider:  tx.PayoutProvider.String,
			},
			Timeline: rapidramp.TransactionTimeline{
				CreatedAt:             tx.CreatedAt,
				PaymentReceivedAt:     &tx.PaymentReceivedAt.Time,
				PaymentConfirmedAt:    &tx.PaymentConfirmedAt.Time,
				ConversionStartedAt:   &tx.ConversionStartedAt.Time,
				ConversionCompletedAt: &tx.ConversionCompletedAt.Time,
				PayoutInitiatedAt:     &tx.PaymentConfirmedAt.Time,
				PayoutCompletedAt:     &tx.PaymentConfirmedAt.Time,
			},
			CreatedAt: tx.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"transactions": m,
	}))
}

// GetQRTransactionStats godoc
// @Summary Get QR transaction statistics
// @Description Retrieves QR transaction statistics for the authenticated user
// @Tags QR Codes
// @Produce json
// @Param period query string false "Time period" default(30d)
// @Param limit query int false "Number of records" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} rapidramp.QRTransactionStats
// @Router /api/v1/qr-codes/stats [get]
// @Security BearerAuth
func (q *QRCodeHandler) GetQRTransactionStats(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	lt := c.DefaultQuery("limit", "20")
	os := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(lt)
	offset, _ := strconv.Atoi(os)

	// Parse period, e.g. "30d", "7d"
	period := c.DefaultQuery("period", "30d")
	days := 30
	if strings.HasSuffix(period, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(period, "d")); err == nil && n > 0 {
			days = n
		}
	}

	startDate := time.Now().AddDate(0, 0, -days)

	row, err := q.server.queries.GetQRTransactionStats(c, db.GetQRTransactionStatsParams{
		UserID:    activeUser.UserID,
		CreatedAt: startDate,
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		q.logger.Errorf("failed to get qr-code transaction stats: %s", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Helper to safely convert DB aggregate (interface{}) to decimal.Decimal
	toDecimal := func(v interface{}) decimal.Decimal {
		switch t := v.(type) {
		case nil:
			return decimal.Zero
		case string:
			d, err := decimal.NewFromString(t)
			if err == nil {
				return d
			}
		case []byte:
			d, err := decimal.NewFromString(string(t))
			if err == nil {
				return d
			}
		case float64:
			return decimal.NewFromFloat(t)
		case int64:
			return decimal.NewFromInt(t)
		}
		return decimal.Zero
	}

	stats := rapidramp.QRTransactionStats{
		TotalTransactions:         int(row.TotalTransactions),
		CompletedTransactions:     int(row.CompletedTransactions),
		FailedTransactions:        int(row.FailedTransactions),
		TotalCryptoReceived:       toDecimal(row.TotalCryptoReceived),
		TotalNetPayout:            toDecimal(row.TotalNetPayout),
		SendingToBankTransactions: int(row.SendingToBankTransactions),
		ConvertingTransactions:    int(row.ConvertingTransactions),
		ReceivedTransactions:      int(row.ReceivedTransactions),
		PendingTransactions:       int(row.PendingTransactions),
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("", stats))
}

// GetQRTransactionStatsAdmin godoc
// @Summary Get QR transaction statistics for admin
// @Description Retrieves QR transaction statistics for the admin
// @Tags QR Codes
// @Produce json
// @Param limit query int false "Limit for pagination" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} rapidramp.QRTransactionStats
// @Router /api/v1/qr-codes/stats/admin [get]
// @Security BearerAuth
func (q *QRCodeHandler) GetQRTransactionStatsAdmin(c *gin.Context) {
	settings, err := q.server.queries.GetSystemSettings(c)
	if err != nil {
		q.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RapidRampEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("rapid ramp is disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// Parse period, e.g. "30d", "7d"
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		q.logger.Errorf("failed to parse limit: %s", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		q.logger.Errorf("failed to parse offset: %s", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	row, err := q.server.queries.GetQRTransactionStatsAdmin(c, db.GetQRTransactionStatsAdminParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		q.logger.Errorf("failed to get qr-code transaction stats: %s", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Helper to safely convert DB aggregate (interface{}) to decimal.Decimal
	toDecimal := func(v interface{}) decimal.Decimal {
		switch t := v.(type) {
		case nil:
			return decimal.Zero
		case string:
			d, err := decimal.NewFromString(t)
			if err == nil {
				return d
			}
		case []byte:
			d, err := decimal.NewFromString(string(t))
			if err == nil {
				return d
			}
		case float64:
			return decimal.NewFromFloat(t)
		case int64:
			return decimal.NewFromInt(t)
		}
		return decimal.Zero
	}

	stats := rapidramp.QRTransactionStats{
		TotalTransactions:         int(row.TotalTransactions),
		CompletedTransactions:     int(row.CompletedTransactions),
		FailedTransactions:        int(row.FailedTransactions),
		TotalCryptoReceived:       toDecimal(row.TotalCryptoReceived),
		TotalNetPayout:            toDecimal(row.TotalNetPayout),
		SendingToBankTransactions: int(row.SendingToBankTransactions),
		ConvertingTransactions:    int(row.ConvertingTransactions),
		ReceivedTransactions:      int(row.ReceivedTransactions),
		PendingTransactions:       int(row.PendingTransactions),
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"stats":  stats,
		"limit":  limit,
		"offset": offset,
	}))
}
