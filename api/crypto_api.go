package api

import (
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type CryptoAPI struct {
	server *Server
}

func (c CryptoAPI) router(server *Server) {
	c.server = server

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/crypto")
	/// Should be managed from the administrative view
	serverGroupV1.POST("wallet", AuthenticatedMiddleware(), c.createWallet)
	serverGroupV1.GET("wallets", AuthenticatedMiddleware(), c.fetchWallets)
	serverGroupV1.POST("address/generate", AuthenticatedMiddleware(), c.generateWalletAddress)
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
	}
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
	}
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

	if provider, exists := c.server.provider.GetProvider(provider.Bitgo); exists {
		cryptoProvider, ok := provider.(*cryptocurrency.BitgoProvider)
		if ok {
			walletData, err := cryptoProvider.CreateWalletAddress(request.WalletID, cryptocurrency.SupportedCoin(request.Coin))
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to Crypto Provider Error: %s", err)))
				return
			}
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("Address Generated", walletData))
			return
		}
	}
}
