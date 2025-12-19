package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	vaultsavings "github.com/SwiftFiat/SwiftFiat-Backend/services/vault_savings"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ============================================================================
// VAULT HANDLER STRUCT
// ============================================================================

type Vault struct {
	server        *Server
	vaultService  *vaultsavings.VaultService
	yieldService  *vaultsavings.YieldService
	walletService *wallet.WalletService
	pushService   *service.PushNotificationService
	emailService  *service.Plunk
	audit         *audit.Service
}

func (v Vault) router(server *Server) {
	v.server = server
	v.walletService = v.server.walletService
	v.pushService = v.server.pushNotification
	v.emailService = v.server.emailService
	v.audit = v.server.auditService
	v.vaultService = v.server.vaultService
	v.yieldService = v.server.yieldService

	vaultGroup := server.router.Group("/api/v1/vault")
	vaultGroup.Use(v.server.authMiddleware.AuthenticatedMiddleware())
	{
		// Goal Management
		vaultGroup.POST("/goals", v.createGoal)
		vaultGroup.GET("/goals", v.listGoals)
		vaultGroup.GET("/goals/:id", v.getGoal)
		vaultGroup.PUT("/goals/:id", v.updateGoal)
		vaultGroup.DELETE("/goals/:id", v.deleteGoal)
		vaultGroup.GET("/summary", v.getSummary)
		vaultGroup.GET("/goals/:id/progress", v.getProgress)

		// Transactions
		vaultGroup.POST("/goals/:id/deposit", v.deposit)
		vaultGroup.POST("/goals/:id/withdraw", v.withdraw)
		vaultGroup.GET("/goals/:id/transactions", v.getVaultTransactions)
		vaultGroup.GET("/transactions", v.getAllTransactions)

		// Recurring Rules
		vaultGroup.PUT("/goals/:id/recurring", v.updateRecurringRule)
		vaultGroup.POST("/goals/:id/recurring/pause", v.pauseRecurring)
		vaultGroup.POST("/goals/:id/recurring/resume", v.resumeRecurring)

		// Admin Routes
		vaultGroup.GET("/admin/metrics", v.getAdminMetrics)
		vaultGroup.GET("/admin/scheduler/stats", v.getSchedulerStats)
		vaultGroup.POST("/admin/scheduler/trigger", v.triggerSchedulerNow)

		// Yield Routes (User)
		vaultGroup.GET("/goals/:id/yield-history", v.getYieldHistory)
		vaultGroup.GET("/goals/:id/yield-projection", v.getYieldProjection)
		vaultGroup.GET("/yield-summary", v.getYieldSummary)

		// Yield Routes (Admin)
		vaultGroup.GET("/admin/yield-configs", v.listYieldConfigs)
		vaultGroup.POST("/admin/yield-configs", v.createYieldConfig)
		vaultGroup.PUT("/admin/yield-configs/:id", v.updateYieldConfig)
		vaultGroup.POST("/admin/yield-configs/:id/deactivate", v.deactivateYieldConfig)
		vaultGroup.POST("/admin/process-yields-now", v.processYieldsNow)
		vaultGroup.GET("/admin/yield-scheduler/stats", v.getYieldSchedulerStats)
		vaultGroup.GET("/admin/goals", v.AdminListGoals)
	}
}

// ============================================================================
// CREATE GOAL
// ============================================================================

