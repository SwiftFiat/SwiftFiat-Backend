package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bills"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/streaks"
	tx "github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Bills struct {
	server             *Server
	transactionService *tx.TransactionService
	notifr             *service.Notification
	walletService      *wallet.WalletService
	currencyService    *currency.CurrencyService
	audit              *audit.Service
	streakScheduler    *streaks.StreakScheduler
}

func (b Bills) router(server *Server) {
	b.server = server
	b.notifr = server.inAppnotificationService
	b.walletService = server.walletService
	b.currencyService = server.currencyService
	b.transactionService = server.transactionService
	b.audit = server.auditService
	b.streakScheduler = server.streakScheduler

	serverGroupV1 := server.router.Group("/api/v1/bills")
	serverGroupV1.GET("categories", b.server.authMiddleware.AuthenticatedMiddleware(), b.getCategories)
	serverGroupV1.GET("services", b.server.authMiddleware.AuthenticatedMiddleware(), b.getServices)
	serverGroupV1.GET("service-variation", b.server.authMiddleware.AuthenticatedMiddleware(), b.getServiceVariations)
	serverGroupV1.POST("buy-airtime", b.server.authMiddleware.AuthenticatedMiddleware(), b.buyAirtime)
	serverGroupV1.POST("buy-data", b.server.authMiddleware.AuthenticatedMiddleware(), b.buyData)
	serverGroupV1.POST("customer-info", b.server.authMiddleware.AuthenticatedMiddleware(), b.getCustomerInfo)
	serverGroupV1.POST("buy-tv", b.server.authMiddleware.AuthenticatedMiddleware(), b.buyTVSubscription)
	serverGroupV1.POST("customer-meter-info", b.server.authMiddleware.AuthenticatedMiddleware(), b.getCustomerMeterInfo)
	serverGroupV1.POST("buy-electricity", b.server.authMiddleware.AuthenticatedMiddleware(), b.buyElectricity)
}

// updateStreakAsync updates user streak asynchronously
func (b *Bills) updateStreakAsync(userID int64, transactionID uuid.UUID, txType tx.TransactionType) {
	bgCtx := context.Background()
	if err := b.transactionService.UpdateStreakAfterBillPayment(
		bgCtx,
		userID,
		transactionID,
		txType,
	); err != nil {
		b.server.logger.Error(fmt.Sprintf("Failed to update streak for user %d: %v", userID, err))
	}
}

// getCategories godoc
// @Summary Get categories
// @Description Get categories
// @Tags bills
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/categories [get]
// @Security BearerAuth
func (b *Bills) getCategories(ctx *gin.Context) {
	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	categories, err := billProv.GetServiceCategories()
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched bill categories", categories))
}

// getServices godoc
// @Summary Get services
// @Description Get services
// @Tags bills
// @Accept json
// @Produce json
// @Param identifier query string true "Identifier"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/services [get]
// @Security BearerAuth
func (b *Bills) getServices(ctx *gin.Context) {
	identifier := ctx.Query("identifier")

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	services, err := billProv.GetServiceIdentifiers(identifier)
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched bill services", models.ToServiceIdentifierResponseList(services)))
}

// getServiceVariations fetches service variations from Redis cache or remote API
// It first checks if the variations are already cached in Redis
// If not, it fetches them from the remote API and stores them in Redis
// It then returns the variations to the client
// The cache is set to expire in 10 minutes
// If the cache is empty, it will be set to expire in 10 seconds

// getServiceVariations godoc
// @Summary Get service variations
// @Description Get service variations
// @Tags bills
// @Accept json
// @Produce json
// @Param service_id query string true "Service ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/service-variations [get]
// @Security BearerAuth
func (b *Bills) getServiceVariations(ctx *gin.Context) {
	serviceID := ctx.Query("serviceID")

	variations, err := b.server.redis.GetVariations(ctx, fmt.Sprintf("variations:%s", serviceID))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if len(variations) > 0 {
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched service variations", variations))
		return
	}

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	remoteVariations, err := billProv.GetServiceVariation(serviceID)
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	// Store variations in Redis cache
	if remoteVariations != nil {
		variations := make([]models.BillVariation, len(remoteVariations))
		for i, variation := range remoteVariations {
			variations[i] = models.BillVariation{
				VariationCode:   variation.VariationCode,
				Name:            variation.Name,
				VariationAmount: variation.VariationAmount,
				FixedPrice:      variation.FixedPrice,
			}
		}

		err = b.server.redis.DeleteVariations(ctx, fmt.Sprintf("variations:%s", serviceID))
		if err != nil {
			b.server.logger.Error(fmt.Sprintf("failed to delete variations in cache: %v", err))
			// Don't return error to user since this is just caching
		}

		err = b.server.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", serviceID), variations)
		if err != nil {
			b.server.logger.Error(fmt.Sprintf("failed to store variations in cache: %v", err))
			// Don't return error to user since this is just caching
		}
	}

	variations, err = b.server.redis.GetVariations(ctx, fmt.Sprintf("variations:%s", serviceID))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	fromCache := len(remoteVariations) == 0 || remoteVariations == nil

	b.server.logger.Info(fmt.Sprintf("fetched service variations: %v %v", variations, fromCache))
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched service variations", variations))
}

