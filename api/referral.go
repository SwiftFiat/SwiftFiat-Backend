package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/referral"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
)

type Referral struct {
	server                  *Server
	service                 *referral.Service
	repo                    *referral.Repo
	logger                  *logging.Logger
	AmountEarnedPerReferral decimal.Decimal
	notifyr                 *service.Notification
	audit                   *audit.Service
}

func (r Referral) router(server *Server) {
	r.server = server
	r.repo = referral.NewReferralRepository(server.queries)
	r.AmountEarnedPerReferral = decimal.NewFromFloat(1000.00) // TODO: make this configurable
	r.logger = r.server.logger
	r.notifyr = service.NewNotificationService(r.server.queries)
	r.service = referral.NewReferralService(r.repo, r.logger, r.notifyr, r.server.pushNotification)
	r.audit = r.server.auditService

	serverGroupV1 := server.router.Group("/api/v1/referral")
	serverGroupV1.GET("/test", r.testReferral)
	serverGroupV1.GET("/list", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetUserReferrals)
	serverGroupV1.GET("/earnings", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetEarnings)
	serverGroupV1.POST("/track", r.server.authMiddleware.AuthenticatedMiddleware(), r.Trackreferral)
	serverGroupV1.POST("/reminder/:id", r.server.authMiddleware.AuthenticatedMiddleware(), r.Reminder)
	serverGroupV1.GET("/admin/list", r.server.authMiddleware.AuthenticatedMiddleware(), r.AdminGetUserReferrals)
	serverGroupV1.POST("/admin/create-referral-config", r.server.authMiddleware.AuthenticatedMiddleware(), r.CreateReferralConfig)
	serverGroupV1.PUT("/admin/update-referral-config", r.server.authMiddleware.AuthenticatedMiddleware(), r.UpdateReferralConfig)
	serverGroupV1.GET("/admin/get-referral-config", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetReferralConfig)
	serverGroupV1.GET("/admin/transactions", r.server.authMiddleware.AuthenticatedMiddleware(), r.transactions)
	serverGroupV1.PUT("/freeze/:user_id", r.server.authMiddleware.AuthenticatedMiddleware(), r.freeze)
	serverGroupV1.PUT("/unfreeze/:user_id", r.server.authMiddleware.AuthenticatedMiddleware(), r.unfreeze)
	serverGroupV1.POST("/withdraw", r.server.authMiddleware.AuthenticatedMiddleware(), r.withdraw)
}

