package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	activitylogs "github.com/SwiftFiat/SwiftFiat-Backend/services/activity_logs"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/referral"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// Auth TODO: Register all services to be accessible from SERVER
type Auth struct {
	server          *Server
	userService     *user_service.UserService
	referralService *referral.Service
	refRepo         *referral.Repo
	activityTracker *activitylogs.ActivityLog
	notifr          *service.Notification
}

func (a Auth) router(server *Server) {
	a.server = server
	a.userService = user_service.NewUserService(
		a.server.queries,
		a.server.logger,
		wallet.NewWalletService(a.server.queries, a.server.logger),
	)
	a.refRepo = referral.NewReferralRepository(server.queries)
	a.notifr = service.NewNotificationService(server.queries)
	a.referralService = referral.NewReferralService(a.refRepo, a.server.logger, a.notifr)
	a.activityTracker = activitylogs.NewActivityLog(*a.server.queries)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/auth")
	serverGroupV1.GET("test", a.testAuth)
	serverGroupV1.POST("login", a.login)
	serverGroupV1.POST("login-passcode", a.loginWithPasscode)
	serverGroupV1.POST("register", a.register)
	serverGroupV1.POST("register-admin", a.registerAdmin)
	// serverGroupV1.GET("otp", a.server.authMiddleware.AuthenticatedMiddleware(), a.sendOTP)
	// serverGroupV1.POST("verify-otp", a.server.authMiddleware.AuthenticatedMiddleware(), a.verifyOTP)
	serverGroupV1.POST("change-password", a.server.authMiddleware.AuthenticatedMiddleware(), a.changePassword)
	serverGroupV1.POST("forgot-password", a.server.authMiddleware.AuthenticatedMiddleware(), a.forgotPassword)
	serverGroupV1.POST("reset-password", a.server.authMiddleware.AuthenticatedMiddleware(), a.resetPassword)
	serverGroupV1.POST("forgot-passcode", a.server.authMiddleware.AuthenticatedMiddleware(), a.forgotPasscode)
	serverGroupV1.POST("reset-passcode", a.server.authMiddleware.AuthenticatedMiddleware(), a.resetPasscode)
	serverGroupV1.POST("create-passcode", a.server.authMiddleware.AuthenticatedMiddleware(), a.createPasscode)
	serverGroupV1.POST("create-pin", a.server.authMiddleware.AuthenticatedMiddleware(), a.createPin)
	serverGroupV1.PUT("update-pin", a.server.authMiddleware.AuthenticatedMiddleware(), a.updateTransactionPin)
	serverGroupV1.GET("profile", a.server.authMiddleware.AuthenticatedMiddleware(), a.profile)
	serverGroupV1.GET("user", a.server.authMiddleware.AuthenticatedMiddleware(), a.getUserID)
	serverGroupV1.DELETE("account", a.server.authMiddleware.AuthenticatedMiddleware(), a.deleteAccount)
	serverGroupV1.POST("send-otp", a.server.authMiddleware.AuthenticatedMiddleware(), a.SendOTPWithTwilio)
	serverGroupV1.POST("verify-otp", a.server.authMiddleware.AuthenticatedMiddleware(), a.VerifyOTPWithTwilio)
	serverGroupV1.POST("verify-email", a.server.authMiddleware.AuthenticatedMiddleware(), a.verifyEmail)
	serverGroupV1.POST("resend-email", a.server.authMiddleware.AuthenticatedMiddleware(), a.resendEmailVerification)

	serverGroupV2 := server.router.Group("/api/v2/auth")
	serverGroupV2.GET("test", a.testAuth)
}