// buyAirtime godoc
// @Summary Buy airtime
// @Description Buy airtime
// @Tags bills
// @Accept json
// @Produce json
// @Param wallet_id query string true "Wallet ID"
// @Param service_id query string true "Service ID"
// @Param phone query string true "Phone"
// @Param amount query int true "Amount"
// @Param pin query string true "Pin"
// @Param use_reward_points query bool false "Use reward points"
// @Param points_to_use query int false "Points to use"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/buy-airtime [post]
// @Security BearerAuth
func (b *Bills) buyAirtime(ctx *gin.Context) {
	request := struct {
		WalletID        string  `json:"wallet_id" binding:"required"`
		ServiceID       string  `json:"service_id" binding:"required"`
		Phone           string  `json:"phone" binding:"required"`
		Amount          int64   `json:"amount" binding:"required"`
		Pin             string  `json:"pin" binding:"required"`
		UseRewardPoints bool    `json:"use_reward_points"`
		PointsToUse     float32 `json:"points_to_use"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	// Start transaction
	dbTx, err := b.server.queries.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	defer dbTx.Rollback()

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Pull user information
	userInfo, err := b.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, userInfo.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	b.server.logger.Infof("buy airtime: %v", request)
	// ========================================================================
	// REWARD POINTS PROCESSING
	// ========================================================================
	originalAmount := decimal.NewFromInt(request.Amount)
	finalAmount := originalAmount
	var pointsUsed decimal.Decimal
	var pointsEarned decimal.Decimal
	redemptionApplied := false

	pointsToUseDecimal := decimal.NewFromFloat32(request.PointsToUse)

	// STEP 1: Validate and apply reward redemption if requested
	if request.UseRewardPoints && request.PointsToUse > 0 {
		var err error
		finalAmount, redemptionApplied, err = b.transactionService.ProcessRewardRedemption(
			ctx, dbTx, &userInfo, pointsToUseDecimal, originalAmount,
		)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if redemptionApplied {
			pointsUsed = pointsToUseDecimal
			b.server.logger.Info(fmt.Sprintf("Reward redemption: User=%d, Points=₦%d, Original=₦%d, Final=₦%d",
				userInfo.ID, pointsUsed, originalAmount, finalAmount))
		}
	}

	// ========================================================================
	// CREATE BILL TRANSACTION (with discounted amount)
	// ========================================================================
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, tx.BillTransaction{
		SourceWalletID:  walletID,
		SentAmount:      finalAmount, // User pays discounted amount
		Description:     "airtime-purchase",
		Type:            tx.Airtime,
		ServiceID:       request.ServiceID,
		ServiceCurrency: "NGN",
	})
	if err != nil {
		b.server.logger.Error(err)
		if walletErr, ok := err.(*wallet.WalletError); ok {
			if walletErr.Error() == wallet.ErrWalletNotFound.Error() {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("wallet not found"))
				return
			}
			if walletErr.Error() == wallet.ErrNotYours.Error() {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("wallet not found"))
				return
			}
			if walletErr.Error() == wallet.ErrInsufficientFunds.Error() {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("insufficient funds"))
				return
			}
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// ========================================================================
	// PURCHASE FROM PROVIDER (with original amount)
	// ========================================================================
	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	purchaseRequestID := time.Now().UTC().Add(time.Hour * 1).Format("20060102150405")

	transaction, err := billProv.BuyAirtime(bills.PurchaseAirtimeRequest{
		ServiceID: request.ServiceID,
		Phone:     request.Phone,
		RequestID: purchaseRequestID,
		Amount:    request.Amount, // Provider receives original amount
	})
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	if _, err := b.server.queries.WithTx(dbTx).UpdateBillServiceTransactionID(ctx, db.UpdateBillServiceTransactionIDParams{
		ServiceTransactionID: sql.NullString{
			String: transaction.TransactionID,
			Valid:  true,
		},
		TransactionID: tInfo.ID,
	}); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// ========================================================================
	// COMPLETE REWARD REDEMPTION (if applicable)
	// ========================================================================
	if redemptionApplied {
		err = b.transactionService.CompleteRewardRedemption(
			ctx, dbTx, int32(userInfo.ID), tInfo.ID,
			pointsUsed, originalAmount, finalAmount,
			"airtime", request.ServiceID,
		)
		if err != nil {
			// Log but don't fail transaction
			b.server.logger.Error("Failed to complete reward redemption:", err)
		}
	}

	// ========================================================================
	// AWARD REWARD POINTS (based on amount paid)
	// ========================================================================
	pointsEarned, err = b.transactionService.AwardRewardPoints(
		ctx, dbTx, int32(userInfo.ID), tInfo.ID, finalAmount, "airtime",
	)
	if err != nil {
		// Log but don't fail transaction
		b.server.logger.Error("Failed to award reward points:", err)
	}

	// ========================================================================
	// COMMIT TRANSACTION
	// ========================================================================
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		// audit log
		logEntry := audit.NewTransactionLog(ctx, audit.EventAirtimePurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, float64(request.Amount), "NGN", false)
		logEntry.Metadata = map[string]any{
			"Reason": err.Error(),
		} // TODO:
		b.audit.Log(logEntry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// UPdate streak
	// b.updateStreakAsync(userInfo.ID, tInfo.ID, tx.Airtime)
	err = b.streakScheduler.UpdateStreakOnTransaction(ctx, userInfo.ID, tInfo.ID, "airtime")
	if err != nil {
		b.server.logger.Error("Failed to update streak:", err)
	}

	// audit log
	logEntry := audit.NewTransactionLog(ctx, audit.EventAirtimePurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, float64(request.Amount), "NGN", true)
	logEntry.Metadata = map[string]any{} // TODO:
	b.audit.Log(logEntry)

	// ========================================================================
	// SEND NOTIFICATION
	// ========================================================================
	notificationMsg := fmt.Sprintf("You have received an airtime of %d to %s", request.Amount, request.Phone)
	if pointsUsed.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You saved ₦%s using reward points", pointsUsed.String())
	}
	if pointsEarned.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You earned ₦%s in reward points!", pointsEarned.String())
	}

	b.notifr.Create(ctx, int32(userInfo.ID), "Airtime Purchase", notificationMsg)

	// TODO: add push notofication

	b.server.logger.Info("transaction (airtime purchase) completed successfully", map[string]interface{}{
		"transaction_id":   tInfo.ID,
		"user_id":          userInfo.ID,
		"original_amount":  originalAmount.InexactFloat64(),
		"discount_applied": pointsUsed,
		"final_paid":       finalAmount.InexactFloat64(),
		"points_earned":    pointsEarned,
	})

	// ========================================================================
	// RETURN ENHANCED RESPONSE
	// ========================================================================
	response := map[string]any{
		"transaction":       tInfo,
		"original_amount":   originalAmount.InexactFloat64(),
		"discount_applied":  pointsUsed,
		"final_amount_paid": finalAmount.InexactFloat64(),
		"points_used":       pointsUsed,
		"points_earned":     pointsEarned,
		"message":           notificationMsg,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase airtime successful", response))

}

// buyData godoc
// @Summary Buy data
// @Description Buy data
// @Tags bills
// @Accept json
// @Produce json
// @Param wallet_id query string true "Wallet ID"
// @Param service_id query string true "Service ID"
// @Param phone query string true "Phone"
// @Param variation_code query string true "Variation Code"
// @Param pin query string true "Pin"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/buy-data [post]
// @Security BearerAuth
func (b *Bills) buyData(ctx *gin.Context) {
	request := struct {
		WalletID        string  `json:"wallet_id" binding:"required"`
		ServiceID       string  `json:"service_id" binding:"required"`
		Phone           string  `json:"phone" binding:"required"`
		VariationCode   string  `json:"variation_code" binding:"required"`
		Pin             string  `json:"pin" binding:"required"`
		UseRewardPoints bool    `json:"use_reward_points"`
		PointsToUse     float32 `json:"points_to_use"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	// Start transaction
	dbTx, err := b.server.queries.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	defer dbTx.Rollback()

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Pull user information
	userInfo, err := b.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, userInfo.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	variations, err := b.server.redis.GetVariations(ctx, fmt.Sprintf("variations:%s", request.ServiceID))
	if err != nil {
		b.server.logger.Error(fmt.Sprintf("failed to get variations from cache: %v", err))
	}

	if len(variations) == 0 {
		provider, exists := b.server.provider.GetProvider(providers.VTPass)
		if !exists {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
			return
		}

		billProv, ok := provider.(*bills.VTPassProvider)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
			return
		}

		remoteVariations, err := billProv.GetServiceVariation(request.ServiceID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if remoteVariations != nil {
			variations = make([]models.BillVariation, len(remoteVariations))
			for i, variation := range remoteVariations {
				variations[i] = models.BillVariation{
					VariationCode:   variation.VariationCode,
					Name:            variation.Name,
					VariationAmount: variation.VariationAmount,
					FixedPrice:      variation.FixedPrice,
				}
			}

			err = b.server.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", request.ServiceID), variations)
			if err != nil {
				b.server.logger.Error(fmt.Sprintf("failed to store variations in cache: %v", err))
			}
		}
	}

	// b.server.logger.Info("variations", variations)

	var selectedVariation *models.BillVariation
	for _, variation := range variations {
		if variation.VariationCode == request.VariationCode {
			selectedVariation = &variation
			break
		}
	}

	if selectedVariation == nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid variation code"))
		return
	}

	amount, err := decimal.NewFromString(selectedVariation.VariationAmount)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid variation amount"))
		return
	}

	// ========================================================================
	// REWARD POINTS PROCESSING
	// ========================================================================
	originalAmount := amount
	finalAmount := originalAmount
	var pointsUsed decimal.Decimal
	var pointsEarned decimal.Decimal
	redemptionApplied := false

	pointsToUseDecimal := decimal.NewFromFloat32(request.PointsToUse)

	// STEP 1: Validate and apply reward redemption if requested
	if request.UseRewardPoints && request.PointsToUse > 0 {
		var err error
		finalAmount, redemptionApplied, err = b.transactionService.ProcessRewardRedemption(
			ctx, dbTx, &userInfo, pointsToUseDecimal, originalAmount,
		)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if redemptionApplied {
			pointsUsed = pointsToUseDecimal
			b.server.logger.Info(fmt.Sprintf("Reward redemption: User=%d, Points=₦%d, Original=₦%d, Final=₦%d",
				userInfo.ID, pointsUsed, originalAmount, finalAmount))
		}
	}

	// ========================================================================
	// CREATE BILL TRANSACTION (with discounted amount)
	// ========================================================================

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, tx.BillTransaction{
		/// SentAmount is still in it's potential stage, Fees etc. should be added before debit
		SourceWalletID:  walletID,
		SentAmount:      finalAmount,
		Description:     "data-purchase",
		Type:            tx.Data,
		ServiceID:       request.ServiceID,
		ServiceCurrency: "NGN",
	})
	if err != nil {
		b.server.logger.Error(err)
		if walletErr, ok := err.(*wallet.WalletError); ok {
			if walletErr.Error() == wallet.ErrWalletNotFound.Error() {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("wallet not found"))
				return
			}
			if walletErr.Error() == wallet.ErrNotYours.Error() {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("wallet not found"))
				return
			}
			if walletErr.Error() == wallet.ErrInsufficientFunds.Error() {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("insufficient funds"))
				return
			}
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	purchaseRequestID := time.Now().UTC().Add(time.Hour * 1).Format("20060102150405")

	transaction, err := billProv.BuyData(bills.PurchaseDataRequest{
		ServiceID:     request.ServiceID,
		BillersCode:   request.Phone,
		RequestID:     purchaseRequestID,
		VariationCode: request.VariationCode,
		Phone:         request.Phone,
		Amount:        amount.IntPart(),
	})
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	if _, err := b.server.queries.WithTx(dbTx).UpdateBillServiceTransactionID(ctx, db.UpdateBillServiceTransactionIDParams{
		ServiceTransactionID: sql.NullString{
			String: transaction.TransactionID,
			Valid:  true,
		},
		TransactionID: tInfo.ID,
	}); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// ========================================================================
	// COMPLETE REWARD REDEMPTION (if applicable)
	// ========================================================================
	if redemptionApplied {
		err = b.transactionService.CompleteRewardRedemption(
			ctx, dbTx, int32(userInfo.ID), tInfo.ID,
			pointsUsed, originalAmount, finalAmount,
			"data", request.ServiceID,
		)
		if err != nil {
			// Log but don't fail transaction
			b.server.logger.Error("Failed to complete reward redemption:", err)
		}
	}

	// ========================================================================
	// AWARD REWARD POINTS (based on amount paid)
	// ========================================================================
	pointsEarned, err = b.transactionService.AwardRewardPoints(
		ctx, dbTx, int32(userInfo.ID), tInfo.ID, finalAmount, "data",
	)
	if err != nil {
		// Log but don't fail transaction
		// todo: audit log here
		b.server.logger.Error("Failed to award reward points:", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		// audit log
		logEntry := audit.NewTransactionLog(ctx, audit.EventDataPurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, amount.InexactFloat64(), "NGN", false)
		logEntry.Metadata = map[string]any{
			"Reason": err.Error(),
		}
		b.audit.Log(logEntry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// update streak
	b.updateStreakAsync(userInfo.ID, tInfo.ID, tx.Data)

	// audit log
	logEntry := audit.NewTransactionLog(ctx, audit.EventDataPurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, amount.InexactFloat64(), "NGN", true)
	b.audit.Log(logEntry)

	notificationMsg := fmt.Sprintf("You have received a data of %s to %s", selectedVariation.VariationAmount, request.Phone)
	if pointsUsed.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You saved ₦%s using reward points", pointsUsed.String())
	}
	if pointsEarned.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You earned ₦%s in reward points!", pointsEarned.String())
	}

	// TODO: push notif
	b.notifr.Create(ctx, int32(userInfo.ID), "Data Purchase", notificationMsg)

	b.server.logger.Info("transaction (data purchase) completed successfully", tInfo)
	// ========================================================================
	// RETURN ENHANCED RESPONSE
	// ========================================================================
	response := map[string]any{
		"transaction":       tInfo,
		"original_amount":   originalAmount.InexactFloat64(),
		"discount_applied":  pointsUsed,
		"final_amount_paid": finalAmount.InexactFloat64(),
		"points_used":       pointsUsed,
		"points_earned":     pointsEarned,
		"message":           notificationMsg,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase data successful", response))
}

// getCustomerInfo godoc
// @Summary Get customer info
// @Description Get customer info
// @Tags bills
// @Accept json
// @Produce json
// @Param service_id query string true "Service ID"
// @Param billers_code query string true "Billers Code"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/customer-info [post]
// @Security BearerAuth
func (b *Bills) getCustomerInfo(ctx *gin.Context) {
	request := struct {
		ServiceID   string `json:"service_id" binding:"required"`
		BillersCode string `json:"billers_code" binding:"required"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	customerInfo, err := billProv.GetCustomerInfo(bills.GetCustomerInfoRequest{
		ServiceID:   request.ServiceID,
		BillersCode: request.BillersCode,
	})
	if err != nil {
		b.server.logger.Error(fmt.Sprintf("error fetching customer info: %s", err.Error()))
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched customer info", models.ToCustomerInfoResponse(*customerInfo)))
}

// buyTVSubscription godoc
// @Summary Buy TV subscription
// @Description Buy TV subscription
// @Tags bills
// @Accept json
// @Produce json
// @Param wallet_id query string true "Wallet ID"
// @Param service_id query string true "Service ID"
// @Param billers_code query string true "Billers Code"
// @Param subscription_type query string true "Subscription Type"
// @Param variation_code query string true "Variation Code"
// @Param pin query string true "Pin"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/buy-tv [post]
// @Security BearerAuth
func (b *Bills) buyTVSubscription(ctx *gin.Context) {
	request := struct {
		WalletID         string  `json:"wallet_id" binding:"required"`
		ServiceID        string  `json:"service_id" binding:"required"`
		BillersCode      string  `json:"billers_code" binding:"required"`
		SubscriptionType string  `json:"subscription_type" binding:"required"`
		VariationCode    string  `json:"variation_code" binding:"required"`
		Pin              string  `json:"pin" binding:"required"`
		UseRewardPoints  bool    `json:"use_reward_points"`
		PointsToUse      float32 `json:"points_to_use"`
	}{}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	// Start transaction
	dbTx, err := b.server.queries.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	defer dbTx.Rollback()

	// Pull user information
	userInfo, err := b.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, userInfo.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	variations, err := b.server.redis.GetVariations(ctx, fmt.Sprintf("variations:%s", request.ServiceID))
	if err != nil {
		b.server.logger.Error(fmt.Sprintf("failed to get variations from cache: %v", err))
	}

	if len(variations) == 0 {
		provider, exists := b.server.provider.GetProvider(providers.VTPass)
		if !exists {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
			return
		}

		billProv, ok := provider.(*bills.VTPassProvider)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
			return
		}

		remoteVariations, err := billProv.GetServiceVariation(request.ServiceID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if remoteVariations != nil {
			variations = make([]models.BillVariation, len(remoteVariations))
			for i, variation := range remoteVariations {
				variations[i] = models.BillVariation{
					VariationCode:   variation.VariationCode,
					Name:            variation.Name,
					VariationAmount: variation.VariationAmount,
					FixedPrice:      variation.FixedPrice,
				}
			}

			err = b.server.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", request.ServiceID), variations)
			if err != nil {
				b.server.logger.Error(fmt.Sprintf("failed to store variations in cache: %v", err))
			}
		}
	}

	var selectedVariation *models.BillVariation
	for _, variation := range variations {
		if variation.VariationCode == request.VariationCode {
			selectedVariation = &variation
			break
		}
	}

	if selectedVariation == nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid variation code"))
		return
	}

	amount, err := decimal.NewFromString(selectedVariation.VariationAmount)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid variation amount"))
		return
	}

	// ========================================================================
	// REWARD POINTS PROCESSING
	// ========================================================================
	originalAmount := amount
	finalAmount := originalAmount
	var pointsUsed decimal.Decimal
	var pointsEarned decimal.Decimal
	redemptionApplied := false

	pointsToUseDecimal := decimal.NewFromFloat32(request.PointsToUse)

	// STEP 1: Validate and apply reward redemption if requested
	if request.UseRewardPoints && request.PointsToUse > 0 {
		var err error
		finalAmount, redemptionApplied, err = b.transactionService.ProcessRewardRedemption(
			ctx, dbTx, &userInfo, pointsToUseDecimal, originalAmount,
		)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if redemptionApplied {
			pointsUsed = pointsToUseDecimal
			b.server.logger.Info(fmt.Sprintf("Reward redemption: User=%d, Points=₦%d, Original=₦%d, Final=₦%d",
				userInfo.ID, pointsUsed, originalAmount, finalAmount))
		}
	}

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, tx.BillTransaction{
		SourceWalletID:  walletID,
		SentAmount:      finalAmount,
		Description:     "tv-subscription-purchase",
		Type:            tx.TV,
		ServiceID:       request.ServiceID,
		ServiceCurrency: "NGN",
	})
	if err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	transaction, err := billProv.BuyTVSubscription(bills.BuyTVSubscriptionRequest{
		ServiceID:        request.ServiceID,
		BillersCode:      request.BillersCode,
		VariationCode:    request.VariationCode,
		SubscriptionType: request.SubscriptionType,
		Amount:           amount.IntPart(),
		Phone:            userInfo.PhoneNumber,
		RequestID:        time.Now().UTC().Add(time.Hour * 1).Format("20060102150405"),
	})
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	if _, err := b.server.queries.WithTx(dbTx).UpdateBillServiceTransactionID(ctx, db.UpdateBillServiceTransactionIDParams{
		ServiceTransactionID: sql.NullString{
			String: transaction.TransactionID,
			Valid:  true,
		},
		TransactionID: tInfo.ID,
	}); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if transaction.Status == "pending" {
		b.server.logger.Error("tv subscription purchase status - pending")
		if _, err := b.server.queries.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			Status: transaction.Status,
			ID:     tInfo.ID,
		}); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}

		// Change transaction status to pending
		tInfo.Status = transaction.Status

		// Commit transaction
		if err := dbTx.Commit(); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}

		ctx.JSON(http.StatusOK, basemodels.NewSuccess("tv subscription purchase pending", tInfo))
		return
	}

	if transaction.Status == "failed" {
		b.server.logger.Error("tv subscription purchase status - failed")
		if _, err := b.server.queries.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			Status: "failed",
			ID:     tInfo.ID,
		}); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}

		// Don't commit failed transactions
		if err := dbTx.Rollback(); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}

		ctx.JSON(http.StatusBadRequest, basemodels.NewError("tv subscription purchase failed"))
		return
	}

	// ========================================================================
	// COMPLETE REWARD REDEMPTION (if applicable)
	// ========================================================================
	if redemptionApplied {
		err = b.transactionService.CompleteRewardRedemption(
			ctx, dbTx, int32(userInfo.ID), tInfo.ID,
			pointsUsed, originalAmount, finalAmount,
			"tv_subscription", request.ServiceID,
		)
		if err != nil {
			// Log but don't fail transaction
			b.server.logger.Error("Failed to complete reward redemption:", err)
		}
	}

	// ========================================================================
	// AWARD REWARD POINTS (based on amount paid)
	// ========================================================================
	pointsEarned, err = b.transactionService.AwardRewardPoints(
		ctx, dbTx, int32(userInfo.ID), tInfo.ID, finalAmount, "tv_subscription",
	)
	if err != nil {
		// Log but don't fail transaction
		b.server.logger.Error("Failed to award reward points:", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)

		// audit log
		logEntry := audit.NewTransactionLog(ctx, audit.EventTVSubscriptionPurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, amount.InexactFloat64(), "NGN", false)
		logEntry.Metadata = map[string]any{
			"Reason": err.Error(),
		} // TODO:
		b.audit.Log(logEntry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// update streak
	b.updateStreakAsync(userInfo.ID, tInfo.ID, tx.TV)

	// audit log
	logEntry := audit.NewTransactionLog(ctx, audit.EventTVSubscriptionPurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, amount.InexactFloat64(), "NGN", true)
	logEntry.Metadata = map[string]any{} // TODO:
	b.audit.Log(logEntry)

	// ========================================================================
	// SEND NOTIFICATION
	// ========================================================================
	notificationMsg := fmt.Sprintf("TV subscription of %s is successful", amount)
	if pointsUsed.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You saved ₦%s using reward points", pointsUsed.String())
	}
	if pointsEarned.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You earned ₦%s in reward points!", pointsEarned.String())
	}

	b.notifr.Create(ctx, int32(userInfo.ID), "Successful TV subscription", notificationMsg)

	b.server.logger.Info("transaction (tv subscription purchase) completed successfully", tInfo)

	// ========================================================================
	// RETURN ENHANCED RESPONSE
	// ========================================================================
	response := map[string]any{
		"transaction":       tInfo,
		"original_amount":   originalAmount.InexactFloat64(),
		"discount_applied":  pointsUsed,
		"final_amount_paid": finalAmount.InexactFloat64(),
		"points_used":       pointsUsed,
		"points_earned":     pointsEarned,
		"message":           notificationMsg,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase tv subscription successful", response))
}

// getCustomerMeterInfo godoc
// @Summary Get customer meter info
// @Description Get customer meter info
// @Tags bills
// @Accept json
// @Produce json
// @Param service_id query string true "Service ID"
// @Param billers_code query string true "Billers Code"
// @Param type query string true "Type"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/customer-meter-info [post]
// @Security BearerAuth
func (b *Bills) getCustomerMeterInfo(ctx *gin.Context) {
	request := struct {
		ServiceID   string `json:"service_id" binding:"required"`
		BillersCode string `json:"billers_code" binding:"required"`
		Type        string `json:"type" binding:"required"` // "postpaid" or "prepaid"
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	customerMeterInfo, err := billProv.GetCustomerMeterInfo(bills.GetCustomerMeterInfoRequest{
		ServiceID:   request.ServiceID,
		BillersCode: request.BillersCode,
		Type:        request.Type,
	})
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched customer meter info", models.ToMeterInfoResponse(*customerMeterInfo)))
}

// buyElectricity godoc
// @Summary Buy electricity
// @Description Buy electricity
// @Tags bills
// @Accept json
// @Produce json
// @Param wallet_id query string true "Wallet ID"
// @Param service_id query string true "Service ID"
// @Param billers_code query string true "Billers Code"
// @Param variation_code query string true "Variation Code"
// @Param amount query int true "Amount"
// @Param pin query string true "Pin"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/bills/buy-electricity [post]
// @Security BearerAuth
func (b *Bills) buyElectricity(ctx *gin.Context) {
	request := struct {
		WalletID        string  `json:"wallet_id" binding:"required"`
		ServiceID       string  `json:"service_id" binding:"required"`
		BillersCode     string  `json:"billers_code" binding:"required"`
		VariationCode   string  `json:"variation_code" binding:"required"`
		Amount          float64 `json:"amount" binding:"required"`
		Pin             string  `json:"pin" binding:"required"`
		UseRewardPoints bool    `json:"use_reward_points"`
		PointsToUse     float32 `json:"points_to_use"`
	}{}
	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	// Start transaction
	dbTx, err := b.server.queries.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	defer dbTx.Rollback()

	// Pull user information
	userInfo, err := b.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, userInfo.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	variations, err := b.server.redis.GetVariations(ctx, fmt.Sprintf("variations:%s", request.ServiceID))
	if err != nil {
		b.server.logger.Error(fmt.Sprintf("failed to get variations from cache: %v", err))
	}

	if len(variations) == 0 {
		provider, exists := b.server.provider.GetProvider(providers.VTPass)
		if !exists {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
			return
		}

		billProv, ok := provider.(*bills.VTPassProvider)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
			return
		}

		remoteVariations, err := billProv.GetServiceVariation(request.ServiceID)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if remoteVariations != nil {
			variations = make([]models.BillVariation, len(remoteVariations))
			for i, variation := range remoteVariations {
				variations[i] = models.BillVariation{
					VariationCode:   variation.VariationCode,
					Name:            variation.Name,
					VariationAmount: variation.VariationAmount,
					FixedPrice:      variation.FixedPrice,
				}
			}

			err = b.server.redis.StoreVariations(ctx, fmt.Sprintf("variations:%s", request.ServiceID), variations)
			if err != nil {
				b.server.logger.Error(fmt.Sprintf("failed to store variations in cache: %v", err))
			}
		}
	}

	var selectedVariation *models.BillVariation
	for _, variation := range variations {
		if variation.VariationCode == request.VariationCode {
			selectedVariation = &variation
			break
		}
	}

	if selectedVariation == nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid variation code"))
		return
	}

	// ========================================================================
	// REWARD POINTS PROCESSING
	// ========================================================================
	originalAmount := decimal.NewFromFloat(request.Amount)
	finalAmount := originalAmount
	var pointsUsed decimal.Decimal
	var pointsEarned decimal.Decimal
	redemptionApplied := false

	pointsToUseDecimal := decimal.NewFromFloat32(request.PointsToUse)

	// STEP 1: Validate and apply reward redemption if requested
	if request.UseRewardPoints && request.PointsToUse > 0 {
		var err error
		finalAmount, redemptionApplied, err = b.transactionService.ProcessRewardRedemption(
			ctx, dbTx, &userInfo, pointsToUseDecimal, originalAmount,
		)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}

		if redemptionApplied {
			pointsUsed = pointsToUseDecimal
			b.server.logger.Info(fmt.Sprintf("Reward redemption: User=%d, Points=₦%d, Original=₦%d, Final=₦%d",
				userInfo.ID, pointsUsed, originalAmount, finalAmount))
		}
	}

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, tx.BillTransaction{
		SourceWalletID:  walletID,
		SentAmount:      finalAmount,
		Description:     "electricity-purchase",
		Type:            tx.Other,
		ServiceID:       request.ServiceID,
		ServiceCurrency: "NGN",
	})
	if err != nil {
		b.server.logger.Error(err)
		if err.Error() == wallet.ErrInsufficientFunds.Error() {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}
		if err.Error() == wallet.ErrWalletNotFound.Error() {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	purchTrans, err := billProv.BuyElectricity(bills.PurchaseElectricityRequest{
		ServiceID:     request.ServiceID,
		BillersCode:   request.BillersCode,
		VariationCode: request.VariationCode,
		Amount:        request.Amount,
		Phone:         userInfo.PhoneNumber,
		RequestID:     time.Now().UTC().Add(time.Hour * 1).Format("20060102150405"),
	})
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	if _, err := b.server.queries.WithTx(dbTx).UpdateBillServiceTransactionID(ctx, db.UpdateBillServiceTransactionIDParams{
		ServiceTransactionID: sql.NullString{
			String: purchTrans.Content.Transaction.TransactionID,
			Valid:  true,
		},
		TransactionID: tInfo.ID,
	}); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	units := fmt.Sprintf("%v", purchTrans.Units)
	tokenAmountString := fmt.Sprintf("%v", purchTrans.TokenAmount)
	var tokenAmount float64
	if tokenAmountString != "" {
		temp, err := decimal.NewFromString(tokenAmountString)
		if err != nil {
			tokenAmount = 0
		} else {
			tokenAmount, _ = temp.Float64()
		}
	}

	fixChargeAmountString := fmt.Sprintf("%v", purchTrans.FixChargeAmount)
	taxAmountString := fmt.Sprintf("%v", purchTrans.TaxAmount)

	/// Update transaction metadata
	tInfo.Metadata.ElectricityMetadata = &tx.ElectricityMetadataResponse{
		PurchasedCode:     purchTrans.Content.Transaction.TransactionID,
		CustomerName:      purchTrans.CustomerName,
		CustomerAddress:   purchTrans.CustomerAddress,
		Token:             purchTrans.Token,
		TokenAmount:       tokenAmount,
		ExchangeReference: purchTrans.ExchangeReference,
		ResetToken:        purchTrans.ResetToken,
		ConfigureToken:    purchTrans.ConfigureToken,
		Units:             units,
		FixChargeAmount:   &fixChargeAmountString,
		Tariff:            purchTrans.Tariff,
		TaxAmount:         &taxAmountString,
	}

	if purchTrans.Content.Transaction.Status == "pending" {
		b.server.logger.Error("electricity purchase status - pending")
		if _, err := b.server.queries.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			Status: purchTrans.Content.Transaction.Status,
			ID:     tInfo.ID,
		}); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Change transaction status to pending
		tInfo.Status = purchTrans.Content.Transaction.Status
		// Commit transaction
		if err := dbTx.Commit(); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		ctx.JSON(http.StatusOK, basemodels.NewSuccess("electricity purchase pending", tInfo))
		return
	}

	if purchTrans.Content.Transaction.Status == "failed" {
		b.server.logger.Error("electricity purchase status - failed")
		if _, err := b.server.queries.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			Status: "failed",
			ID:     tInfo.ID,
		}); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Don't commit failed transactions
		if err := dbTx.Rollback(); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		ctx.JSON(http.StatusBadRequest, basemodels.NewError("electricity purchase failed"))
		return
	}

	// ========================================================================
	// COMPLETE REWARD REDEMPTION (if applicable)
	// ========================================================================
	if redemptionApplied {
		err = b.transactionService.CompleteRewardRedemption(
			ctx, dbTx, int32(userInfo.ID), tInfo.ID,
			pointsUsed, originalAmount, finalAmount,
			"electricity", request.ServiceID,
		)
		if err != nil {
			// Log but don't fail transaction
			b.server.logger.Error("Failed to complete reward redemption:", err)
		}
	}

	// ========================================================================
	// AWARD REWARD POINTS (based on amount paid)
	// ========================================================================
	pointsEarned, err = b.transactionService.AwardRewardPoints(
		ctx, dbTx, int32(userInfo.ID), tInfo.ID, finalAmount, "electricity",
	)
	if err != nil {
		// Log but don't fail transaction
		b.server.logger.Error("Failed to award reward points:", err)
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)

		logEntry := audit.NewTransactionLog(ctx, audit.EventElectricityPurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, request.Amount, "NGN", false)
		logEntry.Metadata = map[string]any{
			"Reason": err.Error(),
		} // TODO:
		b.audit.Log(logEntry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// update streak
	b.updateStreakAsync(userInfo.ID, tInfo.ID, tx.Electricity)

	// audit log
	logEntry := audit.NewTransactionLog(ctx, audit.EventElectricityPurchase, tInfo.ID.String(), activeUser.Role, activeUser.UserID, request.Amount, "NGN", true)
	logEntry.Metadata = map[string]any{} // TODO:
	b.audit.Log(logEntry)

	// ========================================================================
	// SEND NOTIFICATION
	// ========================================================================
	notificationMsg := fmt.Sprintf("Electricity subscription of %f is successful", request.Amount)
	if pointsUsed.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You saved ₦%s using reward points", pointsUsed.String())
	}
	if pointsEarned.GreaterThan(decimal.Zero) {
		notificationMsg += fmt.Sprintf(". You earned ₦%s in reward points!", pointsEarned.String())
	}

	// TTODO: push notifications
	b.notifr.Create(ctx, int32(userInfo.ID), "Successful Electricity Subscription", notificationMsg)

	b.server.logger.Info("transaction (electricity purchase) completed successfully", tInfo)

	// ========================================================================
	// RETURN ENHANCED RESPONSE
	// ========================================================================
	response := map[string]any{
		"transaction":       tInfo,
		"original_amount":   originalAmount.InexactFloat64(),
		"discount_applied":  pointsUsed,
		"final_amount_paid": finalAmount.InexactFloat64(),
		"points_used":       pointsUsed,
		"points_earned":     pointsEarned,
		"message":           notificationMsg,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase electricity successful", response))
}
