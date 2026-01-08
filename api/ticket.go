package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	chatsupport "github.com/SwiftFiat/SwiftFiat-Backend/services/chat_support"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ChatSupport struct {
	server        *Server
	chatService   *chatsupport.ChatService
	ticketService *chatsupport.TicketService
	aiService     *chatsupport.AIService
	audit         *audit.Service
}

var EntityType = "support"

func (c ChatSupport) router(server *Server) {
	c.server = server
	c.chatService = server.chatService
	c.ticketService = server.ticketService
	c.aiService = server.aiService
	c.audit = server.auditService

	// User endpoints
	userGroup := server.router.Group("/api/v1/chat")
	userGroup.Use(c.server.authMiddleware.AuthenticatedMiddleware())
	{
		userGroup.POST("/message", c.sendMessage)
		userGroup.GET("/ticket/:ticketId/messages", c.getMessages)
		userGroup.GET("/my-tickets", c.getMyTickets)
		userGroup.POST("/escalate/:ticketId", c.requestEscalation)

		userGroup.GET("/admin/support/tickets", c.listTickets)
		userGroup.GET("/admin/support/tickets/unassigned", c.getUnassignedTickets)
		userGroup.GET("/admin/support/tickets/:ticketId", c.getTicketDetails)
		userGroup.POST("/admin/support/tickets/:ticketId/claim", c.claimTicket)
		userGroup.PATCH("/admin/support/tickets/:ticketId/assign", c.assignTicket)
		userGroup.PATCH("/admin/support/tickets/:ticketId/status", c.updateTicketStatus)
		userGroup.POST("/admin/support/tickets/:ticketId/resolve", c.resolveTicket)
		userGroup.POST("/admin/support/tickets/:ticketId/message", c.sendAdminMessage)
		userGroup.GET("/admin/support/statistics", c.getStatistics)
		userGroup.GET("/admin/support/my-tickets", c.getMyAssignedTickets)

		userGroup.POST("/admin/faq", c.createFAQ)
		userGroup.GET("/admin/faq", c.listFAQs)
		userGroup.GET("/admin/faq/:faqId", c.getFAQ)
		userGroup.PUT("/admin/faq/:faqId", c.updateFAQ)
		userGroup.DELETE("/admin/faq/:faqId", c.deleteFAQ)
		userGroup.POST("/admin/:faqId/helpful", c.markFAQHelpful)
	}
}

