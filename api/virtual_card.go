package api

import (
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bridgecards"
	virtualcard "github.com/SwiftFiat/SwiftFiat-Backend/services/virtual_card"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)
 
type Virtualcard struct {
	server         *Server
	virtualCardSvc *virtualcard.Service
}

func (v Virtualcard) router(server *Server) {
	v.server = server
	v.virtualCardSvc = server.virtualcard
	v1 := server.router.Group("/api/v1/cards")
	// v1.Use(server.authMiddleware.AuthenticatedMiddleware())
	{
		v1.POST("/", server.authMiddleware.AuthenticatedMiddleware(), v.CreateCard)
		v1.POST("/register-card-holder", server.authMiddleware.AuthenticatedMiddleware(), v.RegisterCardHolder)
		v1.POST("/webhook", v.Webhook)
		v1.POST("/fund-issuing-wallet", v.FundIssuingWallet)
		v1.GET("/get-card-balance", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardBalance)
		v1.POST("/fund-card", server.authMiddleware.AuthenticatedMiddleware(), v.FundCard)
	}

}

func (v *Virtualcard) FundIssuingWallet(c *gin.Context) {
	var req bridgecards.FundIssuingWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	
	message := v.virtualCardSvc.FundIssuingWallet(c, req)
	c.JSON(http.StatusOK, basemodels.NewSuccess(message, nil))
}

type CreateCardRequest struct {
	Brand          string    `json:"card_brand"` // "visa" or "mastercard"
	FundingAmount  string    `json:"funding_amount" binding:"required"`          // Initial funding amount
	CardPlanID     int64     `json:"card_plan_id" binding:"required"`
	CardName       string    `json:"card_name" binding:"required"`
	CardColor      string    `json:"color" binding:"omitempty"`
	SourceWalletID uuid.UUID `json:"source_wallet_id" binding:"required"`
}

// @Summary Create virtual card
// @Description Create a new USD virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body CreateCardRequest true "Card creation parameters"
// @Success 201 {object} virtualcard.CreateCardResult
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards [post]
func (v *Virtualcard) CreateCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req CreateCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	result, err := v.virtualCardSvc.CreateCard(c, &bridgecards.CreateCardRequest{
		UserID:         activeUser.UserID,
		CardPlanID:     req.CardPlanID,
		CardName:       req.CardName,
		CardColor:      req.CardColor,
		FundingAmount:  req.FundingAmount,
		SourceWalletID: req.SourceWalletID,
		Brand:          req.Brand,
	})

	v.server.logger.Infof("create card result is ====: %v", result)

	if err != nil {
		switch err {
		case virtualcard.ErrInsufficientFunds:
			c.JSON(http.StatusBadRequest, basemodels.NewError("insufficient wallet balance"))
		case virtualcard.ErrPlanLimitExceeded:
			c.JSON(http.StatusBadRequest, basemodels.NewError("card plan limit exceeded"))
		case virtualcard.ErrInvalidCardPlan:
			c.JSON(http.StatusBadRequest, basemodels.NewError("invalid card plan"))
		default:
			c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		}
		return
	}

	c.JSON(http.StatusCreated,  result)
}

// @Summary Register cardholder
// @Description Register a new cardholder with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} bridgecards.CreateCardHolderResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/register-cardholder [post]
func (v *Virtualcard) RegisterCardHolder(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	response, err := v.virtualCardSvc.CreateCardHolder(c, int32(activeUser.UserID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK,  response)
}

// @Summary Get card balance
// @Description Get the balance of a virtual card
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.GetCardBalanceResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/get-card-balance [get]
func (v *Virtualcard) GetCardBalance(c *gin.Context) {
	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}

	response, err := v.virtualCardSvc.GetCardBalance(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK,  response)
}

// @Summary Fund virtual card
// @Description Fund a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body bridgecards.FundCardRequest true "Card funding parameters"
// @Success 200 {object} bridgecards.FundCardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/fund-card [post]
func (v *Virtualcard) FundCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}	
	var req bridgecards.FundCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	req.TransactionReference = utils.NewTxRef("fund-card")
	response, err := v.virtualCardSvc.FundCard(c, req, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// @Summary Handle BridgeCard webhooks
// @Description Process webhook events from BridgeCard
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param X-Webhook-Signature header string true "BridgeCard webhook signature"
// @Param payload body bridgecards.WebhookEvent true "Webhook payload"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/webhook [post]
func (v *Virtualcard) Webhook(c *gin.Context) {
	// 1. Extract and verify webhook signature
	// signature := c.GetHeader("x-webhook-signature")
	// if signature == "" {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError("missing webhook signature"))
	// 	return
	// }

	// 2. Read the request body
	body, err := c.GetRawData()
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to read webhook body: %v", err))
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// // 3. Verify the webhook signature
	// isValid, err := v.virtualCardSvc.VerifyWebhookSignature(body, signature)
	// if err != nil {
	// 	v.server.logger.Error(fmt.Sprintf("webhook signature verification failed: %v", err))
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError("invalid webhook signature"))
	// 	return
	// }

	// if !isValid {
	// 	v.server.logger.Error("invalid webhook signature")
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError("invalid webhook signature"))
	// 	return
	// }

	v.server.logger.Infof("webhook body\n: %s", string(body))

	// 4. Parse and process the webhook
	eventType, err := v.virtualCardSvc.ProcessWebhook(c.Request.Context(), body)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to process webhook: %v", err))

		// Still return 200 to BridgeCard to avoid retries
		// Log the error internally for debugging
		c.JSON(http.StatusOK, basemodels.NewSuccess("webhook received but processing failed", nil))
		return
	}

	v.server.logger.Info(fmt.Sprintf("Successfully processed webhook event: %s", eventType))
	c.JSON(http.StatusOK, basemodels.NewSuccess("webhook processed successfully", nil))
}
