package api

import (
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider/giftcards"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type GiftCard struct {
	server *Server
}

func (g GiftCard) router(server *Server) {
	g.server = server

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/giftcard")
	serverGroupV1.GET("all", AuthenticatedMiddleware(), g.getAllGiftCards)
	serverGroupV1.POST("purchase", AuthenticatedMiddleware(), g.purchaseGiftCard)
}

func (g *GiftCard) getAllGiftCards(ctx *gin.Context) {
	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if provider, exists := g.server.provider.GetProvider(provider.Reloadly); exists {
		reloadlyProvider, ok := provider.(*giftcards.ReloadlyProvider)
		if ok {
			giftCards, err := reloadlyProvider.GetAllGiftCards()
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to GiftCard Provider Error: %s", err)))
				return
			}
			/// Log Verification DATA
			g.server.logger.Log(logrus.InfoLevel, "GiftCardDara: ", giftCards)
			ctx.JSON(http.StatusUnauthorized, basemodels.NewSuccess("gift cards fetched", giftCards))
		}
	}

	ctx.JSON(http.StatusUnauthorized, basemodels.NewSuccess("gift cards fetched", activeUser))
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
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	ctx.JSON(http.StatusUnauthorized, basemodels.NewSuccess("gift card purchased", activeUser))
}