// createGoal godoc
// @Summary Create Vault Savings Goal
// @Description Create a new savings goal with optional recurring deposits
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param createGoalRequest body vaultsavings.CreateVaultGoalRequest true "Create Goal Request"
// @Success 201 {object} vaultsavings.VaultSavingResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals [post]
func (v *Vault) createGoal(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req vaultsavings.CreateVaultGoalRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// Validate amount
	if req.TargetAmount != "" {
		amount, err := decimal.NewFromString(req.TargetAmount)
		if err != nil || amount.LessThanOrEqual(decimal.Zero) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid target amount"))
			return
		}
	}

	// Create goal
	goal, err := v.vaultService.CreateVaultGoal(ctx.Request.Context(), req, activeUser.UserID, ctx.ClientIP(), ctx.Request.UserAgent())
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to create vault goal: %v", err))
		if errors.Is(err, vaultsavings.ErrInvalidCurrency) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid currency"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create savings goal"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventVaultCreated, "vault", goal.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Vault savings goal %s created by user %d", goal.ID.String(), activeUser.UserID)
	auditLog.OldValues = nil
	auditLog.NewValues = map[string]interface{}{
		"id":             goal.ID,
		"user_id":        goal.UserID,
		"name":           goal.VaultName,
		"target_amount":  goal.GoalAmount,
		"currency":       goal.Currency,
		"created_at":     time.Now(),
		"updated_at":     time.Now(),
		"status":         goal.Status,
		"recurring_rule": goal.RecurringRule,
	}
	v.audit.Log(auditLog)
	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("savings goal created successfully", goal))
}

// ============================================================================
// LIST GOALS
// ============================================================================

// listGoals godoc
// @Summary List User's Vault Goals
// @Description Get all vault savings goals for the authenticated user
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status (active, completed, paused)"
// @Success 200 {object} []vaultsavings.VaultSavingResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals [get]
func (v *Vault) listGoals(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	status := ctx.Query("status")

	var goals []db.VaultSaving
	if status == "active" {
		goals, err = v.vaultService.GetUserActiveVaults(ctx.Request.Context(), activeUser.UserID)
	} else {
		goals, err = v.vaultService.GetUserVaults(ctx.Request.Context(), activeUser.UserID)
	}

	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch vault goals: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch savings goals"))
		return
	}

	var resp []vaultsavings.VaultSavingResponse
	for _, goal := range goals {
		resp = append(resp, *vaultsavings.MapVaultSavingToResponse(&goal))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("savings goals fetched successfully", resp))
}

// AdminListGoals godoc
// @Summary List All Vault Goals
// @Description Get all vault savings goals for the authenticated user
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} []vaultsavings.VaultSavingResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/goals [get]
func (v *Vault) AdminListGoals(ctx *gin.Context) {
	// activeUser, err := utils.GetActiveUser(ctx)
	// if err != nil {
	// 	v.server.logger.Error(err.Error())
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }

	// if activeUser.Role == models.USER {
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
	// 	return
	// }
	goals, err := v.server.queries.GetAllVaultGoals(ctx.Request.Context())

	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch vault goals: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch savings goals"))
		return
	}

	var resp []vaultsavings.VaultSavingResponse
	for _, goal := range goals {
		resp = append(resp, *vaultsavings.MapVaultSavingToResponse(&goal))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("savings goals fetched successfully", resp))
}

// ============================================================================
// GET GOAL
// ============================================================================

// getGoal godoc
// @Summary Get Vault Goal Details
// @Description Get details of a specific vault savings goal
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Success 200 {object} vaultsavings.VaultSavingResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id} [get]
func (v *Vault) getGoal(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		if errors.Is(err, vaultsavings.ErrVaultNotFound) {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
			return
		}
		v.server.logger.Error(fmt.Sprintf("failed to fetch vault goal: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch savings goal"))
		return
	}

	// Verify ownership
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("savings goal retrieved successfully", goal))
}

// ============================================================================
// GET SUMMARY
// ============================================================================

// getSummary godoc
// @Summary Get Vault Summary
// @Description Get summary of all vaults for the authenticated user
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=db.GetUserVaultsSummaryRow}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/summary [get]
func (v *Vault) getSummary(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	summary, err := v.vaultService.GetUserVaultSummary(ctx.Request.Context(), activeUser.UserID)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch vault summary: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch summary"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("summary retrieved successfully", summary))
}

// ============================================================================
// GET PROGRESS
// ============================================================================

// getProgress godoc
// @Summary Get Goal Progress
// @Description Get progress details for a specific vault goal
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Success 200 {object} vaultsavings.GetVaultGoalProgressResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/progress [get]
func (v *Vault) getProgress(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	// Verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	progress, err := v.vaultService.GetVaultProgress(ctx.Request.Context(), vaultID)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch progress: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch progress"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("progress retrieved successfully", vaultsavings.MapGetVaultGoalProgressRowToReponse(progress)))
}

