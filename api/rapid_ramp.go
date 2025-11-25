package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
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
}

func (q QRCodeHandler) router(server *Server) {
	q.server = server
	q.bankAccountService = q.server.bankAccountService
	q.logger = q.server.logger
	q.qrCodeService = q.server.qrcodeService

	v1 := server.router.Group("/api/v1/qr-codes")
	v1.Use(q.server.authMiddleware.AuthenticatedMiddleware())
	{
		v1.POST("", q.CreateQRCode)
		v1.GET("", q.GetQRCodes)
		v1.GET("public/:token", q.GetQRCodeByToken)
		v1.DELETE("/:qr_id", q.DeleteQRCode)
		v1.GET("/transactions", q.GetQRTransactions)
		v1.GET("/stats", q.GetQRTransactionStats)
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
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req rapidramp.CreateQRCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidRequestData))
		return
	}

	qrCode, err := q.qrCodeService.CreateQRCode(c.Request.Context(), activeUser.UserID, &req)
	if err != nil {
		q.server.logger.Error("Failed to create QR code", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

	c.JSON(http.StatusOK, basemodels.NewSuccess("QR code deleted successfully", nil))
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

	c.JSON(http.StatusOK, basemodels.NewSuccess("", gin.H{
		"transactions": txs,
	}))
}

// GetQRTransactionStats godoc
// @Summary Get QR transaction statistics
// @Description Retrieves QR transaction statistics for the authenticated user
// @Tags QR Codes
// @Produce json
// @Param period query string false "Time period" default(30d)
// @Success 200 {object} rapidramp.QRTransactionStats
// @Router /api/v1/qr-codes/stats [get]
// @Security BearerAuth

func (q *QRCodeHandler) GetQRTransactionStats(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		q.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

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
		TotalTransactions:     int(row.TotalTransactions),
		CompletedTransactions: int(row.CompletedTransactions),
		FailedTransactions:    int(row.FailedTransactions),
		TotalCryptoReceived:   toDecimal(row.TotalCryptoReceived),
		TotalNetPayout:        toDecimal(row.TotalNetPayout),
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("", stats))
}