// / This is a test function for easy conversion from type ID -> dbID (i.e int64)
func (a *Auth) getUserID(ctx *gin.Context) {
	request := struct {
		Id models.ID `json:"id" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"userID": int64(request.Id)})
}

func (a Auth) testAuth(ctx *gin.Context) {
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Authentication API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

func (a *Auth) profile(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	dbUser, err := a.server.queries.GetUserByID(context.Background(), activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("user does not exist"))
		return
	}

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user retrieved successfully", models.UserResponse{}.ToUserResponse(&dbUser)))
}

func (a *Auth) login(ctx *gin.Context) {
	user := new(models.UserLoginParams)

	if err := ctx.ShouldBindJSON(user); err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidPhoneEmailInput))
		return
	}

	dbUser, err := a.userService.FetchUserByEmail(ctx, user.Email)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
		if err.Error() == user_service.ErrUserNotFound.Error() {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
			return
		}

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if err = utils.VerifyHashValue(user.Password, dbUser.HashedPassword.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.IncorrectEmailPass))
		return
	}

	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   dbUser.ID,
		Verified: dbUser.Verified,
		Role:     dbUser.Role,
	})
	if err != nil {
		a.server.logger.Log(logrus.DebugLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	a.server.redis.Set(ctx, fmt.Sprintf("user:%d", dbUser.ID), token, time.Hour*2400)

	if !dbUser.HasWallets {
		err := a.userService.CreateSwiftWalletForUser(ctx, dbUser.ID)
		if err != nil {
			a.server.logger.Error(err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to Instantiate User Wallets"))
			return
		}
	}

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(dbUser),
		Token: token,
	}

	err = a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(userWT.User.UserID),
		Action: fmt.Sprintf("User %s logged in ", dbUser.FirstName.String),
	})
	if err != nil {
		a.server.logger.Error(fmt.Sprintf("error logging activity - login: %v", err))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged in successfully", userWT))
}

func (a *Auth) loginWithPasscode(ctx *gin.Context) {
	user := new(models.UserPasscodeLoginParams)

	if err := ctx.ShouldBindJSON(user); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidCodeEmailInput))
		return
	}

	dbUser, err := a.userService.FetchUserByEmail(ctx, user.Email)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
		if err.Error() == user_service.ErrUserNotFound.Error() {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
			return
		}

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if err = utils.VerifyHashValue(user.Passcode, dbUser.HashedPasscode.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("incorrect email or passcode"))
		return
	}

	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   dbUser.ID,
		Verified: dbUser.Verified,
		Role:     dbUser.Role,
	})
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	a.server.redis.Set(ctx, fmt.Sprintf("user:%d", dbUser.ID), token, time.Hour*2400)

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(dbUser),
		Token: token,
	}

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(dbUser.ID),
		Action: fmt.Sprintf("User %s logged in with passcode ", dbUser.FirstName.String),
	})

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged in successfully", userWT))
}

func (a *Auth) register(ctx *gin.Context) {
	var user models.RegisterUserParams

	err := ctx.ShouldBindJSON(&user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	validate := validator.New()
	err = validate.Var(user.Email, "email")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidEmail))
		return
	}

	err = validate.Var(user.PhoneNumber, "e164")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidPhone))
		return
	}

	hashedPassword, err := utils.GenerateHashValue(user.Password)
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	email := service.Plunk{Config: a.server.config, HttpClient: &http.Client{Timeout: time.Second * 10}}

	// Generate verification code
	verificationCode := utils.GenerateOTP()

	// Save code to Redis
	redisKey := fmt.Sprintf("email_verification:%s", user.Email)
	a.server.redis.Set(ctx, redisKey, verificationCode, time.Minute*10)

	// Parse and execute the OTP template
	tplData := map[string]any{
		"Name": user.FirstName,
		"OTP":  verificationCode,
	}
	body, err := utils.RenderEmailTemplate("templates/otp_template_designed.html", tplData)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Server error"))
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

	newUser, err := a.userService.CreateUserWithWalletsAndKYC(ctx, &arg)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
		if userErr, ok := err.(*user_service.UserError); ok {
			if userErr.ErrorObj == user_service.ErrUserAlreadyExists {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserDetailsAlreadyCreated))
				return
			}
		}

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	referralKey := utils.GenerateRandomString("SWF-", newUser.ID, newUser.FirstName.String, newUser.LastName.String)
	_, err = a.userService.CreateUserReferral(ctx, newUser.ID, referralKey)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
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

	a.server.redis.Set(ctx, fmt.Sprintf("user:%d", newUser.ID), token, time.Hour*2400)

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(newUser),
		Token: token,
	}

	_, err = a.notifr.Create(ctx, int32(newUser.ID), "Welcome to SwiftFiat", fmt.Sprintf("Hello %s, welcome to SwiftFiat. Your referral code is %s", newUser.FirstName.String, referralKey))
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
	}

	subject := "SwiftFiat - Verify your email"
	a.server.logger.Info(fmt.Sprintf("Plunk send: to=%q, subject=%q, body-len=%d", user.Email, subject, len(body)))
	a.server.logger.Info(fmt.Sprintf("Plunk send: apikey=%q, secretkey=%q, baseurl=%q", a.server.config.PlunkApiKey, a.server.config.PlunkSecretKey, a.server.config.PlunkBaseUrl))
	err = email.SendEmail(user.Email, subject, body)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to send verification email: %v", err))
	}

	err = a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(newUser.ID),
		Action: fmt.Sprintf("User %s registered in ", newUser.FirstName.String),
	})
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
	}

	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("account created succcessfully", userWT))
}

type VerifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

func (a *Auth) verifyEmail(ctx *gin.Context) {
	var req VerifyEmailRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid request"))
		return
	}

	redisKey := fmt.Sprintf("email_verification:%s", req.Email)
	storedCode, err := a.server.redis.Get(ctx, redisKey)
	if err != nil || storedCode != req.Code {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid or expired verification code"))
		return
	}

	// Mark user as verified
	user, err := a.server.queries.GetUserByEmail(ctx, req.Email)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("User not found"))
		return
	}
	_, err = a.server.queries.UpdateUserVerification(ctx, db.UpdateUserVerificationParams{
		Verified:  true,
		UpdatedAt: time.Now(),
		ID:        user.ID,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Could not verify user"))
		return
	}

	// Optionally delete the code
	a.server.redis.Delete(ctx, redisKey)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Email verified successfully", nil))
}

func (a *Auth) registerAdmin(ctx *gin.Context) {
	var user models.RegisterAdminParams

	err := ctx.ShouldBindJSON(&user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	/// Validate Presence of Placeholder Values
	validate := validator.New(validator.WithRequiredStructEnabled())
	err = validate.Struct(user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashedPassword, err := utils.GenerateHashValue(user.Password)
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
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
		Role: models.ADMIN,
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
	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(newUser.ID),
		Action: fmt.Sprintf("User %s registered as admin ", newUser.FirstName.String),
	})

	ctx.JSON(http.StatusCreated, models.UserResponse{}.ToUserResponse(&newUser))
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
		Name:        user.FirstName.String,
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

func (a *Auth) resetPasscode(ctx *gin.Context) {
	passcode := new(models.ResetPasscodeParams)

	err := ctx.ShouldBindJSON(&passcode)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'passcode'"))
		return
	}

	dbUser, err := a.server.queries.GetUserByEmail(context.Background(), passcode.Email)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("email address does not exist"))
		return
	}

	dbOTP, err := a.server.queries.GetOTPByUserID(context.Background(), int32(dbUser.ID))
	if errors.Is(err, sql.ErrNoRows) {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	ok := utils.CompareOTP(passcode.OTP, utils.OTPObject{
		OTP:    dbOTP.Otp,
		Expiry: dbOTP.ExpiresAt,
	})
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	hashedPasscode, err := utils.GenerateHashValue(passcode.Code)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	updateParams := db.UpdateUserPasscodeeParams{
		HashedPasscode: sql.NullString{String: hashedPasscode, Valid: true},
		ID:             dbUser.ID,
	}

	user, err := a.server.queries.UpdateUserPasscodee(context.Background(), updateParams)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	/// Delete user token from redis
	a.server.redis.Delete(ctx, fmt.Sprintf("user:%d", dbUser.ID))

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(dbUser.ID),
		Action: fmt.Sprintf("User %s reset password ", dbUser.FirstName.String),
	})

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("passcode reset successful", userResponse))
}

func (a *Auth) changePassword(ctx *gin.Context) {
	newPassword := new(models.ChangePasswordParams)

	err := ctx.ShouldBindJSON(&newPassword)
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
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(user.ID),
		Action: fmt.Sprintf("User %s changed password ", user.FirstName.String),
	})

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("password changed successfully", userResponse))
}

func (a *Auth) createPasscode(ctx *gin.Context) {
	newPasscode := new(models.CreatePasscodeParams)

	err := ctx.ShouldBindJSON(&newPasscode)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'passcode'"))
		return
	}

	hashedPasscode, err := utils.GenerateHashValue(newPasscode.Passcode)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	updateParams := db.UpdateUserPasscodeeParams{
		HashedPasscode: sql.NullString{String: hashedPasscode, Valid: true},
		ID:             activeUser.UserID,
	}

	user, err := a.server.queries.UpdateUserPasscodee(context.Background(), updateParams)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(user.ID),
		Action: fmt.Sprintf("User %s created passcode ", user.FirstName.String),
	})

	a.notifr.Create(ctx, int32(user.ID), "Passcode Created", fmt.Sprintf("Hello %s, your passcode has been created successfully", user.FirstName.String))

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("passcode created successfully", userResponse))
}

func (a *Auth) forgotPasscode(ctx *gin.Context) {
	email := new(models.ForgotPasscodeParams)

	err := ctx.ShouldBindJSON(&email)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid email address"))
		return
	}

	user, err := a.server.queries.GetUserByEmail(ctx, email.Email)
	if err != nil {
		a.server.logger.Error(err.Error())
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("email address does not exist"))
			return
		}
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
		Name:        user.FirstName.String,
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

func (a *Auth) createPin(ctx *gin.Context) {
	newPin := new(models.CreatePinParams)

	err := ctx.ShouldBindJSON(&newPin)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'pin'"))
		return
	}

	hashedPin, err := utils.GenerateHashValue(newPin.Pin)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	updateParams := db.UpdateUserPinParams{
		HashedPin: sql.NullString{String: hashedPin, Valid: true},
		ID:        activeUser.UserID,
	}

	user, err := a.server.queries.UpdateUserPin(context.Background(), updateParams)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("user not found"))
		return
	} else if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(user.ID),
		Action: fmt.Sprintf("User %s created pin ", user.FirstName.String),
	})

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pin created successfully", userResponse))
}

func (a *Auth) updateTransactionPin(ctx *gin.Context) {
	pin := new(models.UpdateTransactionPinParams)

	err := ctx.ShouldBindJSON(&pin)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'pin'"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	dbUser, err := a.server.queries.GetUserByID(context.Background(), activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if err := utils.VerifyHashValue(pin.OldPin, dbUser.HashedPin.String); err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("old pin does not match"))
		return
	}

	hashedPin, err := utils.GenerateHashValue(pin.Pin)
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	updateParams := db.UpdateUserPinParams{
		HashedPin: sql.NullString{String: hashedPin, Valid: true},
		ID:        activeUser.UserID,
	}

	user, err := a.server.queries.UpdateUserPin(context.Background(), updateParams)
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(user.ID),
		Action: fmt.Sprintf("User %s updated transaction pin ", user.FirstName.String),
	})

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pin updated successfully", struct{}{}))
}

func (a *Auth) resetPassword(ctx *gin.Context) {
	resetPassword := new(models.ResetPasswordParams)

	err := ctx.ShouldBindJSON(&resetPassword)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a value for 'password'"))
		return
	}

	dbUser, err := a.server.queries.GetUserByEmail(context.Background(), resetPassword.Email)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("email address does not exist"))
		return
	}

	dbOTP, err := a.server.queries.GetOTPByUserID(context.Background(), int32(dbUser.ID))
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	ok := utils.CompareOTP(resetPassword.OTP, utils.OTPObject{
		OTP:    dbOTP.Otp,
		Expiry: dbOTP.ExpiresAt,
	})
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	if resetPassword.Password != resetPassword.ConfirmPassword {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("password and confirm password do not match"))
		return
	}

	hashedPassword, err := utils.GenerateHashValue(resetPassword.Password)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	updateParams := db.UpdateUserPasswordParams{
		HashedPassword: sql.NullString{String: hashedPassword, Valid: true},
		ID:             dbUser.ID,
	}

	user, err := a.server.queries.UpdateUserPassword(context.Background(), updateParams)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)
	/// Delete user token from redis
	a.server.redis.Delete(ctx, fmt.Sprintf("user:%d", dbUser.ID))

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("password reset successful", userResponse))
}

func (a *Auth) deleteAccount(ctx *gin.Context) {
	request := struct {
		Password string `json:"password" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter your 'password'"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}

	dbUser, err := a.server.queries.GetUserByID(context.Background(), activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	if err := utils.VerifyHashValue(request.Password, dbUser.HashedPassword.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("incorrect password"))
		return
	}

	a.activityTracker.Create(ctx, db.CreateActivityLogParams{
		UserID: int32(dbUser.ID),
		Action: fmt.Sprintf("User %s deleted account ", dbUser.FirstName.String),
	})

	_, err = a.server.queries.DeleteUser(context.Background(), db.DeleteUserParams{
		ID:          activeUser.UserID,
		PhoneNumber: dbUser.PhoneNumber + "DELETED",
		Email:       dbUser.Email + "DELETED",
		FirstName: sql.NullString{
			String: dbUser.FirstName.String + "DELETED",
			Valid:  dbUser.FirstName.Valid,
		},
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	a.server.redis.Delete(ctx, fmt.Sprintf("user:%d", activeUser.UserID))

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("account deleted successfully", struct{}{}))
}

type OTPRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
}

type VerifyRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Code        string `json:"code" binding:"required"`
}

