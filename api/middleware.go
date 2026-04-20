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

// parseBearerCredential extracts the JWT from Authorization using strings.Fields
// so "Bearer   <jwt>" and normal "Bearer <jwt>" both work. Returns an error when
// the scheme is wrong, the credential is missing, or extra space-separated parts
// exist (would yield a truncated/wrong token if we only took fields[1]).
func parseBearerCredential(authHeader string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(authHeader))
	if len(fields) < 2 {
		return "", fmt.Errorf("invalid token, expects bearer token")
	}
	if strings.ToLower(fields[0]) != "bearer" {
		return "", fmt.Errorf("invalid token, expects bearer token")
	}
	if len(fields) > 2 {
		return "", fmt.Errorf("invalid token, JWT must be a single credential after Bearer")
	}
	t := strings.Trim(strings.TrimSpace(fields[1]), `"'`)
	if t == "" {
		return "", fmt.Errorf("invalid token, bearer credential is empty")
	}
	return t, nil
}

// credentialLooksLikeJWT is a cheap guard before jwt.Parse; refresh tokens in
// this API are opaque hex strings and produce "invalid number of segments".
func credentialLooksLikeJWT(s string) bool {
	return strings.Count(s, ".") == 2 && s[0] != '.' && s[len(s)-1] != '.'
}

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
			tokenString = strings.Trim(strings.TrimSpace(ctx.Query("token")), `"'`)
			if tokenString == "" {
				ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Unauthorized Request, token is empty"))
				ctx.Abort()
				return
			}
		} else {
			var err error
			tokenString, err = parseBearerCredential(token)
			if err != nil {
				ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
				ctx.Abort()
				return
			}
		}

		if !credentialLooksLikeJWT(tokenString) {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError(
				"Invalid access token: expected a JWT (three dot-separated parts from login). "+
					"Use data.access_token as Authorization: Bearer <access_token>; do not send refresh_token here."))
			ctx.Abort()
			return
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

		ctx.Set("user_id", user.UserID)
		ctx.Set("user_role", user.Role)
		ctx.Set("user_verified", user.Verified)
		ctx.Set("email", user.Email)
		ctx.Set("user_tag", user.UserTag)
		/// Accessible User Across the App
		ctx.Set("user", user)

		// Forward session_id so SessionBlockMiddleware can validate it.
		// Old tokens (pre-session-manager) won't have this field; that's fine.
		if user.SessionID != "" {
			ctx.Set("session_id", user.SessionID)
		}

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