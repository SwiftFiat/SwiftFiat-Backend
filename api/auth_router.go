package api

import (
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type Auth struct {
	server *Server
}

func (a Auth) router(server *Server) {
	a.server = server

	serverGroup := server.router.Group("/auth")
	serverGroup.GET("test", a.testAuth)
	serverGroup.POST("login", a.login)
	serverGroup.POST("register", a.register)
	serverGroup.POST("register-admin", a.registerAdmin)
	serverGroup.POST("verify-otp", AuthenticatedMiddleware(), a.verifyOTP)
	serverGroup.GET("otp", AuthenticatedMiddleware(), a.sendOTP)
}

func (a Auth) testAuth(ctx *gin.Context) {
	dr := models.SuccessResponse{
		Status:  "success",
		Message: "Authentication API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}
