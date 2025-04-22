package api

import (
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	activitylogs "github.com/SwiftFiat/SwiftFiat-Backend/services/activity_logs"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type ActivityLog struct {
	server *Server
	service activitylogs.ActivityLog
}

func (h ActivityLog) router(server *Server) {
	h.server = server
	h.service = *activitylogs.NewActivityLog(*h.server.queries)

	serverGroupV1 := server.router.Group("/api/v1/activitylogs")
	serverGroupV1.GET("/:id", h.server.authMiddleware.AuthenticatedMiddleware(), h.GetUserActivity)
	serverGroupV1.GET("/recent",h.server.authMiddleware.AuthenticatedMiddleware(), h.GetRecentActivity)
}

func (h *ActivityLog) GetUserActivity(c *gin.Context) {
	user, _ := utils.GetActiveUser(c)
	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	logs, err := h.service.GetByUser(c.Request.Context(), int32(userID), int32(limit), int32(offset))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get activity logs"})
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Activity logs retrieved successfully", logs))

}

func (h *ActivityLog) GetRecentActivity(c *gin.Context) {
	user, err := utils.GetActiveUser(c)
	if err != nil {
		h.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	logs, err := h.service.GetRecent(c.Request.Context(), int32(limit), int32(offset))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get recent activity logs"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Activity logs retrieved successfully", logs))
}