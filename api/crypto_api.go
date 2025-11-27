package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	rapidramp "github.com/SwiftFiat/SwiftFiat-Backend/services/rapid_ramp"
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
	qrcode             *rapidramp.QRCodeService
}

func (c CryptoAPI) router(server *Server) {
	c.server = server
	c.walletService = c.server.walletService
	c.userService = c.server.userService
	c.transactionService = c.server.transactionService
	c.notifyr = c.server.inAppnotificationService
	c.qrcode = c.server.qrcodeService

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/crypto")
	serverGroupV1.POST("/create-wallet", c.server.authMiddleware.AuthenticatedMiddleware(), c.createStaticWallet)
	serverGroupV1.POST("/qr-code", c.server.authMiddleware.AuthenticatedMiddleware(), c.GenerateQRCode)
	serverGroupV1.GET("services", c.fetchServices)
	serverGroupV1.POST("/cryptomus/webhook", c.HandleCryptomusWebhook)
	serverGroupV1.GET("/test", c.testCryptoAPI)
	serverGroupV1.GET("/coin-data", c.GetCoinData)
	serverGroupV1.GET("/coin-price-data", c.GetCoinPriceHistory)
	serverGroupV1.Static("/assets", "./assets")
	serverGroupV1.GET("/images", c.server.authMiddleware.AuthenticatedMiddleware(), c.GetImages)
	serverGroupV1.POST("/test-webhook", c.TestCryptomusWebhookEndpoint)
	serverGroupV1.POST("/resend-webhook", c.ResendWebhook)
	serverGroupV1.POST("/test-crypto-api", c.testCryptoAPI)
	serverGroupV1.POST("/payment-info", c.GetPaymentInfo)
	serverGroupV1.POST("/create-stablecoin-wallet", c.server.authMiddleware.AuthenticatedMiddleware(), c.createStablecoinFundingAddress)
}

// var (
// 	callback = fmt.Sprintf("%s/%s", c.server.config.SwiftBaseUrl, "webhook")
// )

// testCryptoAPI godoc
// @Summary      Test Crypto API Endpoint
// @Description  Tests if the Crypto API is active and reachable
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse
// @Router       /api/v1/crypto/test-crypto-api [post]
func (c *CryptoAPI) testCryptoAPI(ctx *gin.Context) {
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Authentication API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

// GetCoinData godoc
// @Summary      Get Coin Data
// @Description  Retrieves detailed information about a specific cryptocurrency coin using the CoinRanking provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        coin  query     string  true  "Coin Symbol (e.g., BTC, ETH)"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/coin-data [get]
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

type CreateStaticWalletRequest struct {
	Currency string `json:"currency" binding:"required"`
	Network  string `json:"network" binding:"required"`
}

// createStaticWallet godoc
// @Summary      Create Static Wallet
// @Description  Creates a static cryptocurrency wallet for the authenticated user using the Cryptomus provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        body  body      CreateStaticWalletRequest  true  "Wallet Creation Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      401   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/create-wallet [post]
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

	// Generate Order ID
	orderID := fmt.Sprintf("wallet_%d_%s_%s_%d", activeUser.UserID, request.Network, request.Currency, time.Now().Unix())

	c.server.logger.Info(fmt.Sprintf("Order ID: %s", orderID))

	callbackURL := fmt.Sprintf("%s/%s", c.server.config.SwiftBaseUrl, "crypto/cryptomus/webhook")

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

// fetchServices godoc
// @Summary      Fetch Crypto Services
// @Description  Retrieves a list of available cryptocurrency services from the Cryptomus provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/services [get]
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

type QRCodeRequest struct {
	WalletUUID uuid.UUID `json:"wallet_uuid"`
}

// GenerateQRCode godoc
// @Summary      Generate QR Code
// @Description  Generates a QR code for a specified wallet UUID using the Cryptomus provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        body  body      QRCodeRequest  true  "QR Code Generation Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/qr-code [post]
func (c *CryptoAPI) GenerateQRCode(ctx *gin.Context) {
	var req QRCodeRequest

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
	res, err := cryptoProvider.ParseWebhook(rawBody, true)
	if err != nil {
		c.server.logger.Error("process webhook error", err)
		ctx.JSON(400, basemodels.NewError("error parsing webhook"))
		return
	}

	c.server.logger.Debugf("payload: %v", payload)
	// If webhook from rapidramp qrcode, process differently
	if strings.HasPrefix(payload.OrderID, "qr_") {
		c.server.logger.Info("processed as rapid ramp...")
		err := c.qrcode.ProcessCryptomusWebhook(ctx, &payload)
		if err != nil {
			c.server.logger.Errorf("ProcessCryptomusWebhook for qrcode error: %v", err)
		}
		return
	}

	if res.Status == "paid" {
		// Handle USDT and USDC wallet funding
		// Handle regular crypto inflow
		amountDec, err := decimal.NewFromString(payload.MerchantAmount)
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
			AmountInSatoshis:   amountDec,
			Coin:               strings.ToLower(payload.Currency), // Convert to lowercase so coingecko dont break
			Description:        "Crypto Inflow",
			Type:               transaction.Deposit,
			ReceivedAmount:     amountDec,
			TransactionID:      txid,
		}

		_, err = c.transactionService.CreateAllCryptoINflowTXs(ctx, payload.OrderID, cryptoTransaction, c.server.provider)
		if err != nil {
			c.server.logger.Error(fmt.Sprintf("transaction error occurred: %v", err))
		}
	}

	ctx.JSON(http.StatusOK, gin.H{
		"data":     res,
		"order_id": payload.OrderID,
		"status":   payload.Status,
	})
}

