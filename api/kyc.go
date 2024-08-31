package api

import (
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider/kyc"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type KYC struct {
	server *Server
}

func (k KYC) router(server *Server) {
	k.server = server

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/kyc")
	serverGroupV1.POST("validate-bvn", AuthenticatedMiddleware(), k.validateBVN)
	serverGroupV1.POST("validate-nin", AuthenticatedMiddleware(), k.validateNIN)
}

func (k *KYC) validateBVN(ctx *gin.Context) {
	request := struct {
		BVN       string `json:"bvn" binding:"required"`
		FirstName string `json:"first_name" binding:"required"`
		LastName  string `json:"last_name" binding:"required"`
		DOB       string `json:"dob" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		k.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if provider, exists := k.server.provider.GetProvider(provider.Dojah); exists {
		kycProvider, ok := provider.(*kyc.DOJAHProvider)
		if ok {
			verified, err := kycProvider.ValidateBVN(request.BVN, request.FirstName, request.LastName, request.DOB)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
				return
			}
			// Use the verification result
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("BVN Success", verified))
			return
		}
	}

	ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
}

func (k *KYC) validateNIN(ctx *gin.Context) {
	request := struct {
		NIN       string `json:"nin" binding:"required"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Selfie    string `json:"selfie_image" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		k.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if provider, exists := k.server.provider.GetProvider(provider.Dojah); exists {
		kycProvider, ok := provider.(*kyc.DOJAHProvider)
		if ok {
			verified, err := kycProvider.ValidateNIN(request)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
				return
			}
			// Use the verification result
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("NIN Success", verified))
			return
		}
	}

	ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
}