// sendMessage godoc
// @Summary Send a chat message
// @Description Send a message in a support conversation (AI or human)
// @Tags chat
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param text formData string true "Message text"
// @Param attachment formData file false "Image attachment (max 500KB)"
// @Success 200 {object} basemodels.SuccessResponse{data=object{message=chatsupport.ChatMessageResponse,ai_response=chatsupport.AIQueryResponse,ticket_id=int64,escalated=bool}}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/message [post]
func (c *ChatSupport) sendMessage(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	messageText := ctx.PostForm("text")
	if messageText == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("message text is required"))
		return
	}

	// Check if user has an open ticket
	openTicketCount, err := c.server.queries.CountUserOpenTickets(ctx, activeUser.UserID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to check open tickets" + err.Error()))
		return
	}

	var ticketID int64
	var aiResponse *chatsupport.AIQueryResponse
	escalated := false

	// If no open ticket, start with AI
	if openTicketCount == 0 {
		// Query AI first
		conversationContext := []chatsupport.ConversationMessage{}

		aiResponse, err = c.aiService.QueryAI(ctx, &chatsupport.AIQueryRequest{
			Message:             messageText,
			ConversationContext: conversationContext,
			UserID:              activeUser.UserID,
		})
		if err != nil {
			c.server.logger.Error(fmt.Sprintf("AI query failed: %v", err))
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("AI service temporarily unavailable"))
			return
		}

		// Create ticket
		ticket, err := c.ticketService.CreateTicket(ctx, &chatsupport.CreateTicketParams{
			UserID:           activeUser.UserID,
			EscalationReason: aiResponse.EscalationReason,
			Priority:         "medium",
			Category:         "general",
		})
		if err != nil {
			c.server.logger.Error(err.Error())
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create ticket" + err.Error()))
			return
		}
		ticketID = ticket.ID

		// Store user message
		form, _ := ctx.MultipartForm()
		files := form.File["attachment"]

		_, err = c.chatService.SendMessage(ctx, &chatsupport.SendMessageParams{
			TicketID:    ticketID,
			SenderID:    activeUser.UserID,
			SenderType:  "user",
			MessageText: messageText,
			Attachments: files,
		})
		if err != nil {
			c.server.logger.Error(err.Error())
		}

		// If escalation needed, assign to agent
		if aiResponse.HumanRequired {
			escalated = true
			_, err = c.ticketService.AutoAssignTicket(ctx, ticketID)
			if err != nil {
				c.server.logger.Error(fmt.Sprintf("auto-assignment failed: %v", err))
			}
		} else {
			// Store AI response
			_, err = c.chatService.SendMessage(ctx, &chatsupport.SendMessageParams{
				TicketID:    ticketID,
				SenderID:    0, // AI has no user ID
				SenderType:  "ai",
				MessageText: aiResponse.Answer,
			})
			if err != nil {
				c.server.logger.Error(err.Error())
			}
		}
	} else {
		// Get existing open ticket
		tickets, err := c.server.queries.ListTicketsByUser(ctx, db.ListTicketsByUserParams{
			UserID: activeUser.UserID,
			Limit:  1,
			Offset: 0,
		})
		if err != nil || len(tickets) == 0 {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve ticket" + err.Error()))
			return
		}
		ticketID = tickets[0].ID

		// Send message to existing ticket
		form, _ := ctx.MultipartForm()
		files := form.File["attachment"]

		_, err = c.chatService.SendMessage(ctx, &chatsupport.SendMessageParams{
			TicketID:    ticketID,
			SenderID:    activeUser.UserID,
			SenderType:  "user",
			MessageText: messageText,
			Attachments: files,
		})
		if err != nil {
			c.server.logger.Error(err.Error())
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to send message" + err.Error()))
			return
		}
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("message sent", gin.H{
		"ticket_id":   ticketID,
		"escalated":   escalated,
		"ai_response": aiResponse,
	}))
}