func (a *Auth) SendOTPWithTwilio(c *gin.Context) {
	var req OTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		c.JSON(http.StatusBadRequest, basemodels.NewError("an error occurred, try again"))
		return
	}

	p := service.Twilio{Config: a.server.config}

	err := p.SendVerificationCode(req.PhoneNumber)
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to send OTP"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "OTP sent successfully"})
}

func (a *Auth) VerifyOTPWithTwilio(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(err.Error()))
		return
	}
	var req VerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		c.JSON(http.StatusBadRequest, basemodels.NewError("an error occurred, try again"))
		return
	}

	p := service.Twilio{Config: a.server.config}

	verified, err := p.CheckVerificationCode(req.PhoneNumber, req.Code)
	if err != nil || !verified {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid OTP"})
		return
	}

	updateUserParam := db.UpdateUserVerificationParams{
		Verified:  true,
		UpdatedAt: time.Now(),
		ID:        activeUser.UserID,
	}
	/// Update User verified status
	newUser, err := a.server.queries.UpdateUserVerification(c, updateUserParam)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred updating your Account %v", err.Error())))
		return
	}

	a.activityTracker.Create(c, db.CreateActivityLogParams{
		UserID: int32(newUser.ID),
		Action: fmt.Sprintf("User %s verified OTP ", newUser.FirstName.String),
	})
	a.notifr.Create(c, int32(newUser.ID), "Account", "Your account is verified successfully")
	c.JSON(http.StatusOK, basemodels.CustomResponse{Message: "OTP verified successfully"})
}

