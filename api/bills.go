package api

import (
	"errors"
	"fmt"
	"net/http"

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
)

type Bills struct {
	server             *Server
	transactionService *tx.TransactionService
	notifr             *service.Notification
	push               *service.PushNotificationService
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
	b.push = server.pushNotification

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

// mapBillError converts typed service errors to appropriate HTTP status codes.
// Centralised here so all four handlers stay consistent.
func mapBillError(ctx *gin.Context, err error) {
	switch {
	case err.Error() == "Err_AIRTIME_AMOUNT_EXCEEDED_FOR_TIER_1", err.Error() == "Err_DATA_AMOUNT_EXCEEDED_FOR_TIER_1", err.Error() == "Err_ELECTRICITY_AMOUNT_EXCEEDED_FOR_TIER_1", err.Error() == "Err_TV_AMOUNT_EXCEEDED_FOR_TIER_1":
		ctx.JSON(http.StatusUnprocessableEntity, basemodels.NewError(err.Error()))
	case errors.Is(err, tx.ErrInsufficientBalance):
		ctx.JSON(http.StatusUnprocessableEntity, basemodels.NewError(err.Error()))
	case errors.Is(err, tx.ErrTransactionPending):
		ctx.JSON(http.StatusConflict, basemodels.NewError(err.Error()))
	case errors.Is(err, tx.ErrTransactionCompleted):
		ctx.JSON(http.StatusConflict, basemodels.NewError(err.Error()))
	case errors.Is(err, tx.ErrInvalidVariation):
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
	default:
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
	}
}

// validateBillRequest is a shared pre-flight that binds JSON, checks pin,
// authenticates the caller, fetches userInfo, and verifies the transaction PIN.
// Returns (userInfo, ok). If ok is false the response is already written.
func (b *Bills) validateBillRequest(ctx *gin.Context, pin *string) (db.User, bool) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return db.User{}, false
	}

	userInfo, err := b.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError(apistrings.UserNotFound))
		return db.User{}, false
	}

	// Account status checks
	if !userInfo.IsActive {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.DeactivatedAccount))
		return db.User{}, false
	}

	if !userInfo.Verified {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UserNotVerified))
		return db.User{}, false
	}

	if err = utils.VerifyHashValue(*pin, userInfo.HashedPin.String); err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return db.User{}, false
	}

	return userInfo, true
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

// getServiceVariations fetches service variations from Redis cache or remote API
// It first checks if the variations are already cached in Redis
// If not, it fetches them from the remote API and stores them in Redis
// It then returns the variations to the client
// The cache is set to expire in 10 minutes
// If the cache is empty, it will be set to expire in 10 seconds
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

func (b *Bills) buyAirtime(ctx *gin.Context) {
	var request tx.BuyAirtimeRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	userInfo, ok := b.validateBillRequest(ctx, &request.Pin)
	if !ok {
		return
	}

	b.server.logger.Infof("buy airtime: %+v", request)

	response, err := b.transactionService.HandleAirtime(ctx, &userInfo, request)
	if err != nil {
		mapBillError(ctx, err)
		return
	}

	switch response.Status {
	case "pending":
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "Airtime is processing", response))
	case "failed":
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("airtime purchase failed"))
	default:
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("Airtime purchase successful", response))
	}
}

func (b *Bills) buyData(ctx *gin.Context) {
	var request tx.BuyDataRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	userInfo, ok := b.validateBillRequest(ctx, &request.Pin)
	if !ok {
		return
	}

	response, err := b.transactionService.HandleData(ctx, &userInfo, request)
	if err != nil {
		mapBillError(ctx, err)
		return
	}

	switch response.Status {
	case "pending":
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "Data is processing", response)) // FIX [H2]
	case "failed":
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("data purchase failed")) // FIX [H3]
	default:
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("Data purchase successful", response))
	}
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
	var request tx.TVSubRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	userInfo, ok := b.validateBillRequest(ctx, &request.Pin)
	if !ok {
		return
	}

	response, err := b.transactionService.HandleTvSubscription(ctx, &userInfo, request)
	if err != nil {
		b.server.logger.Error(err)
		mapBillError(ctx, err) // FIX [H1]
		return
	}

	switch response.Status {
	case "pending":
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "TV subscription is processing", response)) // FIX [H4]
	case "failed":
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("TV subscription failed")) // FIX [H5]
	default:
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("TV subscription successful", response))
	}
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
	var request tx.ElectricityRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

	userInfo, ok := b.validateBillRequest(ctx, &request.Pin)
	if !ok {
		return
	}

	response, err := b.transactionService.HandleBuyElectricity(ctx, &userInfo, request)
	if err != nil {
		mapBillError(ctx, err) // FIX [H1]
		return
	}

	// FIX [H7]: Original always returned 200 regardless of provider status.
	// Electricity can return pending (token not yet generated) or failed.
	switch response.Status {
	case "pending":
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "Electricity purchase is processing", response))
	case "failed":
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("electricity purchase failed"))
	default:
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("Electricity purchase successful", response))
	}
}