// getMessages godoc
// @Summary Get conversation history
// @Description Retrieve all messages for a specific ticket
// @Tags chat
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Success 200 {object} basemodels.SuccessResponse{data=[]chatsupport.ChatMessageResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/ticket/{ticketId}/messages [get]
func (c *ChatSupport) getMessages(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.ParseInt(ticketIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	// Verify ticket belongs to user (or user is admin)
	ticket, err := c.server.queries.GetTicketByID(ctx, ticketID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("ticket not found"))
		return
	}

	if ticket.UserID != activeUser.UserID && activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	messages, err := c.chatService.GetConversationHistory(ctx, ticketID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve messages" + err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("messages retrieved", messages))
}

// getMyTickets godoc
// @Summary Get my support tickets
// @Description Retrieve all support tickets for the authenticated user
// @Tags chat
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Limit" default(50)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]TicketResponse}
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/my-tickets [get]
func (c *ChatSupport) getMyTickets(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	tickets, err := c.ticketService.GetTicketsByUser(ctx, activeUser.UserID, int32(limit), int32(offset))
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve tickets" + err.Error()))
		return
	}

	var ticketResponses []TicketResponse
	for _, ticket := range tickets {
		ticketResponses = append(ticketResponses, MapTicketToresponse(ticket))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("tickets retrieved", ticketResponses))
}

type TicketResponse struct {
	ID                  int64      `json:"id"`
	UserID              int64      `json:"user_id"`
	Status              string     `json:"status"`
	AssignedTo          *int64     `json:"assigned_to"`
	EscalationReason    *string    `json:"escalation_reason"`
	Priority            string     `json:"priority"`
	Category            *string    `json:"category"`
	ResolvedAt          *time.Time `json:"resolved_at"`
	FirstResponseAt     *time.Time `json:"first_response_at"`
	AverageResponseTime *int32     `json:"average_response_time"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func MapTicketToresponse(raw db.Ticket) TicketResponse {
	return TicketResponse{
		ID:                  raw.ID,
		UserID:              raw.UserID,
		Status:              raw.Status,
		AssignedTo:          &raw.AssignedTo.Int64,
		EscalationReason:    &raw.EscalationReason.String,
		Priority:            raw.Priority,
		Category:            &raw.Category.String,
		ResolvedAt:          &raw.ResolvedAt.Time,
		FirstResponseAt:     &raw.FirstResponseAt.Time,
		AverageResponseTime: &raw.AverageResponseTime.Int32,
		CreatedAt:           raw.CreatedAt,
		UpdatedAt:           raw.UpdatedAt,
	}
}

// requestEscalation godoc
// @Summary Request human support
// @Description Manually request escalation to human support
// @Tags chat
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/escalate/{ticketId} [post]
func (c *ChatSupport) requestEscalation(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.Atoi(ticketIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	// Verify ticket belongs to user
	ticket, err := c.server.queries.GetTicketByID(ctx, int64(ticketID))
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("ticket not found"))
		return
	}

	if ticket.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}
	// Auto-assign to available agent
	_, err = c.ticketService.AutoAssignTicket(ctx, int64(ticketID))
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("escalation failed" + err.Error()))
		return
	}

	// Send system message
	_, err = c.chatService.SendMessage(ctx, &chatsupport.SendMessageParams{
		TicketID:    int64(ticketID),
		SenderID:    0,
		SenderType:  "system",
		MessageText: "A support agent will join your conversation shortly.",
	})
	if err != nil {
		c.server.logger.Error(err.Error())
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("escalated to human support", nil))

}

// listTickets godoc [admin]
// @Summary List all tickets
// @Description Get paginated list of support tickets (admin only)
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status"
// @Param limit query int false "Limit" default(50)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]ListTicketsByStatusRow}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets [get]
func (c *ChatSupport) listTickets(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	status := ctx.Query("status")
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	var tickets []db.ListTicketsByStatusRow
	if status != "" {
		tickets, err = c.ticketService.GetTicketsByStatus(ctx, status, int32(limit), int32(offset))
	} else {
		tickets, err = c.ticketService.GetTicketsByStatus(ctx, "open", int32(limit), int32(offset))
	}

	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve tickets" + err.Error()))
		return
	}

	var response []ListTicketsByStatusRow
	for _, ticket := range tickets {
		response = append(response, MapListTicketToresponse(ticket))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("tickets retrieved", response))
}

type ListTicketsByStatusRow struct {
	ID                  int64      `json:"id"`
	UserID              int64      `json:"user_id"`
	Status              string     `json:"status"`
	AssignedTo          *int64     `json:"assigned_to"`
	EscalationReason    *string    `json:"escalation_reason"`
	Priority            string     `json:"priority"`
	Category            *string    `json:"category"`
	ResolvedAt          *time.Time `json:"resolved_at"`
	FirstResponseAt     *time.Time `json:"first_response_at"`
	AverageResponseTime *int32     `json:"average_response_time"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	FirstName           *string    `json:"first_name"`
	LastName            *string    `json:"last_name"`
	Email               string     `json:"email"`
}

func MapListTicketToresponse(raw db.ListTicketsByStatusRow) ListTicketsByStatusRow {
	return ListTicketsByStatusRow{
		ID:                  raw.ID,
		UserID:              raw.UserID,
		Status:              raw.Status,
		AssignedTo:          &raw.AssignedTo.Int64,
		EscalationReason:    &raw.EscalationReason.String,
		Priority:            raw.Priority,
		Category:            &raw.Category.String,
		ResolvedAt:          &raw.ResolvedAt.Time,
		FirstResponseAt:     &raw.FirstResponseAt.Time,
		AverageResponseTime: &raw.AverageResponseTime.Int32,
		CreatedAt:           raw.CreatedAt,
		UpdatedAt:           raw.UpdatedAt,
		FirstName:           &raw.FirstName.String,
		LastName:            &raw.LastName.String,
		Email:               raw.Email,
	}
}

// getUnassignedTickets godoc [admin]
// @Summary Get unassigned tickets
// @Description Retrieve all tickets that haven't been assigned to an agent
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Limit" default(50)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]ListUnassignedTicketsRow}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/unassigned [get]
func (c *ChatSupport) getUnassignedTickets(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	tickets, err := c.ticketService.GetUnassignedTickets(ctx, int32(limit), int32(offset))
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve tickets" + err.Error()))
		return
	}

	var response []ListUnassignedTicketsRow
	for _, ticket := range tickets {
		response = append(response, MapListUnassignedTicketsRowToresponse(ticket))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("unassigned tickets retrieved", response))
}

