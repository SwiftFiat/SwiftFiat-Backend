package api

import (
	"context"
	"database/sql"
	"fmt"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards/reloadly_models"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
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
		server.config,
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
	serverGroupV1.GET("all", g.server.authMiddleware.AuthenticatedMiddleware(), g.getAllGiftCards)
	serverGroupV1.GET("brands", g.server.authMiddleware.AuthenticatedMiddleware(), g.getAllGiftCardBrands)
	serverGroupV1.GET("categories", g.server.authMiddleware.AuthenticatedMiddleware(), g.getAllGiftCardCategories)
	serverGroupV1.POST("purchase", g.server.authMiddleware.AuthenticatedMiddleware(), g.purchaseGiftCard)
	serverGroupV1.GET("card/:transactionID", g.server.authMiddleware.AuthenticatedMiddleware(), g.getCardInfo)
	serverGroupV1.GET("brands/:brandID", g.server.authMiddleware.AuthenticatedMiddleware(), g.getGiftCardBrandNames)
	serverGroupV1.GET("cards/:brandID/:countryID", g.server.authMiddleware.AuthenticatedMiddleware(), g.getGiftCardByCountryIDAndBrandID)
	serverGroupV1.GET("/buy", g.BuyRGiftCard)
	serverGroupV1.POST("/refactor-buy", g.server.authMiddleware.AuthenticatedMiddleware(), g.RefactorBuyRGiftCard)

	serverGroupV1Admin := server.router.Group("/api/admin/v1/giftcard")
	serverGroupV1Admin.POST("sync", g.server.authMiddleware.AuthenticatedMiddleware(), g.syncGiftCards)

	server.taskScheduler.AddTask("sync_giftcards", "sync_giftcards", func(ctx context.Context) error {
		err := g.service.SyncGiftCards(g.server.provider)
		if err != nil {
			g.server.logger.Error(fmt.Sprintf("failed to sync gift cards: %v", err))
			return err
		}
		return nil
	}, 24*time.Hour)
	server.taskScheduler.ScheduleTask("sync_giftcards", 1*time.Minute)
}

func (g *GiftCard) getAllGiftCards(ctx *gin.Context) {
	// Fetch Query Params and parse
	cursor := ctx.Query("cursor")
	if cursor == "" {
		g.server.logger.Info("no cursor passed to fetch giftcards")
	}

	// Fetch user details
	_, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
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
	_, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	giftcards, err := g.server.queries.FetchGiftCardsByBrand(ctx)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcard brands fetched successfully", models.ToGiftCardBrandResponse(giftcards)))
}

func (g *GiftCard) getAllGiftCardCategories(ctx *gin.Context) {

	// Fetch user details
	_, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	categories, err := g.server.queries.FetchGiftCardsByCategory(ctx)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcard categories fetched successfully", categories))
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

	// /// check varification status
	// if !activeUser.Verified {
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotVerified))
	// 	return
	// }

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	response, err := g.service.BuyGiftCard(g.server.provider, g.transactionService, activeUser.UserID, request.ProductID, walletID, request.Quantity, request.UnitPrice)
	if err != nil {
		g.server.logger.Error("failed to buy gift card", "error", err)
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

	g.server.logger.Info("gift card purchased", "response", response)
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("gift card purchased", response))
}

func (g *GiftCard) getCardInfo(ctx *gin.Context) {
	transactionID, ok := ctx.Params.Get("transactionID")
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please pass transactionID as a path param: /giftCardInfo/:transactionID"))
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
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotVerified))
		return
	}

	response, err := g.service.GetCardInfo(g.server.provider, transactionID)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("error fetching giftcard: %v", err)))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("gift retrieved", response))
}

func (g *GiftCard) getGiftCardBrandNames(ctx *gin.Context) {
	brandID, ok := ctx.Params.Get("brandID")
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please pass brandID as a path param: /giftCardBrandNames/:brandID"))
		return
	}

	brandIDint, err := strconv.Atoi(brandID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse brandID"))
		return
	}

	brandNames, err := g.server.queries.SelectCountriesByBrandID(ctx, sql.NullInt64{
		Int64: int64(brandIDint),
		Valid: true,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcard brand names fetched successfully", models.ToGiftCardBrandNamesResponse(brandNames)))
}

func (g *GiftCard) getGiftCardByCountryIDAndBrandID(ctx *gin.Context) {
	countryID, ok := ctx.Params.Get("countryID")
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please pass countryID as a path param: /cards/:brandID/:countryID"))
		return
	}

	brandID, ok := ctx.Params.Get("brandID")
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please pass brandID as a path param: /cards/:brandID"))
		return
	}

	countryIDint, err := strconv.Atoi(countryID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse countryID"))
		return
	}
	brandIDint, err := strconv.Atoi(brandID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse brandID"))
		return
	}

	response, err := g.server.queries.SelectGiftCardsByCountryIDAndBrandID(ctx, db.SelectGiftCardsByCountryIDAndBrandIDParams{
		CountryID: sql.NullInt64{
			Int64: int64(countryIDint),
			Valid: true,
		},
		BrandID: sql.NullInt64{
			Int64: int64(brandIDint),
			Valid: true,
		},
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcard by country ID fetched successfully", models.ToGiftCardSelectGiftCardsByCountryIDAndBrandIDResponse(response)))
}

func (g *GiftCard) BuyRGiftCard(c *gin.Context) {
	token, err := g.service.GetReloadlyToken(g.server.provider)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
	}

	phoneDetails := reloadlymodels.RecipientPhoneDetails{
		CountryCode: "US",
		PhoneNumber: "8579184613",
	}

	card, err := g.service.BuyRGPGiftCard(g.server.provider, token, reloadlymodels.GiftCardPurchaseRequest{
		ProductID:             5,
		CountryCode:           "US",
		Quantity:              1,
		UnitPrice:             5,
		CustomIdentifier:      "gift-card-amazon-order_20",
		SenderName:            "John Doe",
		RecipientEmail:        "anyone@email.com",
		RecipientPhoneDetails: phoneDetails,
	})

	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"card": card})
}

func (g *GiftCard) RefactorBuyRGiftCard(c *gin.Context) {
	request := struct {
		ProductID int64  `json:"product_id" binding:"required"`
		WalletID  string `json:"wallet_id" binding:"required"`
		Quantity  int    `json:"quantity" binding:"required"`
		UnitPrice int    `json:"unit_price" binding:"required"`
	}{}

	err := c.ShouldBindJSON(&request)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("please check request body: %v", err)))
		return
	}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	walletID, err := uuid.Parse(request.WalletID)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("cannot parse source wallet ID"))
		return
	}

	response, err := g.service.Buy(c, g.server.provider, g.transactionService, activeUser.UserID, request.ProductID, walletID, request.Quantity, request.UnitPrice)
	if err != nil {
		g.server.logger.Error("failed to buy gift card", "error", err)
		if walletErr, ok := err.(*wallet.WalletError); ok {
			if walletErr.Error() == wallet.ErrWalletNotFound.Error() {
				c.JSON(http.StatusBadRequest, basemodels.NewError("wallet not found"))
				return
			}
			if walletErr.Error() == wallet.ErrNotYours.Error() {
				c.JSON(http.StatusBadRequest, basemodels.NewError("wallet not found"))
				return
			}
			if walletErr.Error() == wallet.ErrInsufficientFunds.Error() {
				c.JSON(http.StatusBadRequest, basemodels.NewError("insufficient funds"))
				return
			}
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	g.server.logger.Info("gift card purchased", "response", response)
	c.JSON(http.StatusOK, basemodels.NewSuccess("gift card purchased", response))
}
