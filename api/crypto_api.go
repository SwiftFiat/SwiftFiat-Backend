package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
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
	serverGroupV1.POST("wallet", AuthenticatedMiddleware(), c.createWallet)
	serverGroupV1.GET("wallets", AuthenticatedMiddleware(), c.fetchWallets)
	serverGroupV1.POST("address/generate", AuthenticatedMiddleware(), c.generateWalletAddress)
	serverGroupV1.POST("/webhook", c.HandleWebhook)
}

func (c *CryptoAPI) createWallet(ctx *gin.Context) {
	request := struct {
		Coin string `json:"coin" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter coin"))
		return
	}

	// Get Active User
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	if provider, exists := c.server.provider.GetProvider(provider.Bitgo); exists {
		cryptoProvider, ok := provider.(*cryptocurrency.BitgoProvider)
		if ok {
			walletData, err := cryptoProvider.CreateWallet(cryptocurrency.SupportedCoin(request.Coin))
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
				return
			}
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("Wallet Created", walletData))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}
	ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
}

func (c *CryptoAPI) fetchWallets(ctx *gin.Context) {
	// Get Active User
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	if provider, exists := c.server.provider.GetProvider(provider.Bitgo); exists {
		cryptoProvider, ok := provider.(*cryptocurrency.BitgoProvider)
		if ok {
			walletData, err := cryptoProvider.FetchWallets()
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
				return
			}
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("Wallets Fetched", walletData))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("parsing crypto provider failed, please register Provider"))
		return
	}

	ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to FIND Provider, please register Provider"))
}

func (c *CryptoAPI) generateWalletAddress(ctx *gin.Context) {

	request := struct {
		WalletID string `json:"walletID" binding:"required"`
		Coin     string `json:"coin" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter coin and walletID"))
		return
	}

	// Get Active User
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	var walletData *cryptocurrency.WalletAddress

	/// Generate a new address
	if provider, exists := c.server.provider.GetProvider(provider.Bitgo); exists {
		cryptoProvider, ok := provider.(*cryptocurrency.BitgoProvider)
		if ok {
			walletData, err = cryptoProvider.CreateWalletAddress(request.WalletID, cryptocurrency.SupportedCoin(request.Coin))
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
				return
			}
		}
	}

	/// Assign address to user
	err = c.userService.AssignWalletAddressToUser(ctx, walletData.Address, activeUser.UserID, walletData.Coin)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to assign wallet to User Error: %s", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("New Wallet Address Generated", models.MapWalletAddressToAddressUserResponse(walletData, models.ID(activeUser.UserID), "0", "active", time.Now(), time.Now())))
}

func (c *CryptoAPI) HandleWebhook(ctx *gin.Context) {
	var payload cryptocurrency.WebhookTransferPayload

	c.server.logger.Info(ctx.Request.Body)

	if err := ctx.ShouldBindJSON(&payload); err != nil {
		c.server.logger.Error("failed to decode webhook payload", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}
	// Process the webhook event
	if *payload.State == "confirmed" {

		// Call the wallet service and credit the user basse
		c.server.logger.Info(fmt.Sprintf("Payload BaseValue %v", *payload.BaseValue))
		c.server.logger.Info(fmt.Sprintf("Payload Coin %v", *payload.Coin))

		amountInSatoshis, err := decimal.NewFromString(*payload.BaseValueString)
		if err != nil {
			c.server.logger.Error("could not parse payloadBaseValue to decimal", err)
			return
		}

		cryptoTransaction := transaction.CryptoTransaction{
			SourceHash:         *payload.Hash,
			DestinationAddress: *payload.Receiver,
			AmountInSatoshis:   amountInSatoshis,
			Coin:               *payload.Coin,
			Description:        "Cypto Inflow",
			Type:               transaction.Deposit,
		}

		_, err = c.transactionService.CreateCryptoInflowTransaction(ctx, cryptoTransaction, c.server.provider)
		if err != nil {
			c.server.logger.Error(fmt.Sprintf("transaction error occurred: %v", err))
		}
	}

	c.server.logger.Info(fmt.Sprintf("transaction %v successful", *payload.Hash))
	ctx.JSON(http.StatusOK, gin.H{"status": "success"})
}