type ListUnassignedTicketsRow struct {
	ID                  int64      `json:"id"`
	UserID              int64      `json:"user_id"`
	Status              string     `json:"status"`
	AssignedTo          *int64     `json:"assigned_to"`
	EscalationReason    *string    `json:"escalation_reason"`
	Priority            string     `json:"priority"`
	Category            *string    `json:"category"`
	ResolvedAt          *time.Time `json:"resolved_at"`
	FirstResponseAt     *time.Time `json:"first_response_at"`
	AverageResponseTime *int32     `json:"average_response_time"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	FirstName           *string    `json:"first_name"`
	LastName            *string    `json:"last_name"`
	Email               string     `json:"email"`
}

func MapListUnassignedTicketsRowToresponse(raw db.ListUnassignedTicketsRow) ListUnassignedTicketsRow {
	return ListUnassignedTicketsRow{
		ID:                  raw.ID,
		UserID:              raw.UserID,
		Status:              raw.Status,
		AssignedTo:          &raw.AssignedTo.Int64,
		EscalationReason:    &raw.EscalationReason.String,
		Priority:            raw.Priority,
		Category:            &raw.Category.String,
		ResolvedAt:          &raw.ResolvedAt.Time,
		FirstResponseAt:     &raw.FirstResponseAt.Time,
		AverageResponseTime: &raw.AverageResponseTime.Int32,
		CreatedAt:           raw.CreatedAt,
		UpdatedAt:           raw.UpdatedAt,
		FirstName:           &raw.FirstName.String,
		LastName:            &raw.LastName.String,
		Email:               raw.Email,
	}
}

// getTicketDetails godoc [admin]
// @Summary Get ticket details
// @Description Retrieve detailed information about a specific ticket
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Success 200 {object} basemodels.SuccessResponse{data=object{ticket=ListUnassignedTicketsRow,messages=[]chatsupport.ChatMessageResponse}}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/{ticketId} [get]
func (c *ChatSupport) getTicketDetails(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.ParseInt(ticketIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	ticket, err := c.server.queries.GetTicketWithUserDetails(ctx, ticketID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("ticket not found"))
		return
	}

	messages, err := c.chatService.GetConversationHistory(ctx, ticketID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve messages" + err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("ticket details retrieved", gin.H{
		"ticket":   MapGetTicketUserWithDetailsToResponse(ticket),
		"messages": messages,
	}))
}

func MapGetTicketUserWithDetailsToResponse(raw db.GetTicketWithUserDetailsRow) ListUnassignedTicketsRow {
	return ListUnassignedTicketsRow{
		ID:                  raw.ID,
		UserID:              raw.UserID,
		Status:              raw.Status,
		AssignedTo:          &raw.AssignedTo.Int64,
		EscalationReason:    &raw.EscalationReason.String,
		Priority:            raw.Priority,
		Category:            &raw.Category.String,
		ResolvedAt:          &raw.ResolvedAt.Time,
		FirstResponseAt:     &raw.FirstResponseAt.Time,
		AverageResponseTime: &raw.AverageResponseTime.Int32,
		CreatedAt:           raw.CreatedAt,
		UpdatedAt:           raw.UpdatedAt,
		FirstName:           &raw.FirstName.String,
		LastName:            &raw.LastName.String,
		Email:               raw.Email,
	}
}

// claimTicket godoc [admin]
// @Summary Claim a ticket
// @Description Claim an unassigned ticket for yourself
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Success 200 {object} basemodels.SuccessResponse{data=TicketResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/{ticketId}/claim [post]
func (c *ChatSupport) claimTicket(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role != models.CUSTOMER_REP {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.Atoi(ticketIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	// Get support admin record
	supportAdmin, err := c.server.queries.GetSupportAdminByUserID(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusForbidden, basemodels.NewError("support admin profile not found"))
			return
		}
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve admin profile" + err.Error()))
		return
	}

	ticket, err := c.ticketService.ClaimTicket(ctx, int64(ticketID), supportAdmin.ID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to claim ticket" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventTicketClaimed,
		fmt.Sprint(ticketID),
		fmt.Sprintf("Admin %d claimed ticket %d", activeUser.UserID, ticketID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("ticket claimed successfully", MapTicketToresponse(*ticket)))
}

// assignTicket godoc
// @Summary Assign a ticket
// @Description Assign a ticket to a specific support agent
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Param assignRequest body object{admin_id=int64} true "Assignment request"
// @Success 200 {object} basemodels.SuccessResponse{data=TicketResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/{ticketId}/assign [patch]
func (c *ChatSupport) assignTicket(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER || activeUser.Role == models.CUSTOMER_REP {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.Atoi(ticketIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	var req struct {
		AdminID int64 `json:"admin_id" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	ticket, err := c.ticketService.AssignTicket(ctx, int64(ticketID), req.AdminID, activeUser.UserID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to assign ticket" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewUserLog(
		ctx,
		audit.EventTicketAssigned,
		fmt.Sprint(ticketID),
		activeUser.Role,
		fmt.Sprintf("Admin %d assigned ticket %d to admin %d", activeUser.UserID, ticketID, req.AdminID),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionUpdate,
		true,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("ticket assigned successfully", MapTicketToresponse(*ticket)))
}