// GetCoinPriceHistory godoc
// @Summary      Get Coin Price History
// @Description  Retrieves historical price data for a specific cryptocurrency coin over a defined time period using the CoinRanking provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        coin        query     string  true   "Coin Symbol (e.g., BTC, ETH)"
// @Param        timePeriod  query     string  false  "Time Period (e.g., 24h, 7d, 1m, 1y)"  default(24h)
// @Success      200         {object}  basemodels.SuccessResponse
// @Failure      400         {object}  basemodels.ErrorResponse
// @Failure      500         {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/coin-price-data [get]
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

// GetImages godoc
// @Summary      Get Images
// @Description  Retrieves a list of image filenames from the assets directory.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/images [get]
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

// TestCryptomusWebhookEndpoint godoc
// @Summary      Test Cryptomus Webhook Endpoint
// @Description  Triggers a test webhook to verify the Cryptomus webhook endpoint functionality.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        body  body      cryptocurrency.TestWebhookRequest  true  "Test Webhook Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/test-webhook [post]
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

// ResendWebhook godoc
// @Summary      Resend Cryptomus Webhook
// @Description  Resends a previously sent webhook notification using the Cryptomus provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        body  body      cryptocurrency.ResendWebhookRequest  true  "Resend Webhook Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/resend-webhook [post]
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

// GetPaymentInfo godoc
// @Summary      Get Payment Info
// @Description  Retrieves payment information for a specific order using the Cryptomus provider.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        body  body      cryptocurrency.PaymentInfoRequest  true  "Payment Info Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/payment-info [post]
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

// createStablecoinFundingAddress godoc
// @Summary      Create Stablecoin Funding Address
// @Description  Creates a stablecoin funding address for the authenticated user using the Cryptomus provider.
// @Description  Supported stablecoins are USDT and USDC.
// @Tags         Crypto
// @Accept       json
// @Produce      json
// @Param        body  body      CreateStaticWalletRequest  true  "Stablecoin Funding Address Request"
// @Success      200   {object}  basemodels.SuccessResponse
// @Failure      400   {object}  basemodels.ErrorResponse
// @Failure      401   {object}  basemodels.ErrorResponse
// @Failure      500   {object}  basemodels.ErrorResponse
// @Router       /api/v1/crypto/create-stablecoin-wallet [post]
func (c *CryptoAPI) createStablecoinFundingAddress(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var request CreateStaticWalletRequest
	err = ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter currency and network"))
		return
	}

	// Validate currency (only USDT/USDC allowed)
	if request.Currency != "USDT" && request.Currency != "USDC" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("only USDT and USDC are supported"))
		return
	}

	// Get Cryptomus provider
	provider, exists := c.server.provider.GetProvider(providers.Cryptomus)
	if !exists {
		c.server.logger.Error("failed to get provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider"))
		return
	}

	cryptoProvider, ok := provider.(*cryptocurrency.CryptomusProvider)
	if !ok {
		c.server.logger.Error("failed to parse crypto provider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed"))
		return
	}

	// Generate Order ID
	dbUser, err := c.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		c.server.logger.Error("failed to get user by ID", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to get user"))
		return
	}
	orderID := utils.GenerateOrderID(dbUser.FirstName.String, dbUser.LastName.String)
	callbackURL := fmt.Sprintf("%s/%s", c.server.config.SwiftBaseUrl, "webhook")
	// callbackURL := "https://bb132a871d4b.ngrok-free.app/api/v1/crypto/webhook"

	c.server.logger.Infof("orderid is %s \n and callbackurl is %s \n", orderID, callbackURL)

	// Create static wallet request
	walletRequest := &cryptocurrency.StaticWalletRequest{
		Currency:    request.Currency,
		Network:     request.Network,
		OrderId:     orderID,
		UrlCallback: callbackURL,
	}

	staticWallet, err := cryptoProvider.CreateStaticWallet(walletRequest)
	if err != nil {
		c.server.logger.Error("failed to create static wallet", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to create wallet"))
		return
	}

	// Store the address in database
	err = c.userService.AssignCryptomusAddressToUser(ctx,
		staticWallet.WalletUUID,
		staticWallet.UUID,
		orderID,
		staticWallet.Address,
		activeUser.UserID,
		staticWallet.Currency,
		staticWallet.Network,
		staticWallet.Url,
		callbackURL)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to assign wallet"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Funding Address Created", gin.H{
		"address":     staticWallet.Address,
		"currency":    staticWallet.Currency,
		"network":     staticWallet.Network,
		"order_id":    orderID,
		"payment_url": staticWallet.Url,
	}))
}
