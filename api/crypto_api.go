package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
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
}

func (c CryptoAPI) router(server *Server) {
	c.server = server
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
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/crypto")
	/// Should be managed from the administrative view
	serverGroupV1.POST("/create-wallet", c.server.authMiddleware.AuthenticatedMiddleware(), c.createStaticWallet)
	serverGroupV1.POST("/qr-code", c.server.authMiddleware.AuthenticatedMiddleware(), c.GenerateQRCode)
	serverGroupV1.GET("services", c.server.authMiddleware.AuthenticatedMiddleware(), c.fetchServices)
	serverGroupV1.POST("/webhook", c.HandleCryptomusWebhook)
	serverGroupV1.GET("/test", c.testCryptoAPI)
	serverGroupV1.GET("/coin-data", c.server.authMiddleware.AuthenticatedMiddleware(), c.GetCoinData)
	serverGroupV1.GET("/coin-price-data", c.server.authMiddleware.AuthenticatedMiddleware(), c.GetCoinPriceHistory)
	serverGroupV1.Static("/assets", "./assets")
	serverGroupV1.GET("/images", c.server.authMiddleware.AuthenticatedMiddleware(), c.GetImages)
	serverGroupV1.GET("/test-webhook", c.TestCryptomusWebhookEndpoint)
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
	request := struct {
		Currency string `json:"currency" binding:"required"`
		Network  string `json:"network" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter currency and network"))
		return
	}

	// Get Active User
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Get User from DB
	dbUser, err := c.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		c.server.logger.Error("failed to get user", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
		return
	}

	/// check varification status
	// if !dbUser.Verified {
	// 	c.server.logger.Error("user not verified")
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
	// 	return
	// }

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

	orderID, err := models.EncryptID(models.ID(dbUser.ID))
	if err != nil {
		c.server.logger.Error("failed to encrypt user id", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
		return
	}

	c.server.logger.Info(fmt.Sprintf("Order ID: %s", orderID))

	userAddress, err := c.userService.GetUserCryptomusAddress(ctx, activeUser.UserID, request.Currency, request.Network)
	if err == nil {
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Already Has an Address", models.MapDBCryptomusAddressToCryptomusAddressResponse(userAddress)))
		return
	}

	callbackURL := models.GetCryptoCallbackURL(c.server.config, orderID)

	walletRequest := &cryptocurrency.StaticWalletRequest{
		Currency:    request.Currency,
		Network:     request.Network,
		OrderId:     orderID,
		UrlCallback: "https://swiftfiat-backend.onrender.com/api/v1/crypto/webhook",
	}
	c.server.logger.Info(fmt.Sprintf("Creating static wallet with request: %+v", walletRequest))

	staticWallet, err := cryptoProvider.CreateStaticWallet(walletRequest)
	if err != nil {
		c.server.logger.Error("failed to create static wallet", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
		return
	}

	/// Assign address to user
	err = c.userService.AssignCryptomusAddressToUser(ctx, staticWallet.UUID, staticWallet.UUID, staticWallet.Address, activeUser.UserID, staticWallet.Currency, staticWallet.Network, staticWallet.Url, callbackURL)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to assign wallet to User Error: %s", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Static Wallet Created", gin.H{
		"wallet_uuid":  staticWallet.UUID,
		"address":      staticWallet.Address,
		"network":      staticWallet.Network,
		"currency":     staticWallet.Currency,
		"url":          staticWallet.Url,
		"callback_url": callbackURL,
		"uuid":         staticWallet.UUID,
		"order_id":     orderID,
		"status":       "success",
		"message":      "Static wallet created successfully",
	}))
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

	// Verify signature
	if err := cryptoProvider.VerifyWebhook(&payload, rawBody); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Process webhook
	message, err := cryptoProvider.ProcessWebhook(&payload)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if message == "paid" {
		// Call the wallet service and credit the user basse
		c.server.logger.Info(fmt.Sprintf("sent amount %v", payload.PaymentAmount))
		c.server.logger.Info(fmt.Sprintf("received amount %v", payload.MerchantAmount))
		c.server.logger.Info(fmt.Sprintf("coin currency %v", payload.Currency))
		c.server.logger.Info(fmt.Sprintf("coin currency %v", payload.Currency))
		c.server.logger.Info(fmt.Sprintf("convert to %v", payload.Convert.ToCurrency))
		c.server.logger.Info(fmt.Sprintf("rate %v", payload.Convert.Rate))
		c.server.logger.Info(fmt.Sprintf("convert amount %v", payload.Convert.Amount))

		amountInSatoshis, err := decimal.NewFromString(payload.PaymentAmount)
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

		cryptoTransaction := transaction.CryptoTransaction{
			SourceHash:         payload.Sign,
			DestinationAddress: payload.From,
			AmountInSatoshis:   amountInSatoshis,
			Coin:               payload.Currency,
			Description:        "Crypto Inflow",
			Type:               transaction.Deposit,
			ReceivedAmount:     amounRecieved,
			TransactionID:      uuid.MustParse(payload.UUID),
		}

		_, err = c.transactionService.CreateCryptoInflowTransaction(ctx, cryptoTransaction, c.server.provider)
		if err != nil {
			c.server.logger.Error(fmt.Sprintf("transaction error occurred: %v", err))
		}
	}

	// Respond with success
	ctx.JSON(http.StatusOK, gin.H{
		"message":  message,
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
	err := cryptoProvider.TestCryptomusWebhook(
		uuid.New().String(),                         // uuid
		"test-order-"+time.Now().Format("20060102"), // order_id
		"USDT", // currency
		"TRX",  // network
		"paid", // status
	)

	if err != nil {
		c.server.logger.Error("failed to trigger test webhook", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Cryptomus test webhook triggered",
	})
}
