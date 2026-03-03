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
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bridgecards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	virtualcard "github.com/SwiftFiat/SwiftFiat-Backend/services/virtual_card"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Virtualcard struct {
	server         *Server
	virtualCardSvc *virtualcard.Service
	audit          *audit.Service
}

func (v Virtualcard) router(server *Server) {
	v.server = server
	v.virtualCardSvc = server.virtualcard
	v.audit = server.auditService
	v1 := server.router.Group("/api/v1/cards")
	// v1.Use(server.authMiddleware.AuthenticatedMiddleware())
	{
		v1.POST("/create", server.authMiddleware.AuthenticatedMiddleware(), v.CreateCard)                                   //done
		v1.POST("/register-card-holder", server.authMiddleware.AuthenticatedMiddleware(), v.RegisterCardHolder)             //done
		v1.POST("/webhook", v.Webhook)                                                                                      //done
		v1.GET("/get-card-balance", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardBalance)                      // done
		v1.POST("/fund-card", server.authMiddleware.AuthenticatedMiddleware(), v.FundCard)                                  //done
		v1.POST("/freeze-card", server.authMiddleware.AuthenticatedMiddleware(), v.FreezeCard)                              //done
		v1.POST("/unfreeze-card", server.authMiddleware.AuthenticatedMiddleware(), v.UnfreezeCard)                          //done
		v1.POST("/update-card-pin", server.authMiddleware.AuthenticatedMiddleware(), v.UpdateCardPin)                       //done
		v1.DELETE("/delete-card", server.authMiddleware.AuthenticatedMiddleware(), v.DeleteCard)                            //done
		v1.GET("/list-cards", server.authMiddleware.AuthenticatedMiddleware(), v.ListCards)                                 //done
		v1.GET("/get-card-details", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardDetails)                      //done
		v1.PATCH("/debit-card", server.authMiddleware.AuthenticatedMiddleware(), v.DebitCard)                               //done
		v1.GET("/list-card-transactions", server.authMiddleware.AuthenticatedMiddleware(), v.ListCardTransactions)          //done
		v1.GET("/get-card-transaction-status", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardTransactionStatus) //done
		v1.POST("/withdraw-card", server.authMiddleware.AuthenticatedMiddleware(), v.WithdrawCard)
		v1.GET("/admin/get-card-plans", server.authMiddleware.AuthenticatedMiddleware(), v.ListCardPlans)
		v1.POST("/admin/create-card-plan", server.authMiddleware.AuthenticatedMiddleware(), v.createCardPlan)
		v1.GET("/get-card-plan-by-id", server.authMiddleware.AuthenticatedMiddleware(), v.GetCardPlanById)
		v1.GET("/get-card", server.authMiddleware.AuthenticatedMiddleware(), v.GetVirtualCard)
		v1.GET("/get-user-cards", server.authMiddleware.AuthenticatedMiddleware(), v.GetUserCards)
		v1.POST("/admin/fund-issuing-wallet", server.authMiddleware.AuthenticatedMiddleware(), v.FundIssuingWallet)          //done
		v1.GET("/admin/get-total-cards", server.authMiddleware.AuthenticatedMiddleware(), v.GetTotalCards)                   //one
		v1.GET("/admin/get-total-cards-by-status", server.authMiddleware.AuthenticatedMiddleware(), v.GetTotalCardsByStatus) //done
		// v1.PUT("/admin/update-card-plan/:plan_id", server.authMiddleware.AuthenticatedMiddleware(), v.UpdateCardPlan)                           //done
		v1.DELETE("/admin/delete-card-plan", server.authMiddleware.AuthenticatedMiddleware(), v.DeleteCardPlan)                        //done
		v1.POST("/admin/freeze-card", server.authMiddleware.AuthenticatedMiddleware(), v.AdminFreezeCard)                              //done
		v1.POST("/admin/unfreeze-card", server.authMiddleware.AuthenticatedMiddleware(), v.AdminUnfreezeCard)                          //done
		v1.DELETE("/admin/delete-card", server.authMiddleware.AuthenticatedMiddleware(), v.AdminDeleteCard)                            //done
		v1.POST("/admin/update-card-plan/:plan_id", server.authMiddleware.AuthenticatedMiddleware(), v.AdminUpdateCardPlan)            //done
		v1.GET("/admin/get-issuing-wallet-balance", server.authMiddleware.AuthenticatedMiddleware(), v.GetIssuingWalletBalance)        //done
		v1.GET("/admin/get-all-issued-cards", server.authMiddleware.AuthenticatedMiddleware(), v.GetAllIssuedCards)                    //done
		v1.GET("/admin/list-card-transactions-by-user", server.authMiddleware.AuthenticatedMiddleware(), v.ListCardTransactionsByUser) //done
		
	}

}

