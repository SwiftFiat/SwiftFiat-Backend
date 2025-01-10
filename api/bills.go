package api

import (
	"net/http"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bills"
	"github.com/gin-gonic/gin"
)

type Bills struct {
	server *Server
}

// TODO: This route will be wrapped with an administrative middleware
func (b Bills) router(server *Server) {
	b.server = server

	serverGroupV1 := server.router.Group("/api/v1/bills")
	serverGroupV1.GET("categories", AuthenticatedMiddleware(), b.getCategories)
	serverGroupV1.GET("services", AuthenticatedMiddleware(), b.getServices)
	serverGroupV1.GET("service-variation", AuthenticatedMiddleware(), b.getServiceVariations)
}

func (b *Bills) getCategories(ctx *gin.Context) {
	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	categories, err := billProv.GetServiceCategories()
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched bill categories", categories))
}

func (b *Bills) getServices(ctx *gin.Context) {
	identifier := ctx.Query("identifier")

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	services, err := billProv.GetServiceIdentifiers(identifier)
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched bill services", services))
}

func (b *Bills) getServiceVariations(ctx *gin.Context) {
	serviceID := ctx.Query("serviceID")

	provider, exists := b.server.provider.GetProvider(providers.VTPass)
	if !exists {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("can not find provider Bill Provider"))
		return
	}

	billProv, ok := provider.(*bills.VTPassProvider)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse provider of type - Bill Provider"))
		return
	}

	variations, err := billProv.GetServiceVariation(serviceID)
	if err != nil {
		ctx.JSON(http.StatusNotImplemented, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched service variations", variations))
}
