package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
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
	serverGroupV1.GET("categories", AuthenticatedMiddleware(), b.getCategories)
	serverGroupV1.GET("services", AuthenticatedMiddleware(), b.getServices)
	serverGroupV1.GET("service-variation", AuthenticatedMiddleware(), b.getServiceVariations)
	serverGroupV1.POST("buy-airtime", AuthenticatedMiddleware(), b.buyAirtime)
	serverGroupV1.POST("buy-data", AuthenticatedMiddleware(), b.buyData)
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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched bill services", services))
}

func (b *Bills) getServiceVariations(ctx *gin.Context) {
	serviceID := ctx.Query("serviceID")

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

	variations, err := billProv.GetServiceVariation(serviceID)
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
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
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
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

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	// Create GiftCardTransaction
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

	purchaseRequestID := time.Now().Format("20060102150405")

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
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
	}

	b.server.logger.Info("transaction (airtime purchase) completed successfully", tInfo)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase airtime successful", tInfo))
}

func (b *Bills) buyData(ctx *gin.Context) {
	request := struct {
		WalletID      string `json:"wallet_id" binding:"required"`
		ServiceID     string `json:"service_id" binding:"required"`
		Phone         string `json:"phone" binding:"required"`
		Amount        int64  `json:"amount" binding:"required"`
		VariationCode string `json:"variation_code" binding:"required"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
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

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	// Create GiftCardTransaction
	tInfo, err := b.transactionService.CreateBillPurchaseTransactionWithTx(ctx, dbTx, &userInfo, transaction.BillTransaction{
		/// SentAmount is still in it's potential stage, Fees etc. should be added before debit
		SourceWalletID:  walletID,
		SentAmount:      decimal.NewFromInt(request.Amount),
		Description:     "data-purchase",
		Type:            transaction.Data,
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

	purchaseRequestID := time.Now().Format("20060102150405")

	transaction, err := billProv.BuyData(bills.PurchaseDataRequest{
		ServiceID:     request.ServiceID,
		BillersCode:   request.Phone,
		RequestID:     purchaseRequestID,
		VariationCode: request.VariationCode,
		Phone:         request.Phone,
		Amount:        request.Amount,
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
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
	}

	b.server.logger.Info("transaction (data purchase) completed successfully", tInfo)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase data successful", tInfo))
}