// ============================================================================
// DEPOSIT
// ============================================================================

// deposit godoc
// @Summary Deposit to Vault
// @Description Deposit funds from wallet to vault savings goal
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Param depositRequest body object{from_wallet_id=string,amount=string,description=string} true "Deposit Request"
// @Success 200 {object} vaultsavings.DepositResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/deposit [post]
func (v *Vault) deposit(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	var req struct {
		FromWalletID string `json:"from_wallet_id" binding:"required"`
		Amount       string `json:"amount" binding:"required"`
		Description  string `json:"description"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// Parse wallet ID
	walletID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid wallet ID"))
		return
	}

	// TODO: verify wallet belongs to the user

	// Validate amount
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid amount"))
		return
	}

	// Get vault to verify ownership and currency
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}

	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// TODO: check if deposit currency == vault currency

	// Create deposit request
	depositReq := vaultsavings.DepositRequest{
		UserID:       activeUser.UserID,
		VaultID:      vaultID,
		FromWalletID: walletID,
		Amount:       req.Amount,
		Currency:     goal.Currency,
		Description:  req.Description,
	}

	tx, err := v.vaultService.Deposit(ctx.Request.Context(), depositReq)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to process deposit: %v", err))
		if errors.Is(err, vaultsavings.ErrInsufficientBalance) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("insufficient wallet balance"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventSavingsDeposited, "vault", goal.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Deposit of %s %s to vault %s initiated by user %d", amount.String(), goal.Currency, goal.ID.String(), activeUser.UserID)
	auditLog.OldValues = nil
	auditLog.NewValues = map[string]any{
		"transaction_id": tx.ID,
		"from_wallet_id": walletID,
		"user_id":        activeUser.UserID,
		"vault_id":       goal.ID,
		"amount":         amount.String(),
		"currency":       goal.Currency,
		"status":         tx.Status,
		"created_at":     time.Now(),
	}
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("deposit successful", vaultsavings.MapVaultTxToDepositResponse(tx)))
}

// ============================================================================
// WITHDRAW
// ============================================================================

// withdraw godoc
// @Summary Withdraw from Vault
// @Description Withdraw funds from vault savings goal to wallet
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Param withdrawRequest body object{to_wallet_id=string,amount=string,description=string} true "Withdraw Request"
// @Success 200 {object} vaultsavings.DepositResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/withdraw [post]
func (v *Vault) withdraw(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	var req struct {
		ToWalletID  string `json:"to_wallet_id" binding:"required"`
		Amount      string `json:"amount" binding:"required"`
		Description string `json:"description"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// Parse wallet ID
	walletID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid wallet ID"))
		return
	}

	// Validate amount
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid amount"))
		return
	}

	// Get vault to verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}

	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Create withdrawal request
	withdrawReq := vaultsavings.WithdrawRequest{
		UserID:      activeUser.UserID,
		VaultID:     vaultID,
		ToWalletID:  walletID,
		Amount:      req.Amount,
		Description: req.Description,
	}

	tx, err := v.vaultService.Withdraw(ctx.Request.Context(), withdrawReq)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to process withdrawal: %v", err))
		if errors.Is(err, vaultsavings.ErrInsufficientBalance) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("insufficient vault balance"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to process withdrawal"))
		return
	}

	// Prepare response based on status
	var message string
	if tx.Requires2fa.Bool {
		message = "withdrawal requires 2FA verification"
	} else {
		message = "withdrawal successful"
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventSavingsWithdrawn, "vault", goal.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Withdrawal of %s %s from vault %s initiated by user %d", amount.String(), goal.Currency, goal.ID.String(), activeUser.UserID)
	auditLog.OldValues = nil
	auditLog.NewValues = map[string]any{
		"transaction_id": tx.ID,
		"to_wallet_id":   walletID,
		"user_id":        activeUser.UserID,
		"vault_id":       goal.ID,
		"amount":         amount.String(),
		"currency":       goal.Currency,
		"status":         tx.Status,
		"created_at":     time.Now(),
	}
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess(message, vaultsavings.MapVaultTxToDepositResponse(tx)))
}

