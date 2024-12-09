package api

import (
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/giftcard"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type GiftCard struct {
	server  *Server
	service *giftcard.GiftcardService
}

func (g GiftCard) router(server *Server) {
	g.server = server
	g.service = giftcard.NewGiftcardServiceWithCache(
		server.queries,
		server.logger,
		server.redis,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/giftcard")
	serverGroupV1.POST("sync", AuthenticatedMiddleware(), g.syncGiftCards)
	serverGroupV1.GET("all", AuthenticatedMiddleware(), g.getAllGiftCards)
	serverGroupV1.GET("brands", AuthenticatedMiddleware(), g.getAllGiftCardBrands)
	serverGroupV1.POST("purchase", AuthenticatedMiddleware(), g.purchaseGiftCard)
}

// func (g *GiftCard) fetchAndStoreCards(context.Context) {
// 	if provider, exists := g.server.provider.GetProvider(provider.Reloadly); exists {
// 		reloadlyProvider, ok := provider.(*giftcards.ReloadlyProvider)
// 		if ok {
// 			params := reloadlymodels.ProductQueryParams{
// 				Size:         size,
// 				Page:         page,
// 				IncludeRange: includeRange,
// 				IncludeFixed: includeFixed,
// 			}
// 			giftCards, err := reloadlyProvider.GetAllGiftCards(params)
// 			if err != nil {
// 				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to GiftCard Provider Error: %s", err)))
// 				return
// 			}
// 			/// Log GiftCard DATA
// 			g.server.logger.Log(logrus.InfoLevel, "GiftCardData: ", giftCards)
// 		}
// 	}
// }

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

	giftcards, err := g.server.queries.FetchGiftCards(ctx)
	if err != nil {
		g.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("giftcards fetched successfully", giftcards))
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

	ctx.JSON(http.StatusUnauthorized, basemodels.NewSuccess("gift card purchased", activeUser))
}
