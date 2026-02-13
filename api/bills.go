package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bills"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/rewards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/streaks"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	tx "github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	rewardSvc          *rewards.RewardService
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
	var request transaction.BuyAirtimeRequest

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

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

	b.server.logger.Infof("buy airtime: %v", request)
	response, err := b.transactionService.HandleAirtime(
		ctx,
		&userInfo,
		request,
	)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if response.Status == "pending" {
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "Airtime is processing", response))
		return
	}

	if response.Status == "failed" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("airtime purchase failed"))
		return
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
	var request transaction.BuyDataRequest

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	if request.Pin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("pin is required"))
		return
	}

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

	response, err := b.transactionService.HandleData(ctx, &userInfo, request)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if response.Status == "pending" {
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "Airtime is processing", response))
		return
	}

	if response.Status == "failed" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("airtime purchase failed"))
		return
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
	var request transaction.TVSubRequest

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

	// Create BillTransaction
	response, err := b.transactionService.HandleTvSubscription(ctx, &userInfo, request)
	if err != nil {
		b.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if response.Status == "pending" {
		ctx.JSON(http.StatusAccepted, basemodels.NewCustomResponse("", "Airtime is processing", response))
		return
	}

	if response.Status == "failed" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("airtime purchase failed"))
		return
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
	var request transaction.ElectricityRequest
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

	response, err := b.transactionService.HandleBuyElectricity(ctx, &userInfo, request)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("purchase electricity successful", response))
}
