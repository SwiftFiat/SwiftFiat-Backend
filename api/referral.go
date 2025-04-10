package api

import (
	"errors"
	"fmt"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/referral"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"net/http"
)

type Referral struct {
	server                  *Server
	service                 *referral.Service
	repo                    *referral.Repo
	logger                  *logging.Logger
	AmountEarnedPerReferral decimal.Decimal
}

func (r Referral) router(server *Server) {
	r.server = server
	r.repo = referral.NewReferralRepository(server.queries)
	r.AmountEarnedPerReferral = decimal.NewFromFloat(1000.00) // TODO: make this configurable
	r.logger = r.server.logger
	r.service = referral.NewReferralService(r.repo, r.logger)

	serverGroupV1 := server.router.Group("/api/v1/referral")
	serverGroupV1.GET("/test", r.testReferral)
	serverGroupV1.GET("/list", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetUserReferrals)
	serverGroupV1.GET("/earnings", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetEarnings)
	serverGroupV1.POST("/request=withdrawal", r.server.authMiddleware.AuthenticatedMiddleware(), r.RequestWithdrawal)
	//serverGroupV1.GET("/referral/withdrawals", r.ListWithdrawals)
	//serverGroupV1.PUT("/withdrawals/:id", r.AdminProcessWithdrawal)
}

func (a Referral) testReferral(ctx *gin.Context) {
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Referral API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

func (r *Referral) GetUserReferrals(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		// Todo: add logging
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	referrals, err := r.service.GetUserReferrals(ctx, activeUser.UserID)
	if err != nil {
		//	add logging
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get user referrals"))
	}
	ctx.JSON(http.StatusOK, basemodels.SuccessResponse{
		Status:  "success",
		Message: "referrals retrieved successfully",
		Data:    referrals,
	})
}

func (r *Referral) GetEarnings(c *gin.Context) {
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
	})
}

func (r *Referral) RequestWithdrawal(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		// Todo: add logging
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req struct {
		Amount decimal.Decimal `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	wr, err := r.service.RequestWithdrawal(c, activeUser.UserID, req.Amount)
	if err != nil {
		if errors.Is(err, referral.ErrInsufficientBalance) {
			c.JSON(http.StatusBadRequest, basemodels.NewError("insufficient balance"))
			return
		}
		if errors.Is(err, referral.ErrWithdrawalThreshold) {
			c.JSON(http.StatusBadRequest, basemodels.NewError("amount is below withdrawal threshold"))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to create withdrawal request"))
	}

	c.JSON(http.StatusCreated, basemodels.SuccessResponse{
		Status:  "success",
		Message: "withdrawal request created successfully",
		Data:    wr,
	})
}

func (r *Referral) UpdateWithdrawalRequest(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		// Todo: add logging
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	//	check if active user is admin
	if activeUser.Role != "admin" {
		r.logger.Error(fmt.Errorf("unauthorized access: only admin can process withdrawal request"))
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	var req struct {
		ID     int64                            `json:"id" binding:"required"`
		Status referral.WithdrawalRequestStatus `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	wr, err := r.service.UpdateWithdrawalRequest(c, req.ID, req.Status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update withdrawal request"})
		return
	}

	c.JSON(http.StatusOK, wr)
}

func (r *Referral) Withdraw(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		// Todo: add logging
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	var req struct {
		WalletID          uuid.UUID `json:"wallet_id" binding:"required"`
		WithdrawRequestID int64     `json:"withdraw_request_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
	}

	wallet, err := r.server.queries.GetWallet(c, req.WalletID)
	if err != nil {
		r.logger.Error(fmt.Errorf("failed to get wallet [api/referral.go - Withdraw]: %w", err))
		c.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.InvalidWalletInput))
		return
	}

	wr, err := r.server.queries.GetWithdrawalRequest(c, req.WithdrawRequestID)
	if err != nil {
		return
	}

	amt, err := decimal.NewFromString(wr.Amount)
	if err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid amount"))
		return
	}

	wallett, err := r.service.Withdraw(c, req.WithdrawRequestID, int32(activeUser.UserID), amt, wallet.ID)
	if err != nil {
		r.logger.Error(err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred, try again later."))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("withdrawal successful", wallett))

}
