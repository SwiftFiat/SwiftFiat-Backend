package api

import (
	"fmt"
	"net/http"
	"strings"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	_ "github.com/SwiftFiat/SwiftFiat-Backend/services/security"
	"github.com/gin-gonic/gin"
)

type AuthMiddleware struct {
	redisClient *redis.RedisService
}

func NewAuthMiddleware(redisClient *redis.RedisService) *AuthMiddleware {
	return &AuthMiddleware{redisClient: redisClient}
}

func (a *AuthMiddleware) AuthenticatedMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := ctx.GetHeader("Authorization")
		var tokenString string

		if token == "" {
			// Check query parameter for WebSocket support
			tokenString = ctx.Query("token")
			if tokenString == "" {
				ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Unauthorized Request, token is empty"))
				ctx.Abort()
				return
			}
		} else {
			tokenSplit := strings.Split(token, " ")
			if len(tokenSplit) != 2 || strings.ToLower(tokenSplit[0]) != "bearer" {
				ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid token, expects bearer token"))
				ctx.Abort()
				return
			}
			tokenString = tokenSplit[1]
		}

		user, err := TokenController.VerifyToken(tokenString)
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError(fmt.Sprintf("Unknown Error: %v", err.Error())))
			ctx.Abort()
			return
		}

		userToken, err := a.redisClient.Get(ctx, fmt.Sprintf("user:%d", user.UserID))
		if err != nil {
			if err.Error() == "redis: nil" {
				ctx.JSON(http.StatusUnauthorized, basemodels.NewError("User Token Not Found"))
				ctx.Abort()
				return
			}
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError(fmt.Sprintf("Unknown Error: %v", err.Error())))
			ctx.Abort()
			return
		}

		// fmt.Println("Token:", token)
		// fmt.Println("User Token:", userToken)
		if userToken != tokenString {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("User Token Mismatch"))
			ctx.Abort()
			return
		}

		// TODO: Add Cache Layer Validation for Password Changes etc.
		// https://stackoverflow.com/questions/21978658/invalidating-json-web-tokens
		// e.g security.CacheInstance.Get(string(rune(user.UserID)))

		ctx.Set("user_id", user.UserID)
		ctx.Set("user_role", user.Role)
		ctx.Set("user_verified", user.Verified)
		ctx.Set("email", user.Email)
		ctx.Set("user_tag", user.UserTag)
		/// Accessible User Across the App
		ctx.Set("user", user)
		ctx.Next()
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		// These are critical for POST/PUT/DELETE requests
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		// Important: Browser sends an OPTIONS request before the actual POST request
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
