package api

import (
	"net/http"
	"strings"

	_ "github.com/SwiftFiat/SwiftFiat-Backend/service/security"
	"github.com/gin-gonic/gin"
)

func AuthenticatedMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := ctx.GetHeader("Authorization")
		if token == "" {
			ctx.JSON(http.StatusUnauthorized, gin.H{"message": "Unuathorized Request"})
			ctx.Abort()
			return
		}

		tokenSplit := strings.Split(token, " ")
		if len(tokenSplit) != 2 || strings.ToLower(tokenSplit[0]) != "bearer" {
			ctx.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid token, expects bearer token"})
			ctx.Abort()
			return
		}

		user, err := TokenController.VerifyToken(tokenSplit[1])
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			ctx.Abort()
			return
		}

		// TODO: Add Cache Layer Validation for Password Changes etc.
		// https://stackoverflow.com/questions/21978658/invalidating-json-web-tokens
		// e.g security.CacheInstance.Get(string(rune(user.UserID)))

		ctx.Set("user_id", user.UserID)
		ctx.Set("user_role", user.Role)
		ctx.Set("user_verified", user.Verified)
		/// Accessible User Across the App
		ctx.Set("user", user)
		ctx.Next()
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Header("Access-Control-Allow-Methods", "POST,HEAD,PATCH,OPTIONS,GET,PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
