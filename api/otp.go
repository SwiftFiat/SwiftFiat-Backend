package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/models"
	service "github.com/SwiftFiat/SwiftFiat-Backend/service/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

func (a *Auth) sendOTP(ctx *gin.Context) {
	userId, err := utils.GetActiveUser(ctx)
	if err != nil {
		return
	}

	user, err := a.server.queries.GetUserByID(context.Background(), userId)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, models.NewError("user not found - not authorized to access resources"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// TODO: Verify User Email and Phone Number Don't have issues e.g. matches supplied email

	em := service.OtpNotification{
		Channel:     service.EMAIL,
		PhoneNumber: user.PhoneNumber,
		Email:       user.Email,
		Config:      a.server.config,
	}

	err = em.SendOTP()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, models.NewSuccess("OTP Sent successfully", nil))
}

func (a *Auth) verifyOTP(ctx *gin.Context) {
	userId, err := utils.GetActiveUser(ctx)
	if err != nil {
		return
	}

	user, err := a.server.queries.GetUserByID(context.Background(), userId)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, models.NewError("user not found - not authorized to access resources"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// TODO: Verify User Email and Phone Number Don't have issues e.g. matches supplied email

	em := service.OtpNotification{
		Channel:     service.EMAIL,
		PhoneNumber: user.PhoneNumber,
		Email:       user.Email,
		Config:      a.server.config,
	}

	err = em.SendOTP()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, models.NewSuccess("OTP Sent successfully", nil))
}
