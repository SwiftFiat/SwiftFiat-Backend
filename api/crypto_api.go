package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CryptoAPI struct {
	server             *Server
	userService        *user_service.UserService
	walletService      *wallet.WalletService
	transactionService *transaction.TransactionService
	notifyr            *service.Notification
}

func (c CryptoAPI) router(server *Server) {
	c.server = server
	c.notifyr = service.NewNotificationService(c.server.queries)
	c.walletService = wallet.NewWalletService(
		c.server.queries,
		c.server.logger,
	)
	c.userService = user_service.NewUserService(
		c.server.queries,
		c.server.logger,
		c.walletService,
	)
	c.transactionService = transaction.NewTransactionService(
		c.server.queries,
		currency.NewCurrencyService(
			c.server.queries,
			c.server.logger,
		),
		c.walletService,
		c.server.logger,
		c.server.config,
		c.notifyr,
	)
	c.notifyr = service.NewNotificationService(
		c.server.queries,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/crypto")
	/// Should be managed from the administrative view
	serverGroupV1.POST("/create-wallet", c.server.authMiddleware.AuthenticatedMiddleware(), c.createStaticWallet)
	serverGroupV1.POST("/qr-code", c.server.authMiddleware.AuthenticatedMiddleware(), c.GenerateQRCode)
	serverGroupV1.GET("services", c.fetchServices)
	serverGroupV1.POST("/webhook", c.HandleCryptomusWebhook)
	serverGroupV1.GET("/test", c.testCryptoAPI)
	serverGroupV1.GET("/coin-data", c.GetCoinData)
	serverGroupV1.GET("/coin-price-data", c.GetCoinPriceHistory)
	serverGroupV1.Static("/assets", "./assets")
	serverGroupV1.GET("/images", c.server.authMiddleware.AuthenticatedMiddleware(), c.GetImages)
	serverGroupV1.POST("/test-webhook", c.TestCryptomusWebhookEndpoint)
	serverGroupV1.POST("/resend-webhook", c.ResendWebhook)
	serverGroupV1.POST("/test-crypto-api", c.testCryptoAPI)
	serverGroupV1.POST("/payment-info", c.GetPaymentInfo)
}

func (c *CryptoAPI) testCryptoAPI(ctx *gin.Context) {
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Authentication API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

func (c *CryptoAPI) GetCoinData(ctx *gin.Context) {
	coin := ctx.Query("coin")
	c.server.logger.Info(fmt.Sprintf("getting info on %s", coin))

	if coin == "" {
		ctx.JSON(400, basemodels.NewError("coin parameter is required"))
		return
	}

	provider, exists := c.server.provider.GetProvider(providers.CoinRanking)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
		return
	}

	dataProvider, ok := provider.(*cryptocurrency.CoinRankingProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}

	coinData, err := dataProvider.GetCoinDetailsBySymbol(coin)
	if err != nil {
		c.server.logger.Errorf("failed to get coinData: %v", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Get coin data is successful", coinData))
}

func (c *CryptoAPI) createStaticWallet(ctx *gin.Context) {
	// Get Active User
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	request := struct {
		Currency string `json:"currency" binding:"required"`
		Network  string `json:"network" binding:"required"`
	}{}

	err = ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter currency and network"))
		return
	}

	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}

	dbUser, err := c.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		c.server.logger.Error("failed to get user by ID", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to get user by ID Error: %s", err)))
		return
	}

	// Generate Order ID
	orderID := utils.GenerateOrderID(
		dbUser.FirstName.String,
		dbUser.LastName.String,
	)

	c.server.logger.Info(fmt.Sprintf("Order ID: %s", orderID))

	callbackURL := "https://8d097378f71e.ngrok-free.app/api/v1/crypto/webhook"

	walletRequest := &cryptocurrency.StaticWalletRequest{
		Currency:    request.Currency,
		Network:     request.Network,
		OrderId:     orderID,
		UrlCallback: callbackURL,
	}
	c.server.logger.Info(fmt.Sprintf("Creating static wallet with request: %+v", walletRequest))

	staticWallet, err := cryptoProvider.CreateStaticWallet(walletRequest)
	if err != nil {
		c.server.logger.Error("failed to create static wallet", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
		return
	}

	/// Assign address to user
	err = c.userService.AssignCryptomusAddressToUser(ctx, staticWallet.WalletUUID, staticWallet.UUID, orderID, staticWallet.Address, activeUser.UserID, staticWallet.Currency, staticWallet.Network, staticWallet.Url, callbackURL)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to assign wallet to User Error: %s", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Static Wallet Created", staticWallet))
}