type UpdateCardPlanParams struct {
	Name                  *string `json:"name"`
	Description           *string `json:"description"`
	CreationFee           *string `json:"creation_fee"`
	MonthlyMaintenanceFee *string `json:"monthly_maintenance_fee"`
	MonthlySpendingLimit  *string `json:"monthly_spending_limit"`
	TransactionLimit      *string `json:"transaction_limit"`
	DailySpendingLimit    *string `json:"daily_spending_limit"`
	IsActive              *bool   `json:"is_active"`
	CardLimit             *string `json:"card_limit"`
}

// AdminUpdateCardPlan godoc
// @Summary Update card plan
// @Description Update card plan
// @Tags Cards
// @Accept json
// @Produce json
// @Param plan_id path int true "Card plan ID"
// @Param request body UpdateCardPlanRequest true "Update card plan request"
// @Success 200 {object} CardPlanResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/update-card-plan/{plan_id} [post]
func (v *Virtualcard) AdminUpdateCardPlan(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	id, err := strconv.Atoi(c.Param("plan_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid id"))
		return
	}

	var req UpdateCardPlanParams
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	oldCardPlan, err := v.server.queries.GetCardPlan(c, int64(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	params := db.UpdateCardPlanParams{
		ID: int64(id),

		Name: sql.NullString{
			String: derefString(req.Name),
			Valid:  req.Name != nil,
		},
		Description: sql.NullString{
			String: derefString(req.Description),
			Valid:  req.Description != nil,
		},
		CreationFee: sql.NullString{
			String: derefString(req.CreationFee),
			Valid:  req.CreationFee != nil,
		},
		MonthlyMaintenanceFee: sql.NullString{
			String: derefString(req.MonthlyMaintenanceFee),
			Valid:  req.MonthlyMaintenanceFee != nil,
		},
		MonthlySpendingLimit: sql.NullString{
			String: derefString(req.MonthlySpendingLimit),
			Valid:  req.MonthlySpendingLimit != nil,
		},
		TransactionLimit: sql.NullString{
			String: derefString(req.TransactionLimit),
			Valid:  req.TransactionLimit != nil,
		},
		DailySpendingLimit: sql.NullString{
			String: derefString(req.DailySpendingLimit),
			Valid:  req.DailySpendingLimit != nil,
		},
		CardLimit: sql.NullString{
			String: derefString(req.CardLimit),
			Valid:  req.CardLimit != nil,
		},
		IsActive: sql.NullBool{
			Bool:  derefBool(req.IsActive),
			Valid: req.IsActive != nil,
		},
	}

	plan, err := v.server.queries.UpdateCardPlan(c, params)
	if err != nil {
		errmsg := err.Error()
		// audit log
		entry := audit.NewLog(
			c,
			audit.CategoryCard,
			audit.EventUpdateCardPlan,
			"",
			fmt.Sprintf("%d updated the card plan %s", activeUser.UserID, plan.Name),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errmsg,
		)
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventUpdateCardPlan,
		"",
		fmt.Sprintf("%d updated the card plan %s", activeUser.UserID, plan.Name),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.OldValues = map[string]any{
		"name":                    oldCardPlan.Name,
		"description":             oldCardPlan.Description,
		"creation_fee":            oldCardPlan.CreationFee,
		"monthly_maintenance_fee": oldCardPlan.MonthlyMaintenanceFee,
		"monthly_spending_limit":  oldCardPlan.MonthlySpendingLimit,
		"transaction_limit":       oldCardPlan.TransactionLimit,
		"daily_spending_limit":    oldCardPlan.DailySpendingLimit,
		"is_active":               oldCardPlan.IsActive,
		"card_limit":              oldCardPlan.CardLimit,
	}
	entry.NewValues = map[string]any{
		"name":                    plan.Name,
		"description":             plan.Description,
		"creation_fee":            plan.CreationFee,
		"monthly_maintenance_fee": plan.MonthlyMaintenanceFee,
		"monthly_spending_limit":  plan.MonthlySpendingLimit,
		"transaction_limit":       plan.TransactionLimit,
		"daily_spending_limit":    plan.DailySpendingLimit,
		"is_active":               plan.IsActive,
		"card_limit":              plan.CardLimit,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Card plan updated successfully", mapCardPlanToCardPlanResponse(plan)))
}

// FundIssuingWallet godoc
// @Summary Fund issuing wallet [admin - sandbox only]
// @Description Fund the issuing wallet with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param request body bridgecards.FundIssuingWalletRequest true "Issuing wallet funding parameters"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/fund-issuing-wallet [post]
func (v *Virtualcard) FundIssuingWallet(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	var req bridgecards.FundIssuingWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	message := v.virtualCardSvc.FundIssuingWallet(c, req)

	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventFundIssuingWallet,
		"",
		fmt.Sprintf("%d funded the issuing wallet with %d", activeUser.UserID, req.Amount),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)
	c.JSON(http.StatusOK, basemodels.NewSuccess(message, nil))
}

// GetIssuingWalletBalance godoc
// @Summary Get issuing wallet balance [admin - sandbox only]
// @Description Get the balance of the issuing wallet
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/issuing-wallet-balance [get]
func (v *Virtualcard) GetIssuingWalletBalance(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	balance, err := v.virtualCardSvc.GetIssuingWalletBalance(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, balance)
}

// GetAllIssuedCards godoc
// @Summary Get all issued cards [admin]
// @Description Get all issued cards
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/get-all-issued-cards [get]
func (v *Virtualcard) GetAllIssuedCards(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	cards, err := v.server.queries.GetAllVirtualCards(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, cards)
}

type CreateCardRequest struct {
	CardPlanID    int64  `json:"card_plan_id" binding:"required"`
	CardName      string `json:"card_name" binding:"required"`
	CardColor     string `json:"color" binding:"omitempty"`
	CardHolderID  string `json:"card_holder_id" binding:"required"`
	FundingAmount string `json:"funding_amount" binding:"required"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
	IdempotencyKey2 string `json:"idempotency_key_2" binding:"required"`
}

// CreateCard godoc
// @Summary Create virtual card
// @Tags Cards
// @Description Create a new USD virtual card with BridgeCard
// @Accept json
// @Produce json
// @Success 201 {object} bridgecards.CreateCardResponse
// @Param request body CreateCardRequest true "Card creation parameters"
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
		UserID:        activeUser.UserID,
		CardPlanID:    req.CardPlanID,
		CardName:      req.CardName,
		CardColor:     req.CardColor,
		CardHolderID:  req.CardHolderID,
		FundingAmount: req.FundingAmount,
		IdempotencyKey: req.IdempotencyKey,
		IdempotencyKey2: req.IdempotencyKey2,
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

	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventCreateCard,
		"",
		fmt.Sprintf("%d created a new card", activeUser.UserID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.NewValues = map[string]interface{}{
		"card_id":        result.Data.CardID,
		"currency":       result.Data.Currency,
		"card_name":      req.CardName,
		"card_color":     req.CardColor,
		"card_holder_id": req.CardHolderID,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusCreated, basemodels.NewSuccess("", result))
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
// @Router /api/v1/cards/register-card-holder [post]
func (v *Virtualcard) RegisterCardHolder(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req bridgecards.CreateCardHolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	response, err := v.virtualCardSvc.CreateCardHolder(c, int32(activeUser.UserID), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventRegisterCardHolder,
		"",
		fmt.Sprintf("%d created a new cardholder", activeUser.UserID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.NewValues = map[string]interface{}{
		"cardholder_id": response.Data.CardHolderID,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusOK, response)
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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	response, err := v.virtualCardSvc.GetCardBalance(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, response)
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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, req.CardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	response, err := v.virtualCardSvc.FreezeCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, response)
}

// AdminFreezeCard godoc
// @Summary Freeze virtual card
// @Description Freeze a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.FreezeCardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/freeze-card [post]
func (v *Virtualcard) AdminFreezeCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		errMsg := err.Error()
		// audit log
		entry := audit.NewLog(
			c,
			audit.CategoryCard,
			audit.EventFreezeCard,
			card.ID.String(),
			fmt.Sprintf("admin %d froze card %s", activeUser.UserID, cardID),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	response, err := v.virtualCardSvc.AdminFreezeCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventFreezeCard,
		card.ID.String(),
		fmt.Sprintf("admin %d froze card %s", activeUser.UserID, cardID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)

	c.JSON(http.StatusOK, response)
}

// AdminUnfreezeCard godoc
// @Summary Unfreeze virtual card
// @Description Unfreeze a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} bridgecards.FreezeCardResponse
// @Param card_id query string true "Card ID"
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/unfreeze-card [post]
func (v *Virtualcard) AdminUnfreezeCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		errMsg := err.Error()
		// audit log
		entry := audit.NewLog(
			c,
			audit.CategoryCard,
			audit.EventUnfreezeCard,
			card.ID.String(),
			fmt.Sprintf("admin %d unfroze card %s", activeUser.UserID, cardID),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	response, err := v.virtualCardSvc.AdminUnfreezeCard(c, cardID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventUnfreezeCard,
		card.ID.String(),
		fmt.Sprintf("admin %d unfroze card %s", activeUser.UserID, cardID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)

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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, req.CardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
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

	cardUUID, err := uuid.Parse(cardID)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid card_id format"))
		return
	}

	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	response, err := v.virtualCardSvc.DeleteCard(c, cardUUID, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, response)
}

// AdminDeleteCard godoc
// @Summary Delete virtual card
// @Description Delete a virtual card with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param card_id query string true "Card ID"
// @Success 200 {object} bridgecards.CardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/delete-card [delete]
func (v *Virtualcard) AdminDeleteCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// if activeUser.Role == models.USER {
	// 	c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	cardID := c.Query("card_id")
	if cardID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing card_id query parameter"))
		return
	}
	cardUUID := uuid.MustParse(cardID)

	response, err := v.virtualCardSvc.AdminDeleteCard(c, cardUUID, activeUser.UserID)
	if err != nil {
		errMsg := err.Error()
		// audit log
		entry := audit.NewLog(
			c,
			audit.CategoryCard,
			audit.EventDeleteCard,
			cardUUID.String(),
			fmt.Sprintf("admin deleted card %s", cardUUID.String()),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventDeleteCard,
		cardUUID.String(),
		fmt.Sprintf("admin deleted card %s", cardUUID.String()),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	v.audit.Log(entry)
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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
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
// @Success 200 {object} []GetUserCardsRowResponse
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
		c.JSON(http.StatusBadRequest, basemodels.NewError("You dont have a cardholder ID"))
		return
	}

	cards, err := v.virtualCardSvc.ListCardsFromDB(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var UserCardResponse []GetUserCardsRowResponse
	for _, card := range cards {
		UserCardResponse = append(UserCardResponse, GetUserCardsRowResponse{
			ID:               card.ID,
			BridgecardCardID: card.BridgecardCardID,
			UserID:           card.UserID,
			CardPlanID:       card.CardPlanID,
			CardName:         card.CardName,
			CardColor:        &card.CardColor.String,
			Currency:         card.Currency,
			Status:           card.Status,
			CreatedAt:        card.CreatedAt,
			UpdatedAt:        card.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", UserCardResponse))
}

// ListCardTransactions godoc
// @Summary List virtual card transactions
// @Description List all virtual card transactions for a cardholder with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param limit query int true "Limit"
// @Param offset query int true "Offset"
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

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing limit query parameter"))
		return
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing offset query parameter"))
		return
	}

	response, err := v.server.queries.GetCardTransactionsByUser(c, db.GetCardTransactionsByUserParams{
		UserID: activeUser.UserID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, response)
}

// ListCardTransactionsByUser godoc
// @Summary List virtual card transactions by user
// @Description List all virtual card transactions for a user with BridgeCard
// @Tags Cards
// @Accept json
// @Produce json
// @Param user_id query int true "User ID"
// @Param limit query int true "Limit"
// @Param offset query int true "Offset"
// @Success 200 {object} bridgecards.ListCardTransactionsResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/list-card-transactions-by-user [get]
func (v *Virtualcard) ListCardTransactionsByUser(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized access"))
		return
	}

	user_id, err := strconv.Atoi(c.Query("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing user_id query parameter"))
		return
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing limit query parameter"))
		return
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing offset query parameter"))
		return
	}

	response, err := v.server.queries.GetCardTransactionsByUser(c, db.GetCardTransactionsByUserParams{
		UserID: int64(user_id),
		Limit:  int32(limit),
		Offset: int32(offset),
	})
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
func (v *Virtualcard) GetCardTransactionStatus(c *gin.Context) {
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
	clientTransactionReference := c.Query("client_transaction_reference")
	if clientTransactionReference == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("missing client_transaction_reference query parameter"))
		return
	}

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	response, err := v.virtualCardSvc.GetCardTransactionStatus(c, cardID, clientTransactionReference, activeUser.UserID)
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
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req bridgecards.WithdrawCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	req.TransactionReference = utils.NewTxRef("card_withdrawal")
	req.Currency = "USD"

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, req.CardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}
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

	card, err := v.server.queries.GetVirtualCardByBridgeCardID(c, cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
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

// GetTotalCards godoc
// @Summary Get total number of virtual cards
// @Description Get total number of virtual cards
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} int
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/get-total-cards [get]
func (v *Virtualcard) GetTotalCards(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	totalCards, err := v.server.queries.GetTotalCards(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total cards retrieved successfully", totalCards))
}

// GetTotalCardsByStatus godoc
// @Summary Get total number of virtual cards by status
// @Description Get total number of virtual cards by status
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} db.GetTotalCardsByStatusRow
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/get-total-cards-by-status [get]
func (v *Virtualcard) GetTotalCardsByStatus(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	totalCards, err := v.server.queries.GetTotalCardsByStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Total cards retrieved successfully", totalCards))
}

// GetCardPlanById godoc
// @Summary Get card plan by id
// @Description Get card plan by id
// @Tags Cards
// @Accept json
// @Produce json
// @Param plan_id query int true "Plan ID"
// @Success 200 {object} CardPlanResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/get-card-plan-by-id [get]
func (v *Virtualcard) GetCardPlanById(c *gin.Context) {
	planID, err := strconv.Atoi(c.Query("plan_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	plan, err := v.server.queries.GetCardPlan(c.Request.Context(), int64(planID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Card plan retrieved successfully", mapCardPlanToCardPlanResponse(plan)))
}

type CreateCardPlanRequest struct {
	Name                     string  `json:"name" binding:"required"`
	Description              *string `json:"description"`
	CreationFee              string  `json:"creation_fee" binding:"required"`
	MonthlyMaintenanceFee    string  `json:"monthly_maintenance_fee" binding:"required"`
	MonthlySpendingLimit     string  `json:"monthly_spending_limit" binding:"required"`
	TransactionLimit         string  `json:"transaction_limit" binding:"required"`
	DailySpendingLimit       *string `json:"daily_spending_limit"`
	MaxCardsPerUser          int32   `json:"max_cards_per_user" binding:"required"`
	CardLimit                *string `json:"card_limit" binding:"required" enum:"5000,10000"`
	FailedTxCountBeforeBlock *int32  `json:"failed_tx_count_before_block"`
}

// CreateCardPlan godoc
// @Summary Create card plan
// @Description Create card plan
// @Tags Cards
// @Accept json
// @Produce json
// @Param create_card_plan body CreateCardPlanRequest true "Create card plan"
// @Success 200 {object} CardPlanResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/create-card-plan [post]
func (v *Virtualcard) createCardPlan(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req CreateCardPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	params := db.CreateCardPlanParams{
		Name:                     req.Name,
		Description:              sql.NullString{String: *req.Description, Valid: req.Description != nil},
		CreationFee:              req.CreationFee,
		MonthlyMaintenanceFee:    req.MonthlyMaintenanceFee,
		MonthlySpendingLimit:     req.MonthlySpendingLimit,
		TransactionLimit:         req.TransactionLimit,
		DailySpendingLimit:       sql.NullString{String: *req.DailySpendingLimit, Valid: req.DailySpendingLimit != nil},
		MaxCardsPerUser:          req.MaxCardsPerUser,
		CardLimit:                sql.NullString{String: *req.CardLimit, Valid: req.CardLimit != nil},
		FailedTxCountBeforeBlock: sql.NullInt32{Int32: *req.FailedTxCountBeforeBlock, Valid: req.FailedTxCountBeforeBlock != nil},
	}

	plan, err := v.server.queries.CreateCardPlan(c.Request.Context(), params)
	if err != nil {
		errMsg := err.Error()
		entry := audit.NewLog(
			c,
			audit.CategoryCard,
			audit.EventCreateCardPlan,
			fmt.Sprint(plan.ID),
			fmt.Sprintf("Card plan %s created successfully by admin %d", plan.Name, activeUser.UserID),
			&activeUser.UserID,
			activeUser.Role,
			false,
			&errMsg,
		)
		entry.NewValues = map[string]any{
			"name":                         plan.Name,
			"description":                  plan.Description.String,
			"creation_fee":                 plan.CreationFee,
			"monthly_maintenance_fee":      plan.MonthlyMaintenanceFee,
			"monthly_spending_limit":       plan.MonthlySpendingLimit,
			"transaction_limit":            plan.TransactionLimit,
			"daily_spending_limit":         plan.DailySpendingLimit.String,
			"max_cards_per_user":           plan.MaxCardsPerUser,
			"card_limit":                   plan.CardLimit.String,
			"failed_tx_count_before_block": plan.FailedTxCountBeforeBlock.Int32,
		}
		v.audit.Log(entry)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventCreateCardPlan,
		fmt.Sprint(plan.ID),
		fmt.Sprintf("Card plan %s created successfully by admin %d", plan.Name, activeUser.UserID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.NewValues = map[string]any{
		"name":                         plan.Name,
		"description":                  plan.Description.String,
		"creation_fee":                 plan.CreationFee,
		"monthly_maintenance_fee":      plan.MonthlyMaintenanceFee,
		"monthly_spending_limit":       plan.MonthlySpendingLimit,
		"transaction_limit":            plan.TransactionLimit,
		"daily_spending_limit":         plan.DailySpendingLimit.String,
		"max_cards_per_user":           plan.MaxCardsPerUser,
		"card_limit":                   plan.CardLimit.String,
		"failed_tx_count_before_block": plan.FailedTxCountBeforeBlock.Int32,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Card plan created successfully", mapCardPlanToCardPlanResponse(plan)))
}

// ListCardPlans godoc
// @Summary List card plans
// @Description List card plans
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} []CardPlanResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/get-card-plans [get]
func (v *Virtualcard) ListCardPlans(c *gin.Context) {
	plans, err := v.server.queries.ListCardPlans(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var cardPlans []CardPlanResponse
	for _, plan := range plans {
		cardPlans = append(cardPlans, mapCardPlanToCardPlanResponse(plan))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Card plans retrieved successfully", cardPlans))
}

type UpdateCardPlanRequest struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	MonthlySpendingLimit  string `json:"monthly_spending_limit"`
	MonthlyMaintenanceFee string `json:"monthly_maintenance_fee"`
	TransactionLimit      string `json:"transaction_limit"`
	DailySpendingLimit    string `json:"daily_spending_limit"`
	IsActive              bool   `json:"is_active"`
	CardLimit             string `json:"card_limit"`
}

// DeleteCardPlan godoc
// @Summary Delete card plan
// @Description Delete card plan
// @Tags Cards
// @Accept json
// @Produce json
// @Param plan_id query int true "Plan ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/admin/delete/card-plan [delete]
func (v *Virtualcard) DeleteCardPlan(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cardPlanID, err := strconv.Atoi(c.Query("plan_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	plan, err := v.server.queries.GetCardPlan(c.Request.Context(), int64(cardPlanID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	err = v.server.queries.DeleteCardPlan(c.Request.Context(), int64(cardPlanID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	entry := audit.NewLog(
		c,
		audit.CategoryCard,
		audit.EventDeleteCardPlan,
		fmt.Sprint(cardPlanID),
		fmt.Sprintf("user %d deleted card plan %s", activeUser.UserID, plan.Name),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	entry.OldValues = map[string]any{
		"name":                    plan.Name,
		"description":             plan.Description,
		"monthly_spending_limit":  plan.MonthlySpendingLimit,
		"monthly_maintenance_fee": plan.MonthlyMaintenanceFee,
		"transaction_limit":       plan.TransactionLimit,
		"daily_spending_limit":    plan.DailySpendingLimit,
		"is_active":               plan.IsActive,
		"card_limit":              plan.CardLimit,
	}
	v.audit.Log(entry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Card plan deleted successfully", nil))
}

// GetVirtualCard godoc
// @Summary Get virtual card
// @Description Get virtual card
// @Tags Cards
// @Accept json
// @Produce json
// @Param id query string true "Virtual Card ID"
// @Success 200 {object} VirtualCardResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/get-card [get]
func (v *Virtualcard) GetVirtualCard(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cardID, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	card, err := v.server.queries.GetVirtualCard(c.Request.Context(), cardID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if card.UserID != activeUser.UserID {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("Virtual card retrieved successfully", mapVirtualCardToResponse(&card)))
}

// GetUserCards godoc
// @Summary Get user cards
// @Description Get user cards
// @Tags Cards
// @Accept json
// @Produce json
// @Success 200 {object} []GetUserCardsRowResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/cards/get-user-cards [get]
func (v *Virtualcard) GetUserCards(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		v.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	cards, err := v.server.queries.GetUserCards(c.Request.Context(), activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var response []GetUserCardsRowResponse
	for _, card := range cards {
		response = append(response, mapGetUserCardsRowToResponse(card))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("User cards retrieved successfully", response))
}

type GetUserCardsRowResponse struct {
	ID                      uuid.UUID  `json:"id"`
	UserID                  int64      `json:"user_id"`
	CardPlanID              int64      `json:"card_plan_id"`
	BridgecardCardID        string     `json:"bridgecard_card_id"`
	CardName                string     `json:"card_name"`
	CardColor               *string    `json:"card_color"`
	Currency                string     `json:"currency"`
	CurrentMonthSpend       int64      `json:"current_month_spend"`
	CurrentDaySpend         int64      `json:"current_day_spend"`
	SpendingMonth           *string    `json:"spending_month"`
	SpendingDay             *string    `json:"spending_day"`
	Status                  string     `json:"status"`
	StatusReason            *string    `json:"status_reason"`
	AutoTopupEnabled        bool       `json:"auto_topup_enabled"`
	AutoTopupThreshold      *int64     `json:"auto_topup_threshold"`
	AutoTopupAmount         *int64     `json:"auto_topup_amount"`
	AutoTopupSourceWalletID *uuid.UUID `json:"auto_topup_source_wallet_id"`
	NextBillingDate         *time.Time `json:"next_billing_date"`
	LastBillingDate         *time.Time `json:"last_billing_date"`
	LastTransactionAt       *time.Time `json:"last_transaction_at"`
	TotalTransactionsCount  int64      `json:"total_transactions_count"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	TerminatedAt            *time.Time `json:"terminated_at"`
	PlanName                string     `json:"plan_name"`
	MonthlySpendingLimit    string     `json:"monthly_spending_limit"`
	TransactionLimit        string     `json:"transaction_limit"`
}

func mapGetUserCardsRowToResponse(row db.GetUserCardsRow) GetUserCardsRowResponse {
	return GetUserCardsRowResponse{
		ID:                      row.ID,
		UserID:                  row.UserID,
		CardPlanID:              row.CardPlanID,
		BridgecardCardID:        row.BridgecardCardID,
		CardName:                row.CardName,
		CardColor:               &row.CardColor.String,
		Currency:                row.Currency,
		CurrentMonthSpend:       row.CurrentMonthSpend.Int64,
		CurrentDaySpend:         row.CurrentDaySpend.Int64,
		SpendingMonth:           &row.SpendingMonth.String,
		SpendingDay:             &row.SpendingDay.String,
		Status:                  row.Status,
		StatusReason:            &row.StatusReason.String,
		AutoTopupEnabled:        row.AutoTopupEnabled,
		AutoTopupThreshold:      &row.AutoTopupThreshold.Int64,
		AutoTopupAmount:         &row.AutoTopupAmount.Int64,
		AutoTopupSourceWalletID: &row.AutoTopupSourceWalletID.UUID,
		NextBillingDate:         &row.NextBillingDate.Time,
		LastBillingDate:         &row.LastBillingDate.Time,
		LastTransactionAt:       &row.LastTransactionAt.Time,
		TotalTransactionsCount:  row.TotalTransactionsCount,
		CreatedAt:               row.CreatedAt,
		UpdatedAt:               row.UpdatedAt,
		TerminatedAt:            &row.TerminatedAt.Time,
		PlanName:                row.PlanName,
		MonthlySpendingLimit:    row.MonthlySpendingLimit,
		TransactionLimit:        row.TransactionLimit,
	}
}

type VirtualCardResponse struct {
	ID                      uuid.UUID  `json:"id"`
	UserID                  int64      `json:"user_id"`
	CardPlanID              int64      `json:"card_plan_id"`
	BridgecardCardID        string     `json:"bridgecard_card_id"`
	CardName                string     `json:"card_name"`
	CardColor               *string    `json:"card_color"`
	Currency                string     `json:"currency"`
	CurrentMonthSpendCents  int64      `json:"current_month_spend_cents"`
	CurrentDaySpendCents    int64      `json:"current_day_spend_cents"`
	SpendingMonth           *string    `json:"spending_month"`
	SpendingDay             *string    `json:"spending_day"`
	Status                  string     `json:"status"`
	StatusReason            *string    `json:"status_reason"`
	AutoTopupEnabled        bool       `json:"auto_topup_enabled"`
	AutoTopupThresholdCents *int64     `json:"auto_topup_threshold_cents"`
	AutoTopupAmountCents    *int64     `json:"auto_topup_amount_cents"`
	AutoTopupSourceWalletID *uuid.UUID `json:"auto_topup_source_wallet_id"`
	NextBillingDate         *time.Time `json:"next_billing_date"`
	LastBillingDate         *time.Time `json:"last_billing_date"`
	LastTransactionAt       *time.Time `json:"last_transaction_at"`
	TotalTransactionsCount  int64      `json:"total_transactions_count"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	TerminatedAt            *time.Time `json:"terminated_at"`
}

func mapVirtualCardToResponse(card *db.VirtualCard) VirtualCardResponse {
	return VirtualCardResponse{
		ID:                      card.ID,
		UserID:                  card.UserID,
		CardPlanID:              card.CardPlanID,
		BridgecardCardID:        card.BridgecardCardID,
		CardName:                card.CardName,
		CardColor:               &card.CardColor.String,
		Currency:                card.Currency,
		CurrentMonthSpendCents:  card.CurrentMonthSpend.Int64,
		CurrentDaySpendCents:    card.CurrentDaySpend.Int64,
		SpendingMonth:           &card.SpendingMonth.String,
		SpendingDay:             &card.SpendingDay.String,
		Status:                  card.Status,
		StatusReason:            &card.StatusReason.String,
		AutoTopupEnabled:        card.AutoTopupEnabled,
		AutoTopupThresholdCents: &card.AutoTopupThreshold.Int64,
		AutoTopupAmountCents:    &card.AutoTopupAmount.Int64,
		AutoTopupSourceWalletID: &card.AutoTopupSourceWalletID.UUID,
		NextBillingDate:         &card.NextBillingDate.Time,
		LastBillingDate:         &card.LastBillingDate.Time,
		LastTransactionAt:       &card.LastTransactionAt.Time,
		TotalTransactionsCount:  card.TotalTransactionsCount,
		CreatedAt:               card.CreatedAt,
		UpdatedAt:               card.UpdatedAt,
		TerminatedAt:            &card.TerminatedAt.Time,
	}
}

type CardPlanResponse struct {
	ID                       int64      `json:"id"`
	Name                     string     `json:"name"`
	Description              *string    `json:"description"`
	IsActive                 bool       `json:"is_active"`
	CreationFee              string     `json:"creation_fee"`
	MonthlyMaintenanceFee    string     `json:"monthly_maintenance_fee"`
	MonthlySpendingLimit     string     `json:"monthly_spending_limit"`
	TransactionLimit         string     `json:"transaction_limit"`
	DailySpendingLimit       *string    `json:"daily_spending_limit"`
	CardLimit                *string    `json:"card_limit"`
	MaxCardsPerUser          int32      `json:"max_cards_per_user"`
	FailedTxCountBeforeBlock int32      `json:"failed_tx_count_before_block"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
	DeletedAt                *time.Time `json:"deleted_at"`
}

func mapCardPlanToCardPlanResponse(cardPlan db.CardPlan) CardPlanResponse {
	return CardPlanResponse{
		ID:                       cardPlan.ID,
		Name:                     cardPlan.Name,
		Description:              &cardPlan.Description.String,
		IsActive:                 cardPlan.IsActive,
		CreationFee:              cardPlan.CreationFee,
		MonthlyMaintenanceFee:    cardPlan.MonthlyMaintenanceFee,
		MonthlySpendingLimit:     cardPlan.MonthlySpendingLimit,
		TransactionLimit:         cardPlan.TransactionLimit,
		DailySpendingLimit:       &cardPlan.DailySpendingLimit.String,
		CardLimit:                &cardPlan.CardLimit.String,
		MaxCardsPerUser:          cardPlan.MaxCardsPerUser,
		FailedTxCountBeforeBlock: cardPlan.FailedTxCountBeforeBlock.Int32,
		CreatedAt:                cardPlan.CreatedAt,
		UpdatedAt:                cardPlan.UpdatedAt,
		DeletedAt:                &cardPlan.DeletedAt.Time,
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
