package api

import (
	"fmt"
	"net/http"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// WebhookReplayRequest is the admin request to replay a webhook
type WebhookReplayRequest struct {
	WebhookID string `json:"webhook_id" binding:"required"`
	Reason    string `json:"reason" binding:"required"`
}

// ReplayWebhookEndpoint allows admins to manually replay webhooks
func (c *CryptoAPI) ReplayWebhookEndpoint(ctx *gin.Context) {
	var req WebhookReplayRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid request: "+err.Error()))
		return
	}

	webhookID, err := uuid.Parse(req.WebhookID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid webhook_id format"))
		return
	}

	// Record the replay attempt
	replayID, err := c.webhookAudit.ReplayWebhook(ctx, webhookID, "admin", req.Reason)
	if err != nil {
		c.server.logger.Error("replay_record_failed", "error", err, "webhook_id", webhookID)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to record replay attempt"))
		return
	}

	// TODO: Fetch the stored webhook, parse payload, and call processStoredWebhook
	// For now, return a placeholder message
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Webhook replay recorded", gin.H{
		"replay_id": replayID,
		"status":    "pending",
	}))
}

// ListWebhooksEndpoint returns admin dashboard with webhook list
func (c *CryptoAPI) ListWebhooksEndpoint(ctx *gin.Context) {
	page := 1
	limit := 50

	// Parse pagination params if provided
	if p := ctx.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if l := ctx.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	offset := (page - 1) * limit
	status := ctx.Query("status") // Optional status filter

	webhooks, total, err := c.webhookAudit.ListWebhooks(ctx, limit, offset, status)
	if err != nil {
		c.server.logger.Error("webhook_list_failed", "error", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to list webhooks"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Webhooks retrieved successfully", gin.H{
		"webhooks": webhooks,
		"total":    total,
		"page":     page,
		"limit":    limit,
		"pages":    (total + int64(limit) - 1) / int64(limit),
	}))
}

// GetWebhookEndpoint retrieves details of a specific webhook
func (c *CryptoAPI) GetWebhookEndpoint(ctx *gin.Context) {
	webhookID := ctx.Param("webhook_id")
	webhookUUID, err := uuid.Parse(webhookID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid webhook_id format"))
		return
	}

	webhook, err := c.webhookAudit.GetWebhookByID(ctx, webhookUUID)
	if err != nil {
		c.server.logger.Error("webhook_get_failed", "error", err, "webhook_id", webhookID)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve webhook details"))
		return
	}

	if webhook == nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("Webhook not found"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Webhook retrieved successfully", webhook))
}