// ============================================================================
// GET TRANSACTIONS
// ============================================================================

// getVaultTransactions godoc
// @Summary Get Vault Transactions
// @Description Get transaction history for a specific vault
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Param limit query int false "Limit" default(20)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} []vaultsavings.DepositResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/transactions [get]
func (v *Vault) getVaultTransactions(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	// Verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Parse pagination
	limit := int32(20)
	offset := int32(0)
	if l := ctx.Query("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil && parsedLimit > 0 {
			limit = int32(parsedLimit)
		}
	}
	if o := ctx.Query("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil && parsedOffset >= 0 {
			offset = int32(parsedOffset)
		}
	}

	param := db.GetVaultTransactionsByVaultIDParams{
		Limit:   limit,
		Offset:  offset,
		VaultID: vaultID,
	}

	transactions, err := v.vaultService.GetVaultTransactions(ctx.Request.Context(), param)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch transactions: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transactions"))
		return
	}

	var filteredTransactions []vaultsavings.DepositResponse
	for _, tx := range transactions {
		filteredTransactions = append(filteredTransactions, *vaultsavings.MapVaultTxToDepositResponse(&tx))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("transactions retrieved successfully", filteredTransactions))
}

// getAllTransactions godoc
// @Summary Get All Vault Transactions
// @Description Get all vault transactions for the authenticated user
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Limit" default(20)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} []vaultsavings.DepositResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/transactions [get]
func (v *Vault) getAllTransactions(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Parse pagination
	limit := int32(20)
	offset := int32(0)
	if l := ctx.Query("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil && parsedLimit > 0 {
			limit = int32(parsedLimit)
		}
	}
	if o := ctx.Query("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil && parsedOffset >= 0 {
			offset = int32(parsedOffset)
		}
	}

	param := db.GetVaultTransactionsByUserIDParams{
		Limit:  limit,
		Offset: offset,
		UserID: activeUser.UserID,
	}

	transactions, err := v.vaultService.GetUserTransactions(ctx.Request.Context(), param)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch transactions: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch transactions"))
		return
	}

	var filteredTransactions []vaultsavings.DepositResponse
	for _, tx := range transactions {
		filteredTransactions = append(filteredTransactions, *vaultsavings.MapVaultTxToDepositResponse(&tx))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("transactions retrieved successfully", filteredTransactions))
}

// ============================================================================
// UPDATE GOAL
// ============================================================================

