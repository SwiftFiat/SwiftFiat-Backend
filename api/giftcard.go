package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider/giftcards"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/service/provider/giftcards/reloadly_models"
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
	// Fetch Query Params and parse
	params := ctx.Request.URL.Query()
	size, err := strconv.Atoi(params.Get("size"))
	if err != nil {
		size = 10
	}
	page, err := strconv.Atoi(params.Get("page"))
	if err != nil {
		page = 0
	}
	includeRange, err := strconv.ParseBool(params.Get("includeRange"))
	if err != nil {
		includeRange = false
	}
	includeFixed, err := strconv.ParseBool(params.Get("includeFixed"))
	if err != nil {
		includeFixed = false
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

	if provider, exists := g.server.provider.GetProvider(provider.Reloadly); exists {
		reloadlyProvider, ok := provider.(*giftcards.ReloadlyProvider)
		if ok {
			params := reloadlymodels.ProductQueryParams{
				Size:         size,
				Page:         page,
				IncludeRange: includeRange,
				IncludeFixed: includeFixed,
			}
			giftCards, err := reloadlyProvider.GetAllGiftCards(params)
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to GiftCard Provider Error: %s", err)))
				return
			}
			/// Log GiftCard DATA
			g.server.logger.Log(logrus.InfoLevel, "GiftCardDara: ", giftCards)
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("gift cards fetched", giftCards))
		}
	}
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
