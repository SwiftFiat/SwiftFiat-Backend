package api

import (
	"errors"
	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/referral"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"net/http"
	"strconv"
)

type Referral struct {
	server                  *Server
	service                 *referral.Service
	repo                    *referral.Repo
	AmountEarnedPerReferral decimal.Decimal
}

func (r Referral) router(server *Server) {
	r.server = server
	r.repo = referral.NewReferralRepository(server.queries)
	r.AmountEarnedPerReferral = decimal.NewFromFloat(1000.00) // TODO: make this configurable
	r.service = referral.NewReferralService(r.repo)

	serverGroupV1 := server.router.Group("/api/v1/referral")
	serverGroupV1.GET("/test", r.testReferral)
	serverGroupV1.GET("/list", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetUserReferrals)
	serverGroupV1.GET("/earnings", r.server.authMiddleware.AuthenticatedMiddleware(), r.GetEarnings)
	serverGroupV1.POST("/withdraw", r.server.authMiddleware.AuthenticatedMiddleware(), r.RequestWithdrawal)
	//serverGroupV1.GET("/referral/withdrawals", r.ListWithdrawals)
	//serverGroupV1.PUT("/withdrawals/:id", r.AdminProcessWithdrawal)
	serverGroupV1.POST("/track-referral", r.server.authMiddleware.AuthenticatedMiddleware(), r.TrackReferral)
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

	var req referral.WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	wr, err := r.service.RequestWithdrawal(c, activeUser.UserID, req)
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

//func (r *Referral) ListWithdrawals(c *gin.Context) {
//	activeUser, err := utils.GetActiveUser(c)
//	if err != nil {
//		// Todo: add logging
//		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
//		return
//	}
//}

//func (r *Referral) AdminProcessWithdrawal(c *gin.Context) {
//	//	check if active user is admin
//	activeUser, err := utils.GetActiveUser(c)
//	if err != nil {
//		// Todo: add logging
//		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
//		return
//	}
//
//	var req struct {
//		Status referral.WithdrawalRequestStatus `json:"status" binding:"required"`
//		Notes  string                           `json:"notes"`
//	}
//
//	if err := c.ShouldBindJSON(&req); err != nil {
//		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
//		return
//	}
//
//	requestID, err := strconv.ParseInt(c.Param("id"), 10, 64)
//	if err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request ID"})
//		return
//	}
//
//	wr, err := r.service.ProcessWithdrawalRequest(c.Request.Context(), requestID, req.Status, adminID, req.Notes)
//	if err != nil {
//		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process withdrawal"})
//		return
//	}
//
//	c.JSON(http.StatusOK, wr)
//}

func (r *Referral) TrackReferral(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		// Todo: add logging
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	referrerIDStr := c.Query("ref")
	if referrerIDStr == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("referral code required"))
		return
	}

	referrerID, err := strconv.ParseInt(referrerIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid referral code"})
		return
	}

	if referrerID == activeUser.UserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot refer yourself"})
		return
	}

	refT, err := r.service.TrackReferral(c.Request.Context(), referrerID, activeUser.UserID, r.AmountEarnedPerReferral)

	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to track referral"))
		return
	}

	c.JSON(http.StatusOK, basemodels.SuccessResponse{
		Status:  "success",
		Message: "referral tracked successfully",
		Data:    refT,
	})
}