// updateGoal godoc
// @Summary Update Vault Goal
// @Description Update vault goal details (name, description, target amount)
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Param updateGoalRequest body object{name=string,description=string,goal_amount=string} true "Update Goal Request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id} [put]
func (v *Vault) updateGoal(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		GoalAmount  *string `json:"goal_amount"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// Verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Validate goal amount if provided
	if req.GoalAmount != nil {
		amount, err := decimal.NewFromString(*req.GoalAmount)
		if err != nil || amount.LessThanOrEqual(decimal.Zero) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid goal amount"))
			return
		}
	}

	err = v.vaultService.UpdateVaultDetails(ctx.Request.Context(), vaultID, req.Name, req.Description, req.GoalAmount)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to update vault goal: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update savings goal"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventVaultUpdated, "vault", goal.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Vault savings goal %s updated by user %d", goal.ID.String(), activeUser.UserID)
	auditLog.OldValues = map[string]any{
		"name":        goal.VaultName,
		"description": goal.Description,
		"goal_amount": goal.GoalAmount,
	}
	newValues := make(map[string]interface{})
	if req.Name != nil {
		newValues["name"] = *req.Name
	}
	if req.Description != nil {
		newValues["description"] = *req.Description
	}
	if req.GoalAmount != nil {
		newValues["goal_amount"] = *req.GoalAmount
	}
	auditLog.NewValues = newValues
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("savings goal updated successfully", nil))
}

// ============================================================================
// DELETE GOAL
// ============================================================================

// deleteGoal godoc
// @Summary Delete Vault Goal
// @Description Delete a vault goal (only if balance is zero)
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id} [delete]
func (v *Vault) deleteGoal(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	// Verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	err = v.vaultService.DeleteVault(ctx.Request.Context(), vaultID)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to delete vault goal: %v", err.Error()))
		if err.Error() == "VAULT_HAS_BALANCE_ERROR" {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot delete vault with balance"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to delete savings goal"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventVaultDeleted, "vault", goal.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Vault savings goal %s deleted by user %d", goal.ID.String(), activeUser.UserID)
	auditLog.OldValues = map[string]interface{}{
		"id":             goal.ID,
		"user_id":        goal.UserID,
		"name":           goal.VaultName,
		"target_amount":  goal.GoalAmount,
		"currency":       goal.Currency,
		"created_at":     goal.CreatedAt,
		"updated_at":     goal.UpdatedAt,
		"status":         goal.Status,
		"recurring_rule": goal.RecurringRule,
	}
	auditLog.NewValues = nil
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("savings goal deleted successfully", nil))
}

// ============================================================================
// UPDATE RECURRING RULE
// ============================================================================

// updateRecurringRule godoc
// @Summary Update Recurring Rule
// @Description Update recurring deposit settings for a vault goal
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Param updateRecurringRequest body vaultsavings.UpdateRecurringRuleRequest true "Update Recurring Request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/recurring [put]
func (v *Vault) updateRecurringRule(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	var req vaultsavings.UpdateRecurringRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// Verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	err = v.vaultService.UpdateRecurringRule(ctx.Request.Context(), vaultID, req)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to update recurring rule: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update recurring deposits"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventRecurringRuleUpdated, "vault", goal.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Recurring deposit rule for vault %s updated by user %d", goal.ID.String(), activeUser.UserID)
	auditLog.OldValues = map[string]interface{}{
		"recurring_rule": goal.RecurringRule,
	}
	auditLog.NewValues = map[string]interface{}{
		"recurring_rule": req,
	}
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("recurring deposits updated successfully", nil))
}

// pauseRecurring godoc
// @Summary Pause Recurring Deposits
// @Description Pause automatic recurring deposits for a vault goal
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/recurring/pause [post]
func (v *Vault) pauseRecurring(ctx *gin.Context) {
	enabled := false
	v.updateRecurringEnabled(ctx, &enabled)

	auditLog := audit.NewVaultLog(ctx, audit.EventVaultRecurringRulePaused, "vault", ctx.Param("id"), "", nil, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Recurring deposits for vault %s paused", ctx.Param("id"))
	v.audit.Log(auditLog)
}

// resumeRecurring godoc
// @Summary Resume Recurring Deposits
// @Description Resume automatic recurring deposits for a vault goal
// @Tags vault
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault Goal ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/recurring/resume [post]
func (v *Vault) resumeRecurring(ctx *gin.Context) {
	enabled := true
	v.updateRecurringEnabled(ctx, &enabled)

	auditLog := audit.NewVaultLog(ctx, audit.EventVaultRecurringRuleResumed, "vault", ctx.Param("id"), "", nil, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Recurring deposits for vault %s resumed", ctx.Param("id"))
	v.audit.Log(auditLog)
}

func (v *Vault) updateRecurringEnabled(ctx *gin.Context, enabled *bool) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	// Verify ownership
	goal, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault goal not found"))
		return
	}
	if goal.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	req := vaultsavings.UpdateRecurringRuleRequest{
		Enabled: enabled,
	}

	err = v.vaultService.UpdateRecurringRule(ctx.Request.Context(), vaultID, req)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to update recurring rule: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update recurring deposits"))
		return
	}

	action := "paused"
	if *enabled {
		action = "resumed"
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("recurring deposits %s successfully", action), nil))
}

// ============================================================================
// ADMIN ROUTES
// ============================================================================

// getAdminMetrics godoc
// @Summary Get Vault Metrics (Admin)
// @Description Get vault system metrics for admin dashboard
// @Tags vault-admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=db.GetVaultsDashboardMetricsRow}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/metrics [get]
func (v *Vault) getAdminMetrics(ctx *gin.Context) {
	// activeUser, err := utils.GetActiveUser(ctx)
	// if err != nil {
	// 	v.server.logger.Error(err.Error())
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	// if activeUser.Role == models.USER {
	// 	ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
	// 	return
	// }

	metrics, err := v.server.queries.GetVaultsDashboardMetrics(ctx.Request.Context())
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("failed to fetch vault metrics: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch metrics"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("metrics retrieved successfully", metrics))
}

// getSchedulerStats godoc
// @Summary Get Scheduler Stats (Admin)
// @Description Get current vault scheduler statistics
// @Tags vault-admin
// @Security BearerAuth
// @Success 200 {object} vaultsavings.SchedulerStats
// @Router /api/v1/vault/admin/scheduler/stats [get]
func (v *Vault) getSchedulerStats(ctx *gin.Context) {
	activeUser, _ := utils.GetActiveUser(ctx)
	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	stats, err := v.server.vaultScheduler.GetStats(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get stats"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("stats retrieved", stats))
}

// triggerSchedulerNow godoc
// @Summary Trigger Scheduler Now (Admin)
// @Description Manually trigger recurring deposits processing
// @Tags vault-admin
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/vault/admin/scheduler/trigger [post]
func (v *Vault) triggerSchedulerNow(ctx *gin.Context) {
	activeUser, _ := utils.GetActiveUser(ctx)
	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	if err := v.server.vaultScheduler.ProcessAllDueNow(ctx.Request.Context()); err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to trigger"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventSchedulerTriggered, "vault", "N/A", activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Vault scheduler manually triggered by admin user %d", activeUser.UserID)
	auditLog.OldValues = nil
	auditLog.NewValues = map[string]interface{}{
		"triggered_at": time.Now(),
		"user_id":      activeUser.UserID,
	}
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("scheduler triggered", nil))
}

// ============================================================================
// USER YIELD ENDPOINTS
// ============================================================================

// getYieldHistory godoc
// @Summary Get Vault Yield History
// @Description Get historical yield earnings for a specific vault
// @Tags vault-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault ID"
// @Param limit query int false "Limit" default(20)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=[]vaultsavings.VaultYieldResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/yield-history [get]
func (v *Vault) getYieldHistory(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	// Verify ownership
	vault, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault not found"))
		return
	}
	if vault.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}
	// Parse pagination
	limit := int32(20)
	offset := int32(0)
	if l := ctx.Query("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil && parsedLimit > 0 {
			limit = int32(parsedLimit)
		}
	}
	if o := ctx.Query("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil && parsedOffset >= 0 {
			offset = int32(parsedOffset)
		}
	}

	yields, err := v.server.queries.GetVaultYieldsByVaultID(ctx.Request.Context(), db.GetVaultYieldsByVaultIDParams{
		VaultID: vaultID,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to get yield history: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get yield history"))
		return
	}

	yieldsResponse := make([]vaultsavings.VaultYieldResponse, len(yields))
	for i, y := range yields {
		yieldsResponse[i] = *vaultsavings.MapVaultYieldToResponse(&y)
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yield history retrieved", yieldsResponse))
}

// getYieldProjection godoc
// @Summary Get Yield Projection
// @Description Get estimated future yield earnings for a vault
// @Tags vault-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Vault ID"
// @Param days query int false "Projection period in days" default(30)
// @Success 200 {object} basemodels.SuccessResponse{data=vaultsavings.YieldProjection}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/goals/{id}/yield-projection [get]
func (v *Vault) getYieldProjection(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	vaultID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid vault ID"))
		return
	}

	// Verify ownership
	vault, err := v.vaultService.GetVaultByID(ctx.Request.Context(), vaultID)
	if err != nil {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("vault not found"))
		return
	}
	if vault.UserID != activeUser.UserID {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("access denied"))
		return
	}

	// Parse days parameter
	days := 30 // Default 30 days
	if d := ctx.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	projection, err := v.yieldService.GetYieldProjection(ctx.Request.Context(), vaultID, days)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to get yield projection: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to calculate projection"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yield projection calculated", projection))
}

// getYieldSummary godoc
// @Summary Get User Yield Summary
// @Description Get summary of all yield earnings across all vaults
// @Tags vault-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/yield-summary [get]
func (v *Vault) getYieldSummary(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Get all yields for user
	yields, err := v.server.queries.GetVaultYieldsByUserID(ctx.Request.Context(), db.GetVaultYieldsByUserIDParams{
		UserID: activeUser.UserID,
		Limit:  1000,
		Offset: 0,
	})
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to get yields: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get yield summary"))
		return
	}

	// Calculate totals by currency
	totals := make(map[string]decimal.Decimal)
	totalEarnings := decimal.Zero

	for _, y := range yields {
		if y.Status.Valid && y.Status.String == "credited" { // make status enums a const type
			amount, _ := decimal.NewFromString(y.YieldAmount)

			// Currency-specific totals
			if existing, ok := totals[y.VaultName]; ok {
				totals[y.VaultName] = existing.Add(amount)
			} else {
				totals[y.VaultName] = amount
			}

			// Overall total
			totalEarnings = totalEarnings.Add(amount)
		}
	}

	summary := map[string]any{
		"total_yield_earnings": totalEarnings.StringFixed(4),
		"total_yield_events":   len(yields),
		"by_vault":             totals,
		"last_credited":        nil,
	}

	if len(yields) > 0 {
		summary["last_credited"] = yields[0].CreditedAt
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yield summary retrieved", summary))
}

// ============================================================================
// ADMIN YIELD MANAGEMENT ENDPOINTS
// ============================================================================

// listYieldConfigs godoc
// @Summary List Yield Configurations (Admin)
// @Description Get all yield configurations
// @Tags vault-admin-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=[]vaultsavings.VaultYieldConfigResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/yield-configs [get]
func (v *Vault) listYieldConfigs(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	configs, err := v.server.queries.GetAllActiveYieldConfigs(ctx.Request.Context())
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to get yield configs: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get yield configs"))
		return
	}

	configsResponse := make([]vaultsavings.VaultYieldConfigResponse, len(configs))
	for i, c := range configs {
		configsResponse[i] = *vaultsavings.MapVaultYieldConfigToResponse(&c)
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yield configs retrieved", configsResponse))
}

// createYieldConfig godoc
// @Summary Create Yield Configuration (Admin)
// @Description Create a new yield configuration for a currency
// @Tags vault-admin-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param createYieldConfigRequest body vaultsavings.CreateYieldConfigParams true "Yield Config"
// @Success 201 {object} basemodels.SuccessResponse{data=vaultsavings.VaultYieldConfigResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/yield-configs [post]
func (v *Vault) createYieldConfig(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// if activeUser.Role == models.USER {
	// 	ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
	// 	return
	// }

	var req vaultsavings.CreateYieldConfigParams
	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// Validate APY is reasonable (0-50%)
	apy, _ := decimal.NewFromString(req.ApyRate)
	if apy.LessThan(decimal.Zero) || apy.GreaterThan(decimal.NewFromInt(50)) {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("APY must be between 0 and 50"))
		return
	}

	config, err := v.yieldService.CreateYieldConfig(ctx.Request.Context(), req)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to create yield config: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create yield config"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventYieldConfigCreated, "vault", config.ID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Yield config %s created by user %d", config.ID.String(), activeUser.UserID)
	auditLog.NewValues = map[string]interface{}{
		"id":                    config.ID,
		"currency":              config.Currency,
		"apy_rate":              config.ApyRate,
		"min_balance_for_yield": config.MinBalanceForYield,
		"compound_frequency":    config.CompoundFrequency,
		"is_active":             config.IsActive,
		"effective_from":        config.EffectiveFrom,
		"effective_until":       config.EffectiveUntil,
		"notes":                 config.Notes,
		"created_at":            config.CreatedAt,
	}
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("yield config created", *vaultsavings.MapVaultYieldConfigToResponse(config)))
}

type UpdateYieldConfigParams struct {
	ID                 uuid.UUID `json:"id"`
	ApyRate            string    `json:"apy_rate"`
	MinBalanceForYield string    `json:"min_balance_for_yield"`
	CompoundFrequency  string    `json:"compound_frequency"`
	IsActive           bool      `json:"is_active"`
	EffectiveUntil     time.Time `json:"effective_until"`
	Notes              string    `json:"notes"`
}

// updateYieldConfig godoc
// @Summary Update Yield Configuration (Admin)
// @Description Update an existing yield configuration
// @Tags vault-admin-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Config ID"
// @Param updateYieldConfigRequest body vaultsavings.UpdateYieldConfigParams true "Updated Config"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/yield-configs/{id} [put]
func (v *Vault) updateYieldConfig(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	configID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid config ID"))
		return
	}

	var req vaultsavings.UpdateYieldConfigParams
	if err := ctx.ShouldBindJSON(&req); err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid request body"))
		return
	}

	// get existing config to compare old values
	existingConfig, err := v.server.queries.GetYieldConfigByID(ctx.Request.Context(), configID)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to fetch existing yield config: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch existing yield config"))
		return
	}

	if err := v.yieldService.UpdateYieldConfig(ctx.Request.Context(), configID, req); err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to update yield config: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update yield config"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventYieldConfigUpdated, "vault", configID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Yield config %s updated by user %d", configID.String(), activeUser.UserID)
	auditLog.NewValues = map[string]any{
		"apy_rate":              req.ApyRate,
		"min_balance_for_yield": req.MinBalanceForYield,
		"compound_frequency":    req.CompoundFrequency,
		"is_active":             req.IsActive,
		"effective_until":       req.EffectiveUntil,
		"notes":                 req.Notes,
	}

	auditLog.OldValues = map[string]any{
		"apy_rate":              existingConfig.ApyRate,
		"min_balance_for_yield": existingConfig.MinBalanceForYield,
		"compound_frequency":    existingConfig.CompoundFrequency,
		"is_active":             existingConfig.IsActive,
		"effective_until":       existingConfig.EffectiveUntil,
		"notes":                 existingConfig.Notes,
	}
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yield config updated", nil))
}

// deactivateYieldConfig godoc
// @Summary Deactivate Yield Configuration (Admin)
// @Description Deactivate a yield configuration
// @Tags vault-admin-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Config ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/yield-configs/{id}/deactivate [post]
func (v *Vault) deactivateYieldConfig(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	configID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid config ID"))
		return
	}

	if err := v.server.queries.DeactivateYieldConfig(ctx.Request.Context(), configID); err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to deactivate config: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to deactivate config"))
		return
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventYieldConfigDeactivated, "vault", configID.String(), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Yield config %s deactivated by user %d", configID.String(), activeUser.UserID)
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yield config deactivated", nil))
}

// @Summary Process Yields Now (Admin)
// @Description Manually trigger yield calculations for all due vaults
// @Tags vault-admin-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/process-yields-now [post]
func (v *Vault) processYieldsNow(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	successCount, failureCount, err := v.yieldService.ProcessAllDueYields(ctx.Request.Context(), 1000)
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to process yields: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to process yields"))
		return
	}

	result := map[string]any{
		"success_count":   successCount,
		"failure_count":   failureCount,
		"total_processed": successCount + failureCount,
	}

	// audit log
	auditLog := audit.NewVaultLog(ctx, audit.EventYieldsProcessed, "vault", "", activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("Yields processed by admin user %d: %d success, %d failure", activeUser.UserID, successCount, failureCount)
	auditLog.NewValues = result
	v.audit.Log(auditLog)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("yields processed", result))
}

// getYieldSchedulerStats godoc
// @Summary Get Yield Scheduler Stats (Admin)
// @Description Get statistics about the yield calculation scheduler
// @Tags vault-admin-yields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Router /api/v1/vault/admin/yield-scheduler/stats [get]
func (v *Vault) getYieldSchedulerStats(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		v.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	stats, err := v.server.yieldScheduler.GetStats(ctx.Request.Context())
	if err != nil {
		v.server.logger.Error(fmt.Sprintf("Failed to get scheduler stats: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get stats"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("scheduler stats retrieved", stats))
}

func (v *Vault) ProcessVaultNow(ctx *gin.Context) {} // use ProcessVaultNow in vault scheduler

func (v *Vault) ProcessVaultYieldNow(ctx *gin.Context) {} // use ProcessVaultYieldNow in yield_scheduler