// updateTicketStatus godoc
// @Summary Update ticket status
// @Description Update the status of a support ticket
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Param statusRequest body object{status=string} true "Status update request"
// @Success 200 {object} basemodels.SuccessResponse{data=TicketResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/{ticketId}/status [patch]
func (c *ChatSupport) updateTicketStatus(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.Atoi(ticketIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	ticket, err := c.ticketService.UpdateTicketStatus(ctx, int64(ticketID), req.Status)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update ticket status" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventTicketStatusUpdated,
		fmt.Sprint(ticketID),
		fmt.Sprintf("Admin %d updated ticket %d status to %s", activeUser.UserID, ticketID, req.Status),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("ticket status updated", MapTicketToresponse(*ticket)))
}

// resolveTicket godoc
// @Summary Resolve a ticket
// @Description Mark a ticket as resolved
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Success 200 {object} basemodels.SuccessResponse{data=TicketResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/{ticketId}/resolve [post]
func (c *ChatSupport) resolveTicket(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	ticketIDStr := ctx.Param("ticketId")
	ticketID, err := strconv.Atoi(ticketIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	ticket, err := c.ticketService.ResolveTicket(ctx, int64(ticketID))
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to resolve ticket" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventTicketResolved,
		fmt.Sprint(ticketID),
		fmt.Sprintf("Admin %d resolved ticket %d", activeUser.UserID, ticketID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("ticket resolved successfully", MapTicketToresponse(*ticket)))
}

// sendAdminMessage godoc
// @Summary Send admin message
// @Description Send a message as a support agent
// @Tags admin-support
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param ticketId path string true "Ticket ID"
// @Param text formData string true "Message text"
// @Param attachment formData file false "Image attachment (max 500KB)"
// @Success 200 {object} basemodels.SuccessResponse{data=chatsupport.ChatMessageResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/tickets/{ticketId}/message [post]
func (c *ChatSupport) sendAdminMessage(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}
	ticketIDStr := ctx.Param("ticketId")

	ticketID, err := strconv.Atoi(ticketIDStr)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid ticket ID"))
		return
	}

	messageText := ctx.PostForm("text")
	if messageText == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("message text is required"))
		return
	}

	form, _ := ctx.MultipartForm()
	files := form.File["attachment"]

	message, err := c.chatService.SendMessage(ctx, &chatsupport.SendMessageParams{
		TicketID:    int64(ticketID),
		SenderID:    activeUser.UserID,
		SenderType:  "admin",
		MessageText: messageText,
		Attachments: files,
	})
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to send message" + err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("message sent", message))
}

// getStatistics godoc
// @Summary Get support statistics
// @Description Retrieve support ticket statistics
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param since query string false "Start date (RFC3339 format)" default("2024-01-01T00:00:00Z")
// @Success 200 {object} basemodels.SuccessResponse{data=db.GetTicketStatisticsRow}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/statistics [get]
func (c *ChatSupport) getStatistics(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Default to last 30 days
	since := time.Now().AddDate(0, 0, -30)
	if sinceStr := ctx.Query("since"); sinceStr != "" {
		parsedTime, err := time.Parse(time.RFC3339, sinceStr)
		if err == nil {
			since = parsedTime
		}
	}

	stats, err := c.ticketService.GetTicketStatistics(ctx, since)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve statistics" + err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("statistics retrieved", stats))
}