func (c *CryptoAPI) fetchServices(ctx *gin.Context) {
	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}

	services, err := cryptoProvider.ListServices()
	if err != nil {
		c.server.logger.Error("failed to fetch services", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Services Fetched", gin.H{
		"services": models.ToCryptoServicesResponse(services),
		"count":    len(services),
	}))
}

func (c *CryptoAPI) GenerateQRCode(ctx *gin.Context) {
	var req = struct {
		WalletUUID uuid.UUID `json:"wallet_uuid"`
	}{}

	if err := ctx.ShouldBind(&req); err != nil {
		c.server.logger.Error("failed to bind request", err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("Failed to bind request: %v", err)))
		return
	}
	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}

	code, err := cryptoProvider.GenerateQRCode(req.WalletUUID)
	if err != nil {
		c.server.logger.Error("failed to generate QR Code", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to generate QR Code: %v", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("QR Code Generated", code))
}

func (c *CryptoAPI) HandleCryptomusWebhook(ctx *gin.Context) {
	// Read raw body for signature verification
	rawBody, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		logging.NewLogger().Error("failed to read webhook body", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	// Parse JSON payload
	var payload cryptocurrency.WebhookPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		logging.NewLogger().Error("invalid webhook JSON payload", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}

	// Log incoming webhook
	logging.NewLogger().Info("received webhook",
		"order_id", payload.OrderID,
		"status", payload.Status)

	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}

	// Process webhook
	res, err := cryptoProvider.ParseWebhook(rawBody, false)
	if err != nil {
		c.server.logger.Error("process webhook error", err)
		ctx.JSON(400, basemodels.NewError("error parsing webhook"))
		return
	}
	if res.Status == "confirm-check" {
		c.server.logger.Info("confirm-check status received")
	}

	if res.Status == "check" {
		c.server.logger.Info("Waiting for the transaction to appear on the blockchain")
	}

	if res.Status == "processing" {
		c.server.logger.Info("payment is processing")
	}
	if res.Status == "fail" {
		c.server.logger.Info("payment error")
	}
	if res.Status == "system_fail" {
		c.server.logger.Info("A system error has occurred")
	}

	if res.Status == "wrong_amount" {
		c.server.logger.Info("The client paid less than required")
	}

	if res.Status == "paid" {
		// Call the wallet service and credit the user basse
		c.server.logger.Info(fmt.Sprintf("sent amount %v", payload.PaymentAmount))
		c.server.logger.Info(fmt.Sprintf("received amount %v", payload.MerchantAmount))
		c.server.logger.Info(fmt.Sprintf("coin currency %v", payload.Currency))
		c.server.logger.Info(fmt.Sprintf("coin currency %v", payload.Currency))
		c.server.logger.Info(fmt.Sprintf("convert to %v", payload.Convert.ToCurrency))
		c.server.logger.Info(fmt.Sprintf("rate %v", payload.Convert.Rate))
		c.server.logger.Info(fmt.Sprintf("convert amount %v", payload.Convert.Amount))

		amountInSatoshis, err := decimal.NewFromString(payload.MerchantAmount)
		if err != nil {
			c.server.logger.Errorf("conversion error1: %v", err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}
		amounRecieved, err := decimal.NewFromString(payload.MerchantAmount)
		if err != nil {
			c.server.logger.Errorf("conversion error2: %v", err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}

		txid, err := uuid.Parse(payload.UUID)
		if err != nil {
			c.server.logger.Errorf("UUID conversion error3: %v", err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occured while processing webhook"))
			return
		}

		cryptoTransaction := transaction.CryptoTransaction{
			SourceHash:         payload.Sign,
			DestinationAddress: payload.From,
			AmountInSatoshis:   amountInSatoshis,
			Coin:               strings.ToLower(payload.Currency), // Convert to lowercase so coingecko dont break
			Description:        "Crypto Inflow",
			Type:               transaction.Deposit,
			ReceivedAmount:     amounRecieved,
			TransactionID:      txid,
		}

		_, err = c.transactionService.CreateCryptoInflowTransaction(ctx, payload.OrderID, cryptoTransaction, c.server.provider)
		if err != nil {
			c.server.logger.Error(fmt.Sprintf("transaction error occurred: %v", err))
		}
	}

	// Respond with success
	ctx.JSON(http.StatusOK, gin.H{
		"data":     res,
		"order_id": payload.OrderID,
		"status":   payload.Status,
	})
}

func (c *CryptoAPI) GetCoinPriceHistory(ctx *gin.Context) {
	coin := ctx.Query("coin")
	timePeriod := ctx.DefaultQuery("timePeriod", "24h")
	c.server.logger.Info(fmt.Sprintf("getting price data for %s", coin))

	if coin == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("coin parameter is required"))
		return
	}

	provider, exists := c.server.provider.GetProvider(providers.CoinRanking)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
		return
	}

	dataProvider, ok := provider.(*cryptocurrency.CoinRankingProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}
	historydata, err := dataProvider.GetCoinHistoryData(coin, timePeriod)

	if err != nil {
		c.server.logger.Errorf("failed to get coin price data: %v", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Get coin price data is successful", basemodels.NewSuccess("coin price data", historydata)))
}

func (c *CryptoAPI) GetImages(ctx *gin.Context) {
	files, err := os.ReadDir("./assets")
	if err != nil {
		c.server.logger.Error("failed to read assets directory", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to read assets directory"))
		return
	}

	var images []string

	for _, file := range files {
		if !file.IsDir() {
			images = append(images, file.Name())
		}
	}
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Images fetched successfully", images))
}

func (c *CryptoAPI) TestCryptomusWebhookEndpoint(ctx *gin.Context) {
	var req cryptocurrency.TestWebhookRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request data",
			"details": err.Error(),
		})
		return
	}
	// Get provider instance
	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Cryptomus provider not configured",
		})
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "invalid provider type",
		})
		return
	}

	// Trigger test webhook
	res, err := cryptoProvider.TestCryptomusWebhook(&req)
	if err != nil {
		c.server.logger.Error("Failed to trigger test webhook",
			"error", err,
			"request", req,
		)
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Test webhook triggered",
		"data": gin.H{
			"result": res.Result,
			"state":  res.State,
		},
	})
}

func (c *CryptoAPI) ResendWebhook(ctx *gin.Context) {
	var req cryptocurrency.ResendWebhookRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request data",
			"details": err.Error(),
		})
		return
	}
	// Get provider instance
	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Cryptomus provider not configured",
		})
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "invalid provider type",
		})
		return
	}

	res, err := cryptoProvider.ResendWebhook(&req)
	if err != nil {
		c.server.logger.Error("Failed to resend webhook",
			"error", err,
			"request", req,
		)
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Webhook resent successfully", res))
}

func (c *CryptoAPI) GetPaymentInfo(ctx *gin.Context) {
	var req cryptocurrency.PaymentInfoRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request data",
			"details": err.Error(),
		})
		return
	}

	// Get provider instance
	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Cryptomus provider not configured",
		})
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "invalid provider type",
		})
		return
	}

	res, err := cryptoProvider.GetPaymentInfo(&req)
	if err != nil {
		c.server.logger.Error("Failed to get payment info",
			"error", err,
			"request", req,
		)
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Webhook resent successfully", res))
}
