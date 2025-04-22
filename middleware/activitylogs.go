// internal/middleware/activity_logger.go
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"slices"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	activitylogs "github.com/SwiftFiat/SwiftFiat-Backend/services/activity_logs"
	"github.com/gin-gonic/gin"
)

type ActivityLogMiddleware struct {
	store db.Store
}

func NewActivityLogMiddleware(store db.Store) *ActivityLogMiddleware {
	return &ActivityLogMiddleware{
		store: store,
	}
}

func (a *ActivityLogMiddleware) ActivityLogger(s activitylogs.ActivityLog) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip logging for certain endpoints if needed
		if shouldSkipLogging(c.Request.URL.Path) {
			c.Next()
			return
		}

		// Process request first
		c.Next()

		// Get user from context if authenticated
		var userID *int32
		if uid, exists := c.Get("userID"); exists {
			if u, ok := uid.(int32); ok {
				userID = &u
			}
		}

		// Create log in background to not block the response
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			createdAt := time.Now()

			// Handle nil userID gracefully
			var action string
			if userID != nil {
				action = a.getActionFromRequest(c, *userID, createdAt)
			} else {
				action = fmt.Sprintf("Unauthenticated request to %s", c.FullPath())
			}
			_, _ = s.Create(ctx, activitylogs.CreateActivityLogParams{
				UserID:    userID,
				Action:    action,
				IPAddress: c.ClientIP(),
				UserAgent: c.Request.UserAgent(),
				CreatedAt: createdAt,
			})
		}()
	}
}

func shouldSkipLogging(path string) bool {
	// Define the list of routes that should be logged.
	// Should be in sync with routes in getActionFromRequest
	allowedPaths := []string{
		"/login", 
		"/register", 
		"/register-admin", 
		"/change-password",
		"/reset-password",
	}
	return slices.Contains(allowedPaths, path)
}

func (a *ActivityLogMiddleware) getActionFromRequest(c *gin.Context, userID int32, createdAt time.Time) string {
	user, err := a.store.GetUserByID(context.Background(), int64(userID))
	if err != nil {
		return fmt.Sprintf("Request from unknown user to %s", c.FullPath())
	}
	timeElapsed := time.Since(createdAt)
	switch {
	case c.Request.Method == http.MethodPost && c.FullPath() == "/login":
		return fmt.Sprintf("user %s logged in %s ago", user.FirstName.String, timeElapsed)
	case c.Request.Method == http.MethodPost && c.FullPath() == "/register":
		return fmt.Sprintf("user %s registered %s ago", user.FirstName.String, timeElapsed)
	case c.Request.Method == http.MethodPost && c.FullPath() == "/register-admin":
		return fmt.Sprintf("user %s registered as admin %s ago", user.FirstName.String, timeElapsed)
	case c.Request.Method == http.MethodPost && c.FullPath() == "/change-password":
		return fmt.Sprintf("user %s changes password %s ago", user.FirstName.String, timeElapsed)
	case c.Request.Method == http.MethodPost && c.FullPath() == "/reset-password":
		return fmt.Sprintf("user %s reset password %s ago", user.FirstName.String, timeElapsed)
	// Add more specific cases as needed
	default:
		return c.Request.Method + " " + c.FullPath()
	}
}