package api

import (
	"fmt"
	"net/http"
	"strconv"

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
		v1.POST("/", server.authMiddleware.AuthenticatedMiddleware(), v.CreateCard) //done
		v1.POST("/register-card-holder", server.authMiddleware.AuthenticatedMiddleware(), v.RegisterCardHolder) //done
		v1.POST("/webhook", v.Webhook) //done
		v1.GET("/get-card-balance", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardBalance) // done
		v1.POST("/fund-card", server.authMiddleware.AuthenticatedMiddleware(), v.FundCard) //done
		v1.POST("/freeze-card", server.authMiddleware.AuthenticatedMiddleware(), v.FreezeCard) //done
		v1.POST("/unfreeze-card", server.authMiddleware.AuthenticatedMiddleware(), v.UnfreezeCard) //done
		v1.POST("/update-card-pin", server.authMiddleware.AuthenticatedMiddleware(), v.UpdateCardPin) //done
		v1.DELETE("/delete-card", server.authMiddleware.AuthenticatedMiddleware(), v.DeleteCard) //done
		v1.GET("/list-cards", server.authMiddleware.AuthenticatedMiddleware(), v.ListCards) //done
		v1.GET("/get-card-details", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardDetails) //done
		v1.PATCH("/debit-card", server.authMiddleware.AuthenticatedMiddleware(), v.DebitCard) //done
		v1.GET("/list-card-transactions", server.authMiddleware.AuthenticatedMiddleware(), v.ListCardTransactions) //done
		v1.GET("/get-card-transaction-status", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardTransactionStatus) //done
		v1.POST("/withdraw-card", server.authMiddleware.AuthenticatedMiddleware(), v.WithdrawCard) //not done
	}
	v1admin := server.router.Group("/api/v1/admin/cards")
	v1admin.Use(server.authMiddleware.AuthenticatedMiddleware())
	{
		v1admin.POST("/fund-issuing-wallet", v.FundIssuingWallet) //done
	}

}

// FundIssuingWallet godoc
// @Summary Fund issuing wallet [admin]
// @Description Fund the issuing wallet with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body bridgecards.FundIssuingWalletRequest true "Issuing wallet funding parameters"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/admin/cards/fund-issuing-wallet [post]
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

// CreateCard godoc
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

// RegisterCardHolder godoc [Replace existing KYC with this]
// @Summary Register cardholder
// @Description Register a new cardholder with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body bridgecards.CreateCardHolderRequest true "Cardholder registration parameters"
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

	var req *bridgecards.CreateCardHolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	response, err := v.virtualCardSvc.CreateCardHolder(c, int32(activeUser.UserID), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK,  response)
}

// GetCardBalance godoc
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

// FundCard godoc
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

// FreezeCard godoc
// @Summary Freeze virtual card
// @Description Freeze a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.FreezeCardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/freeze-card [post]
func (v *Virtualcard) FreezeCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	
	response, err := v.virtualCardSvc.FreezeCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// UnfreezeCard godoc
// @Summary Unfreeze virtual card
// @Description Unfreeze a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} bridgecards.FreezeCardResponse
// @Param card_id query string true "Card ID"
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/unfreeze-card [post]
func (v *Virtualcard) UnfreezeCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	
	response, err := v.virtualCardSvc.UnfreezeCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// UpdateCardPin godoc
// @Summary Update virtual card pin
// @Description Update the pin of a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body bridgecards.UpdateCardPinRequest true "Card pin update parameters"
// @Success 200 {object} bridgecards.CardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/update-card-pin [post]
func (v *Virtualcard) UpdateCardPin(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	var req bridgecards.UpdateCardPinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}
	
	response, err := v.virtualCardSvc.UpdateCardPin(c, req, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// DeleteCard godoc
// @Summary Delete virtual card
// @Description Delete a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.CardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/delete-card [post]
func (v *Virtualcard) DeleteCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	
	response, err := v.virtualCardSvc.DeleteCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// DebitCard godoc
// @Summary Debit virtual card
// @Description Debit a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.DebitCardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/debit-card [patch]
func (v *Virtualcard) DebitCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	
	response, err := v.virtualCardSvc.DebitCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// ListCards godoc
// @Summary List virtual cards
// @Description List all virtual cards for a cardholder with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param cardholder_id query string true "Cardholder ID"
// @Success 200 {object} bridgecards.ListCardsResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/list-cards [get]
func (v *Virtualcard) ListCards(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cardholderIDfromUser, err := v.server.queries.GetBridgeCardCardholderByUserID(c, activeUser.UserID)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if cardholderIDfromUser.String == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing cardholder_id query parameter"))
		return
	}
	
	response, err := v.virtualCardSvc.ListCards(c, cardholderIDfromUser.String, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// ListCardTransactions godoc
// @Summary List virtual card transactions
// @Description List all virtual card transactions for a cardholder with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Param page query int true "Page number"
// @Success 200 {object} bridgecards.ListCardTransactionsResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/list-card-transactions [get]
func (v *Virtualcard) ListCardTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cardID := c.Query("card_id")
	page, _ := strconv.Atoi(c.Query("page"))
	// start := c.Query("start")
	// end := c.Query("end")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}

	var req bridgecards.ListCardTransactionsRequest
	req.CardID = cardID
	req.Page = page
	// req.StartDate = start
	// req.EndDate = end
	response, err := v.virtualCardSvc.ListCardTransactions(c, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// GetCardTransactionStatus godoc
// @Summary Get virtual card transaction status
// @Description Get the status of a virtual card transaction with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Param client_transaction_reference query string true "Client transaction reference"
// @Success 200 {object} bridgecards.GetCardTransactionStatusResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/get-card-transaction-status [get]
func (s *Virtualcard) GetCardTransactionStatus(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		s.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	clientTransactionReference := c.Query("client_transaction_reference")
	if clientTransactionReference == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing client_transaction_reference query parameter"))
		return
	}

	response, err := s.virtualCardSvc.GetCardTransactionStatus(c, cardID, clientTransactionReference, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

// WithdrawCard godoc
// @Summary Withdraw virtual card [not done]
// @Description Withdraw a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body bridgecards.WithdrawCardRequest true "Card withdrawal parameters"
// @Success 200 {object} bridgecards.WithdrawCardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/withdraw-card [post]
func (v *Virtualcard) WithdrawCard(c *gin.Context) {
	// activeUser, err := utils.GetActiveUser(c)
	// if err != nil {
	// 	v.server.logger.Error(err.Error())
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	var req bridgecards.WithdrawCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	req.TransactionReference = utils.NewTxRef("card_withdrawal")
	req.Currency = "USD"
	response, err := v.virtualCardSvc.WithdrawCard(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}	
	
	c.JSON(http.StatusOK, response)
}

// GetCardDetails godoc
// @Summary Get virtual card details
// @Description Get a virtual card details with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.GetCardDetailsResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/get-card-details [get]
func (v *Virtualcard) GetCardDetails(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	
	response, err := v.virtualCardSvc.GetCardDetails(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	
	c.JSON(http.StatusOK, response)
}

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
