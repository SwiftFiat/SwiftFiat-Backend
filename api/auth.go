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
	"github.com/go-playground/validator/v10"
	"github.com/lib/pq"
)

type Auth struct {
	server *Server
}

func (a Auth) router(server *Server) {
	a.server = server

	serverGroup := server.router.Group("/auth")
	serverGroup.GET("test", a.testAuth)
	serverGroup.POST("login", a.login)
	serverGroup.POST("register", a.register)
	serverGroup.POST("register-admin", a.registerAdmin)
	serverGroup.GET("otp", AuthenticatedMiddleware(), a.sendOTP)
	serverGroup.POST("verify-otp", AuthenticatedMiddleware(), a.verifyOTP)
	serverGroup.POST("forgot-password", a.forgotPassword)
	serverGroup.POST("verify-otp-password", a.verifyOTPPassword)
	serverGroup.POST("change-password", AuthenticatedMiddleware(), a.changePassword)
}

func (a Auth) testAuth(ctx *gin.Context) {
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Authentication API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

func (a *Auth) login(ctx *gin.Context) {
	user := new(models.UserLoginParams)

	if err := ctx.ShouldBindJSON(user); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dbUser, err := a.server.queries.GetUserByEmail(context.Background(), user.Email)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect Email or Password"})
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err = utils.VerifyHashValue(user.Password, dbUser.HashedPassword.String); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect Email or Password"})
		return
	}

	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   dbUser.ID,
		Verified: dbUser.Verified,
		Role:     dbUser.Role,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(&dbUser),
		Token: token,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged in successfully", userWT))
}

func (a *Auth) register(ctx *gin.Context) {
	var user models.RegisterUserParams

	err := ctx.ShouldBindJSON(&user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	hashedPassword, err := utils.GenerateHashValue(user.Password)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	arg := db.CreateUserParams{
		FirstName:   sql.NullString{String: user.FirstName, Valid: true},
		LastName:    sql.NullString{String: user.LastName, Valid: true},
		Email:       user.Email,
		PhoneNumber: user.PhoneNumber,
		HashedPassword: sql.NullString{
			Valid:  true,
			String: hashedPassword,
		},
		Role: models.USER,
	}

	newUser, err := a.server.queries.CreateUser(context.Background(), arg)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == db.DuplicateEntry {
				// 23505 --> Violated Unique Constraints
				// TODO: Make these constants
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("user already exists"))
				return
			}
			fmt.Println("pq error:", pqErr.Code.Name())
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   newUser.ID,
		Verified: newUser.Verified,
		Role:     newUser.Role,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(&newUser),
		Token: token,
	}

	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("account created succcessfully", userWT))
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

	ctx.JSON(http.StatusCreated, models.UserResponse{}.ToUserResponse(&newUser))
}

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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("account status verified successfully", (models.UserResponse{}.ToUserResponse(&newUser))))
}

func (a *Auth) forgotPassword(ctx *gin.Context) {
	email := new(models.ForgotPasswordParams)

	err := ctx.ShouldBindJSON(&email)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("please provide a valid email address"))
		return
	}

	user, err := a.server.queries.GetUserByEmail(context.Background(), email.Email)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("email address does not exist"))
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
		log.Default().Output(6, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("error sending OTP please try again later"))
		return
	}
	ctx.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("OTP Sent successfully to your %v", em.Channel), struct{}{}))
}

func (a *Auth) verifyOTPPassword(ctx *gin.Context) {
	verifyInfo := new(models.VerifyOTPPasswordParams)

	err := ctx.ShouldBindJSON(&verifyInfo)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid OTP and Email"))
		return
	}

	//. Get User's information
	activeUser, err := a.server.queries.GetUserByEmail(context.Background(), verifyInfo.Email)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("email address does not exist"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	//. Get OTP Information from User
	dbOTP, err := a.server.queries.GetOTPByUserID(context.Background(), int32(activeUser.ID))
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred verifying your OTP %v", err.Error())))
		return
	}

	/// If User OTP is Expired --> Returns false
	ok := utils.CompareOTP(verifyInfo.OTP, utils.OTPObject{
		OTP:    dbOTP.Otp,
		Expiry: dbOTP.ExpiresAt,
	})
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   activeUser.ID,
		Verified: activeUser.Verified,
		Role:     activeUser.Role,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred with token generation"))
		return
	}

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(&activeUser),
		Token: token,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("password reset successful", userWT))
}

func (a *Auth) changePassword(ctx *gin.Context) {
	newPassword := new(models.ChangePasswordParams)

	err := ctx.BindJSON(&newPassword)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'password'"))
		return
	}

	hashedPassword, err := utils.GenerateHashValue(newPassword.Password)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	updateParams := db.UpdateUserPasswordParams{
		HashedPassword: sql.NullString{String: hashedPassword, Valid: true},
		ID:             activeUser.UserID,
	}

	user, err := a.server.queries.UpdateUserPassword(context.Background(), updateParams)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("user not found"))
		return
	} else if err != nil {
		log.Default().Output(6, fmt.Sprintf("error: %v", err.Error()))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("error performing password update"))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("password changed successfully", userResponse))
}
