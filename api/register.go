package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	service "github.com/SwiftFiat/SwiftFiat-Backend/service/notification_service"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/lib/pq"
)

func (a *Auth) register(ctx *gin.Context) {
	var user models.RegisterUserParams

	err := ctx.ShouldBindJSON(&user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	arg := db.CreateUserParams{
		FirstName:   sql.NullString{String: user.FirstName, Valid: true},
		LastName:    sql.NullString{String: user.LastName, Valid: true},
		Email:       user.Email,
		PhoneNumber: user.PhoneNumber,
		Role:        models.USER,
	}

	newUser, err := a.server.queries.CreateUser(context.Background(), arg)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" {
				// 23505 --> Violated Unique Constraints
				// TODO: Make these constants
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "user already exists"})
				return
			}
			fmt.Println("pq error:", pqErr.Code.Name())
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// sms := service.SmsNotification{
	// 	Message:     "Your OTP is 5439",
	// 	PhoneNumber: newUser.PhoneNumber,
	// 	Config:      a.server.config,
	// }

	em := service.EmailNotification{
		Message: "Your OTP is 3349",
		Email:   "johnpaulmuoneme@gmail.com",
		Subject: "Account OTP",
		Config:  a.server.config,
	}

	err = em.SendEmail()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "error sending OTP"})
		return
	}

	ctx.JSON(http.StatusCreated, models.UserResponse{}.ToUserResponse(&newUser))
}

func (a *Auth) registerAdmin(ctx *gin.Context) {
	var user models.RegisterAdminParams

	/// Validate Presence of Placeholder Values
	validate := validator.New(validator.WithRequiredStructEnabled())
	err := validate.Struct(user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = ctx.ShouldBindJSON(&user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	arg := db.CreateUserParams{
		FirstName:   sql.NullString{String: user.FirstName, Valid: true},
		LastName:    sql.NullString{String: user.LastName, Valid: true},
		Email:       user.Email,
		PhoneNumber: user.PhoneNumber,
		Role:        models.ADMIN,
	}

	newUser, err := a.server.queries.CreateUser(context.Background(), arg)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" {
				// 23505 --> Violated Unique Constraints
				// TODO: Make these constants
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "user already exists"})
				return
			}
			fmt.Println("pq error:", pqErr.Code.Name())
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sms := service.SmsNotification{
		Message:     "Your OTP is 5439",
		PhoneNumber: newUser.PhoneNumber,
		Config:      a.server.config,
	}

	err = sms.SendSMS()
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "error sending OTP"})
		return
	}

	ctx.JSON(http.StatusCreated, models.UserResponse{}.ToUserResponse(&newUser))
}
