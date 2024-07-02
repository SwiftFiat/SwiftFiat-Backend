package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	service "github.com/SwiftFiat/SwiftFiat-Backend/service/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

func (a *Auth) sendOTP(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	user, err := a.server.queries.GetUserByID(context.Background(), activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("user not found - not authorized to access resources"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	otp := utils.GenerateOTP()
	newParam := db.UpsertOTPParams{
		UserID:    int32(user.ID),
		Otp:       otp,
		Expired:   false,
		ExpiresAt: time.Now().Add(time.Minute * 30),
	}

	log.Default().Output(0, fmt.Sprintf("newParam Expiry: %v", newParam.ExpiresAt.Local()))

	/// Add OTP to DB
	resp, err := a.server.queries.UpsertOTP(context.Background(), newParam)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	em := service.OtpNotification{
		Channel:     service.EMAIL,
		PhoneNumber: user.PhoneNumber,
		Email:       user.Email,
		Config:      a.server.config,
	}

	log.Default().Output(0, fmt.Sprintf("Generated OTP: %v; FetchedOTP: %v", otp, resp.Otp))
	log.Default().Output(0, fmt.Sprintf("FetchedOTP Expiry: %v", resp.ExpiresAt.Local()))

	err = em.SendOTP(resp.Otp)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}
	ctx.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("OTP Sent successfully to your %v", em.Channel), nil))
}

func (a *Auth) verifyOTP(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	var otp models.UserOTPParams

	err = ctx.ShouldBindJSON(&otp)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid OTP, key 'otp' missing"))
		return
	}

	dbOTP, err := a.server.queries.GetOTPByUserID(context.Background(), int32(activeUser.UserID))
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred verifying your OTP %v", err.Error())))
		return
	}

	/// User OTP Exists
	if dbOTP.Otp != otp.OTP {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	updateUserParam := db.UpdateUserVerificationParams{
		Verified:  true,
		UpdatedAt: time.Now(),
		ID:        activeUser.UserID,
	}

	/// Update User verified status
	newUser, err := a.server.queries.UpdateUserVerification(context.Background(), updateUserParam)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred updating your Account %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("account status verified successfully", models.UserResponse{}.ToUserResponse(&newUser)))
}
