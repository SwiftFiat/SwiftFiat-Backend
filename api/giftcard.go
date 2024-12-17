package api

import (
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/giftcard"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type GiftCard struct {
	server             *Server
	service            *giftcard.GiftcardService
	transactionService *transaction.TransactionService
}

func (g GiftCard) router(server *Server) {
	g.server = server
	g.service = giftcard.NewGiftcardServiceWithCache(
		server.queries,
		server.logger,
		server.redis,
	)
	g.transactionService = transaction.NewTransactionService(
		server.queries,
		currency.NewCurrencyService(
			server.queries,
			server.logger,
		),
		wallet.NewWalletServiceWithCache(
			server.queries,
			server.logger,
			server.redis,
		),
		server.logger,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/giftcard")
	serverGroupV1.POST("sync", AuthenticatedMiddleware(), g.syncGiftCards)
	serverGroupV1.GET("all", AuthenticatedMiddleware(), g.getAllGiftCards)
	serverGroupV1.GET("brands", AuthenticatedMiddleware(), g.getAllGiftCardBrands)
	serverGroupV1.GET("categories", AuthenticatedMiddleware(), g.getAllGiftCardCategories)
	serverGroupV1.POST("purchase", AuthenticatedMiddleware(), g.purchaseGiftCard)
}

func (g *GiftCard) getAllGiftCards(ctx *gin.Context) {
	// Fetch Query Params and parse
	cursor := ctx.Query("cursor")
	if cursor == "" {
		g.server.logger.Info("no cursor passed to fetch giftcards")
	}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotVerified))
		return
	}

	giftcards, err := g.server.queries.FetchGiftCards(ctx)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcards fetched successfully", models.ToGiftCardResponse(giftcards)))

}

func (g *GiftCard) getAllGiftCardBrands(ctx *gin.Context) {

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotVerified))
		return
	}

	giftcards, err := g.server.queries.FetchGiftCardsByBrand(ctx)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcard brands fetched successfully", models.ToBrandObject(giftcards)))
}

func (g *GiftCard) getAllGiftCardCategories(ctx *gin.Context) {

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotVerified))
		return
	}

	categories, err := g.server.queries.FetchGiftCardsByCategory(ctx)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcard brands fetched successfully", categories))
}

// / Administrative function
func (g *GiftCard) syncGiftCards(ctx *gin.Context) {
	err := g.service.SyncGiftCards(g.server.provider)
	if err != nil {
		g.server.logger.Error(fmt.Sprintf("failed to sync gift cards: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcards synced", nil))
}

func (g *GiftCard) purchaseGiftCard(ctx *gin.Context) {

	request := struct {
		ProductID int64  `json:"product_id" binding:"required"`
		WalletID  string `json:"wallet_id" binding:"required"`
		Quantity  int    `json:"quantity" binding:"required"`
		UnitPrice int    `json:"unit_price" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("please check request body: %v", err)))
		return
	}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotVerified))
		return
	}

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	response, err := g.service.BuyGiftCard(g.server.provider, g.transactionService, activeUser.UserID, request.ProductID, walletID, request.Quantity, request.UnitPrice)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("error processing giftcard purchase: %v", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("gift card purchased", response))
}