// getMyAssignedTickets godoc
// @Summary Get my assigned tickets
// @Description Retrieve all tickets assigned to the authenticated admin
// @Tags admin-support
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Limit" default(50)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]ListUnassignedTicketsRow}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/chat/admin/support/my-tickets [get]
func (c *ChatSupport) getMyAssignedTickets(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Get support admin record
	supportAdmin, err := c.server.queries.GetSupportAdminByUserID(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("no tickets assigned", []interface{}{}))
			return
		}
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve admin profile" + err.Error()))
		return
	}

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	tickets, err := c.ticketService.GetTicketsByAssignedAdmin(ctx, supportAdmin.ID, int32(limit), int32(offset))
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve tickets" + err.Error()))
		return
	}

	var response []ListUnassignedTicketsRow
	for _, reponse := range tickets {
		response = append(response, MapListTicketsByAssignedAdminRowToResponse(reponse))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("tickets retrieved", response))
}

func MapListTicketsByAssignedAdminRowToResponse(raw db.ListTicketsByAssignedAdminRow) ListUnassignedTicketsRow {
	return ListUnassignedTicketsRow{
		ID:                  raw.ID,
		UserID:              raw.UserID,
		Status:              raw.Status,
		AssignedTo:          &raw.AssignedTo.Int64,
		EscalationReason:    &raw.EscalationReason.String,
		Priority:            raw.Priority,
		Category:            &raw.Category.String,
		ResolvedAt:          &raw.ResolvedAt.Time,
		FirstResponseAt:     &raw.FirstResponseAt.Time,
		AverageResponseTime: &raw.AverageResponseTime.Int32,
		CreatedAt:           raw.CreatedAt,
		UpdatedAt:           raw.UpdatedAt,
		Email:               raw.Email,
		FirstName:           &raw.FirstName.String,
		LastName:            &raw.LastName.String,
	}
}