// testReferral godoc
// @Summary      Test Referral API
// @Description  Tests if the Referral API is active
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse
// @Router       /api/v1/referral/test [get]
func (r Referral) testReferral(ctx *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(ctx)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Referral API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

type TrackReferralRequest struct {
	ReferralCode string `json:"referral_code" binding:"required"`
}

// Trackreferral godoc
// @Summary      Track Referral
// @Description  Tracks a referral using the provided referral code
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Param        referral_code  body      string  true  "Referral Code"
// @Success      200  {object}  basemodels.SuccessResponse
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/track [post]
func (r *Referral) Trackreferral(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	var request TrackReferralRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'referral_code'"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError("user not found"))
		return
	}

	config, err := r.server.queries.GetReferralConfig(c)
	if err != nil {
		r.server.logger.Error("Failed to get referral config", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get referral config"))
		return
	}

	refAmount, err := decimal.NewFromString(config.ReferralAmount)
	if err != nil {
		r.server.logger.Error("Failed to parse referral amount", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to parse referral amount"))
		return
	}

	ref, err := r.service.TrackReferral(c, request.ReferralCode, activeUser.UserID, refAmount)
	if err != nil {
		r.server.logger.Error("Failed to track referral", logrus.Fields{
			"error":         err,
			"referral_code": request.ReferralCode,
			"user_id":       activeUser.UserID,
		})
		c.JSON(http.StatusBadRequest, basemodels.NewError("failed to track referral: "+err.Error()))
		return
	}

	// audit log
	auditLog := audit.NewReferralLog(c, audit.EventReferralTracked, "referral", fmt.Sprintf("Referral tracked for user %d", activeUser.UserID), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("User %d tracked referral with code %s", activeUser.UserID, request.ReferralCode)
	r.audit.Log(auditLog)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Referral tracked successfully", ref))
}

type ReferralWithUser struct {
	Referral referral.Referral `json:"referral" binding:"required"`
	User     string            `json:"first_name" binding:"required"`
}

// GetUserReferrals godoc
// @Summary      Get User Referrals
// @Description  Retrieves the list of referrals for the authenticated user
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  basemodels.SuccessResponse{data=[]ReferralWithUser}
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/list [get]
func (r *Referral) GetUserReferrals(ctx *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(ctx)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	referrals, err := r.service.GetUserReferrals(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var refsWithUser []ReferralWithUser
	for _, ref := range referrals {
		user, err := r.server.queries.GetUserByID(ctx, ref.RefereeID)
		if err != nil {
			r.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get referee data"))
			return
		}
		refsWithUser = append(refsWithUser, ReferralWithUser{
			Referral: ref,
			User:     user.FirstName.String,
		})
	}
	ctx.JSON(http.StatusOK, basemodels.SuccessResponse{
		Status:  "success",
		Message: "referrals retrieved successfully",
		Data:    refsWithUser,
		Version: utils.REVISION,
	})
}

// AdminGetUserReferrals godoc
// @Summary      list User Referrals
// @Description  Retrieves all referrals
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  []db.UserReferral
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/admin/list [get]
func (r *Referral) AdminGetUserReferrals(ctx *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(ctx)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	referrals, err := r.service.GetAllReferrals(ctx)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("", referrals))
}

// GetEarnings godoc
// @Summary      Get Referral Earnings
// @Description  Retrieves the referral earnings for the authenticated user
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/earnings [get]
func (r *Referral) GetEarnings(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		// Todo: add logging
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	earnings, err := r.service.GetReferralEarnings(c, activeUser.UserID)
	if err != nil {
		//	add logging
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user earnings"))
		return
	}
	c.JSON(http.StatusOK, basemodels.SuccessResponse{
		Status:  "success",
		Message: "earnings retrieved successfully",
		Data:    earnings,
		Version: utils.REVISION,
	})
}

// GetEarnings godoc
// @Summary      Get Referral Earnings
// @Description  Retrieves the referral earnings for the authenticated user
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/reminder/:id [get]
func (r *Referral) Reminder(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	userID := c.Param("id")
	if userID == "" {
		r.logger.Error("user_id parameter is empty")
		c.JSON(http.StatusBadRequest, basemodels.NewError("user_id is required"))
		return
	}
	// parsedUserID, err := strconv.Atoi(userID)
	// if err != nil {
	// 	r.logger.Error(fmt.Errorf("invalid user_id: %v, provided: %s", err, userID))
	// 	c.JSON(http.StatusBadRequest, basemodels.NewError("invalid user_id"))
	// 	return
	// }
	// r.notifyr.Create(c, int32(parsedUserID), "referral", "You have a pending referral request, complete your KYC!")
	c.JSON(http.StatusOK, basemodels.NewSuccess("reminder sent successfully", nil))
}

// CreateReferralConfig godoc
// @Summary      Create Referral Config
// @Description  Creates a new referral config
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/admin/create-referral-config [post]
func (r *Referral) CreateReferralConfig(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	//	check if active user is admin
	if activeUser.Role == models.USER {
		r.logger.Error(fmt.Errorf("unauthorized access"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req struct {
		ReferralPercentageEarnedPerConversion string `json:"referral_percentage_earned_per_conversion" binding:"required"`
		ReferralAmount                        string `json:"referral_amount" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	minimumWithdrawalThreshold, err := decimal.NewFromString(req.ReferralPercentageEarnedPerConversion)
	if err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid minimum withdrawal threshold"))
		return
	}

	referralAmount, err := decimal.NewFromString(req.ReferralAmount)
	if err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid referral amount"))
		return
	}

	config, err := r.service.CreateReferralConfig(c, referralAmount, minimumWithdrawalThreshold)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategoryRewards,
		audit.EventCreateReferralConfig,
		fmt.Sprint(config.ID),
		fmt.Sprintf("Admin %d created referral config %d", activeUser.UserID, config.ID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	r.audit.Log(entry)
	c.JSON(http.StatusOK, basemodels.NewSuccess("referral config created successfully", nil))
}

// UpdateReferralConfig godoc
// @Summary      Update Referral Config
// @Description  Updates the referral config
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Param        id  path  int  true  "Referral Config ID"
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/admin/update-referral-config [put]
func (r *Referral) UpdateReferralConfig(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	//	check if active user is admin
	if activeUser.Role == models.ADMIN {
		r.logger.Error(fmt.Errorf("unauthorized access"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req struct {
		ID                                    int64   `json:"id" binding:"required"`
		ReferralPercentageEarnedPerConversion *string `json:"referral_percentage_earned_per_conversion"`
		ReferralAmount                        *string `json:"referral_amount"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	var minThresholdPtr *decimal.Decimal
	var refAmountPtr *decimal.Decimal

	if req.ReferralPercentageEarnedPerConversion != nil {
		val, err := decimal.NewFromString(*req.ReferralPercentageEarnedPerConversion)
		if err != nil {
			c.JSON(http.StatusBadRequest, basemodels.NewError("invalid threshold"))
			return
		}
		minThresholdPtr = &val
	}

	if req.ReferralAmount != nil {
		val, err := decimal.NewFromString(*req.ReferralAmount)
		if err != nil {
			c.JSON(http.StatusBadRequest, basemodels.NewError("invalid amount"))
			return
		}
		refAmountPtr = &val
	}

	config, err := r.service.UpdateReferralConfig(c, req.ID, minThresholdPtr, refAmountPtr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	entry := audit.NewLog(
		c,
		audit.CategoryRewards,
		audit.EventUpdateReferralConfig,
		fmt.Sprint(config.ID),
		fmt.Sprintf("Admin %d updated referral config %d", activeUser.UserID, config.ID),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	r.audit.Log(entry)
	c.JSON(http.StatusOK, basemodels.NewSuccess("referral config updated successfully", nil))
}

// GetReferralConfig godoc
// @Summary      Get Referral Config
// @Description  Retrieves the referral config
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/admin/get-referral-config [get]
func (r *Referral) GetReferralConfig(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// check if active user is admin
	if activeUser.Role == models.USER {
		r.logger.Error(fmt.Errorf("unauthorized access"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	config, err := r.service.GetReferralConfig(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, basemodels.NewSuccess("referral config retrieved successfully", config))
}

// withdraw godoc
// @Summary      Withdraw Referral Earnings
// @Description  Allows a user to withdraw their referral earnings
// @Tags         Referral
// @Accept       json
// @Produce      json
// @Param        amount  body      string  true  "Amount to Withdraw"
// @Success      200  {object}  basemodels.SuccessResponse
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      401  {object}  basemodels.ErrorResponse
// @Router       /api/v1/referral/withdraw [post]
func (r *Referral) withdraw(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req struct {
		IdempotencyKey string `json:"idempotency_key" binding:"required"`
		Amount         string `json:"amount" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid amount"))
		return
	}

	tx, err := r.service.Withdraw(c, amount, activeUser.UserID, req.IdempotencyKey)
	if err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	auditLog := audit.NewReferralLog(c, audit.EventReferralWithdrawal, "referral", fmt.Sprintf("Referral withdrawal of %s by user %d", amount.String(), activeUser.UserID), activeUser.Role, &activeUser.UserID, audit.SeverityInfo)
	auditLog.Description = fmt.Sprintf("User %d withdrew referral earnings of %s", activeUser.UserID, amount.String())
	r.audit.Log(auditLog)

	c.JSON(http.StatusOK, basemodels.NewSuccess("withdrawal completed successfully", tx))
}

func (r *Referral) transactions(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// check if active user is admin
	if activeUser.Role == models.USER {
		r.logger.Error(fmt.Errorf("unauthorized access"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	txs, err := r.server.queries.GetAllReferralTransactions(c)
	if err != nil {
		c.JSON(500, basemodels.NewError("failed to get transactions, try again"))
		return
	}

	c.JSON(200, basemodels.NewSuccess("", txs))
}

func (r *Referral) freeze(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// check if active user is admin
	if activeUser.Role == models.USER {
		r.logger.Error(fmt.Errorf("unauthorized access"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(400, basemodels.NewError("invalid user id"))
		return
	}

	err = r.server.queries.FreezeReferralEarning(c, int32(userId))
	if err != nil {
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(200, basemodels.NewSuccess("Referral earnings freezed", nil))
}

func (r *Referral) unfreeze(c *gin.Context) {
	settings, err := r.server.queries.GetSystemSettings(c)
	if err != nil {
		r.server.logger.Error("Failed to get system settings", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get system settings"))
		return
	}
	if !settings.RewardsEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("referral rewards are disabled"))
		return
	}

	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// check if active user is admin
	if activeUser.Role == models.USER {
		r.logger.Error(fmt.Errorf("unauthorized access"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(400, basemodels.NewError("invalid user id"))
		return
	}

	err = r.server.queries.UnFreezeReferralEarning(c, int32(userId))
	if err != nil {
		c.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	c.JSON(200, basemodels.NewSuccess("Referral earnings unfreezed", nil))
}