func (a *Auth) resendEmailVerification(ctx *gin.Context) {
    var req struct {
        Email string `json:"email" binding:"required,email"`
    }
    if err := ctx.ShouldBindJSON(&req); err != nil {
        ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid request"))
        return
    }

    // Check if user exists
    user, err := a.server.queries.GetUserByEmail(ctx, req.Email)
    if err != nil {
        ctx.JSON(http.StatusBadRequest, basemodels.NewError("User not found"))
        return
    }
    if user.Verified {
        ctx.JSON(http.StatusBadRequest, basemodels.NewError("Email already verified"))
        return
    }

    // Generate new verification code
    verificationCode := utils.GenerateOTP()
    redisKey := fmt.Sprintf("email_verification:%s", req.Email)
    a.server.redis.Set(ctx, redisKey, verificationCode, time.Minute*10)

    // Prepare email body
    tplData := map[string]any{
        "Name": user.FirstName.String,
        "OTP":  verificationCode,
    }
    body, err := utils.RenderEmailTemplate("templates/otp_template_designed.html", tplData)
    if err != nil {
        a.server.logger.Error(logrus.ErrorLevel, err.Error())
        ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Server error"))
        return
    }

    subject := "SwiftFiat - Verify your email"
    email := service.Plunk{Config: a.server.config, HttpClient: &http.Client{Timeout: time.Second * 10}}
    err = email.SendEmail(req.Email, subject, body)
    if err != nil {
        a.server.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to send verification email: %v", err))
        ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to send verification email"))
        return
    }

    ctx.JSON(http.StatusOK, basemodels.NewSuccess("Verification email resent successfully", nil))
}