// createFAQ godoc
// @Summary Create FAQ document
// @Description Create a new FAQ document for the knowledge base
// @Tags admin-faq
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param faqRequest body object{title=string,content=string,category=string,tags=[]string} true "FAQ creation request"
// @Success 201 {object} basemodels.SuccessResponse{data=FaqDocumentResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/faq [post]
func (c *ChatSupport) createFAQ(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	var req struct {
		Title    string   `json:"title" binding:"required"`
		Content  string   `json:"content" binding:"required"`
		Category string   `json:"category"`
		Tags     []string `json:"tags"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	// TODO: Generate embedding using OpenAI or similar service
	embeddingID := fmt.Sprintf("emb_%s", uuid.New().String())

	faq, err := c.server.queries.CreateFAQDocument(ctx, db.CreateFAQDocumentParams{
		Title:       req.Title,
		Content:     req.Content,
		Category:    sql.NullString{String: req.Category, Valid: req.Category != ""},
		Tags:        req.Tags,
		EmbeddingID: sql.NullString{String: embeddingID, Valid: true},
		CreatedBy:   sql.NullInt64{Int64: activeUser.UserID, Valid: true},
	})
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create FAQ" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventFAQCreated,
		fmt.Sprint(faq.ID),
		fmt.Sprintf("Admin %d created FAQ: %s", activeUser.UserID, faq.Title),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("FAQ created successfully", MapFaqDocumentToResponse(faq)))
}

type FaqDocumentResponse struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	Category     *string   `json:"category"`
	Tags         []string  `json:"tags"`
	EmbeddingID  *string   `json:"embedding_id"`
	IsActive     bool      `json:"is_active"`
	ViewCount    int32     `json:"view_count"`
	HelpfulCount int32     `json:"helpful_count"`
	CreatedBy    *int64    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func MapFaqDocumentToResponse(raw db.FaqDocument) FaqDocumentResponse {
	return FaqDocumentResponse{
		ID:           raw.ID,
		Title:        raw.Title,
		Content:      raw.Content,
		Category:     &raw.Category.String,
		Tags:         raw.Tags,
		EmbeddingID:  &raw.EmbeddingID.String,
		IsActive:     raw.IsActive,
		ViewCount:    raw.ViewCount,
		HelpfulCount: raw.HelpfulCount,
		CreatedBy:    &raw.CreatedBy.Int64,
		CreatedAt:    raw.CreatedAt,
		UpdatedAt:    raw.UpdatedAt,
	}
}

// listFAQs godoc
// @Summary List FAQ documents
// @Description Retrieve paginated list of FAQ documents
// @Tags admin-faq
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Limit" default(50)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]FaqDocumentResponse}
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/faq [get]
func (c *ChatSupport) listFAQs(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	faqs, err := c.server.queries.ListFAQDocuments(ctx, db.ListFAQDocumentsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve FAQs" + err.Error()))
		return
	}

	var response []FaqDocumentResponse
	for _, r := range faqs {
		response = append(response, MapFaqDocumentToResponse(r))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("FAQs retrieved", response))
}

// getFAQ godoc
// @Summary Get FAQ document
// @Description Retrieve a specific FAQ document by ID
// @Tags admin-faq
// @Produce json
// @Security BearerAuth
// @Param faqId path string true "FAQ ID"
// @Success 200 {object} basemodels.SuccessResponse{data=FaqDocumentResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/faq/{faqId} [get]
func (c *ChatSupport) getFAQ(ctx *gin.Context) {
	faqIDStr := ctx.Param("faqId")
	faqID, err := strconv.ParseInt(faqIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid FAQ ID"))
		return
	}

	faq, err := c.server.queries.GetFAQDocumentByID(ctx, faqID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("FAQ not found"))
			return
		}
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to retrieve FAQ" + err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("FAQ retrieved", MapFaqDocumentToResponse(faq)))
}

// @Summary Update FAQ document
// @Description Update an existing FAQ document
// @Tags admin-faq
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param faqId path string true "FAQ ID"
// @Param faqRequest body object{title=string,content=string,category=string,tags=[]string} true "FAQ update request"
// @Success 200 {object} basemodels.SuccessResponse{data=FaqDocumentResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/faq/{faqId} [put]
func (c *ChatSupport) updateFAQ(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	faqIDStr := ctx.Param("faqId")
	faqID, err := strconv.ParseInt(faqIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid FAQ ID"))
		return
	}

	var req struct {
		Title    string   `json:"title" binding:"required"`
		Content  string   `json:"content" binding:"required"`
		Category string   `json:"category"`
		Tags     []string `json:"tags"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request"))
		return
	}

	faq, err := c.server.queries.UpdateFAQDocument(ctx, db.UpdateFAQDocumentParams{
		ID:       faqID,
		Title:    req.Title,
		Content:  req.Content,
		Category: sql.NullString{String: req.Category, Valid: req.Category != ""},
		Tags:     req.Tags,
	})
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update FAQ" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventFAQUpdated,
		fmt.Sprint(faqID),
		fmt.Sprintf("Admin %d updated FAQ: %s", activeUser.UserID, faq.Title),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("FAQ updated successfully", MapFaqDocumentToResponse(faq)))
}

// deleteFAQ godoc
// @Summary Delete FAQ document
// @Description Soft delete an FAQ document
// @Tags admin-faq
// @Produce json
// @Security BearerAuth
// @Param faqId path string true "FAQ ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/faq/{faqId} [delete]
func (c *ChatSupport) deleteFAQ(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	faqIDStr := ctx.Param("faqId")
	faqID, err := strconv.ParseInt(faqIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid FAQ ID"))
		return
	}

	err = c.server.queries.DeactivateFAQDocument(ctx, faqID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to delete FAQ" + err.Error()))
		return
	}

	// Audit log
	logentry := audit.NewLog(
		ctx,
		audit.CategorySupport,
		audit.EventFAQDeleted,
		fmt.Sprint(faqID),
		fmt.Sprintf("Admin %d deleted FAQ %d", activeUser.UserID, faqID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	c.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("FAQ deleted successfully", nil))
}

// markFAQHelpful godoc
// @Summary Mark FAQ as helpful
// @Description Increment the helpful counter for an FAQ
// @Tags admin-faq
// @Produce json
// @Security BearerAuth
// @Param faqId path string true "FAQ ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/faq/{faqId}/helpful [post]
func (c *ChatSupport) markFAQHelpful(ctx *gin.Context) {
	faqIDStr := ctx.Param("faqId")
	faqID, err := strconv.ParseInt(faqIDStr, 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid FAQ ID"))
		return
	}

	err = c.aiService.MarkFAQHelpful(ctx, faqID)
	if err != nil {
		c.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to mark FAQ as helpful" + err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("FAQ marked as helpful", nil))
}
