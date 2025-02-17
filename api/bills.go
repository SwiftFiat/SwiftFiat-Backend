package api

import (
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
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Bills struct {
	server             *Server
	transactionService *transaction.TransactionService
}

func (b Bills) router(server *Server) {
	b.server = server
	walletService := wallet.NewWalletServiceWithCache(b.server.queries, b.server.logger, b.server.redis)
	currencyService := currency.NewCurrencyService(b.server.queries, b.server.logger)
	b.transactionService = transaction.NewTransactionService(
		b.server.queries,
		currencyService,
		walletService,
		b.server.logger,
	)

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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched service variations", variations))
}

func (b *Bills) buyAirtime(ctx *gin.Context) {
	request := struct {
		WalletID  string `json:"wallet_id" binding:"required"`
		ServiceID string `json:"service_id" binding:"required"`
		Phone     string `json:"phone" binding:"required"`
		Amount    int64  `json:"amount" binding:"required"`
		Pin       string `json:"pin" binding:"required"`
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

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, transaction.BillTransaction{
		/// SentAmount is still in it's potential stage, Fees etc. should be added before debit
		SourceWalletID:  walletID,
		SentAmount:      decimal.NewFromInt(request.Amount),
		Description:     "airtime-purchase",
		Type:            transaction.Airtime,
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

	transaction, err := billProv.BuyAirtime(bills.PurchaseAirtimeRequest{
		ServiceID: request.ServiceID,
		Phone:     request.Phone,
		RequestID: purchaseRequestID,
		Amount:    request.Amount,
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

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	b.server.logger.Info("transaction (airtime purchase) completed successfully", tInfo)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase airtime successful", tInfo))
}

func (b *Bills) buyData(ctx *gin.Context) {
	request := struct {
		WalletID  string `json:"wallet_id" binding:"required"`
		ServiceID string `json:"service_id" binding:"required"`
		Phone     string `json:"phone" binding:"required"`
		// Amount        int64  `json:"amount" binding:"required"` -- User's can inject arbitrary amounts
		VariationCode string `json:"variation_code" binding:"required"`
		Pin           string `json:"pin" binding:"required"`
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
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	fmt.Println(variations)

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

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, transaction.BillTransaction{
		/// SentAmount is still in it's potential stage, Fees etc. should be added before debit
		SourceWalletID:  walletID,
		SentAmount:      amount,
		Description:     "data-purchase",
		Type:            transaction.Data,
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

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	b.server.logger.Info("transaction (data purchase) completed successfully", tInfo)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase data successful", tInfo))
}

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

func (b *Bills) buyTVSubscription(ctx *gin.Context) {
	request := struct {
		WalletID         string `json:"wallet_id" binding:"required"`
		ServiceID        string `json:"service_id" binding:"required"`
		BillersCode      string `json:"billers_code" binding:"required"`
		SubscriptionType string `json:"subscription_type" binding:"required"`
		VariationCode    string `json:"variation_code" binding:"required"`
		Pin              string `json:"pin" binding:"required"`
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
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
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

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, transaction.BillTransaction{
		SourceWalletID:  walletID,
		SentAmount:      amount,
		Description:     "tv-subscription-purchase",
		Type:            transaction.TV,
		ServiceID:       request.ServiceID,
		ServiceCurrency: "NGN",
	})
	if err != nil {
		b.server.logger.Error(err)
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
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if transaction.Status == "pending" {
		b.server.logger.Error("tv subscription purchase status - pending")
		if _, err := b.server.queries.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			Status: transaction.Status,
			ID:     tInfo.ID,
		}); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Change transaction status to pending
		tInfo.Status = transaction.Status

		// Commit transaction
		if err := dbTx.Commit(); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
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
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Don't commit failed transactions
		if err := dbTx.Rollback(); err != nil {
			b.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		ctx.JSON(http.StatusBadRequest, basemodels.NewError("tv subscription purchase failed"))
		return
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	b.server.logger.Info("transaction (tv subscription purchase) completed successfully", tInfo)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase tv subscription successful", tInfo))
}

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

func (b *Bills) buyElectricity(ctx *gin.Context) {
	request := struct {
		WalletID      string  `json:"wallet_id" binding:"required"`
		ServiceID     string  `json:"service_id" binding:"required"`
		BillersCode   string  `json:"billers_code" binding:"required"`
		VariationCode string  `json:"variation_code" binding:"required"`
		Amount        float64 `json:"amount" binding:"required"`
		Pin           string  `json:"pin" binding:"required"`
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
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
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

	// Create BillTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, transaction.BillTransaction{
		SourceWalletID:  walletID,
		SentAmount:      decimal.NewFromFloat(request.Amount),
		Description:     "electricity-purchase",
		Type:            transaction.Electricity,
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

	/// Update transaction metadata
	tInfo.Metadata.ElectricityMetadata = &transaction.ElectricityMetadataResponse{
		PurchasedCode:     purchTrans.Content.Transaction.TransactionID,
		CustomerName:      purchTrans.CustomerName,
		CustomerAddress:   purchTrans.CustomerAddress,
		Token:             purchTrans.Token,
		TokenAmount:       tokenAmount,
		ExchangeReference: purchTrans.ExchangeReference,
		ResetToken:        purchTrans.ResetToken,
		ConfigureToken:    purchTrans.ConfigureToken,
		Units:             units,
		FixChargeAmount:   purchTrans.FixChargeAmount,
		Tariff:            purchTrans.Tariff,
		TaxAmount:         purchTrans.TaxAmount,
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

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	b.server.logger.Info("transaction (electricity purchase) completed successfully", tInfo)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase electricity successful", tInfo))
}
