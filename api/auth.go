package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/referral"
	"github.com/pquerna/otp/totp"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
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
	audit           *audit.Service
	notifr          *service.Notification
}

func (a Auth) router(server *Server) {
	a.server = server
	a.userService = a.server.userService
	a.refRepo = referral.NewReferralRepository(server.queries)
	a.notifr = a.server.inAppnotificationService
	a.referralService = referral.NewReferralService(a.refRepo, a.server.logger, a.notifr)
	a.audit = a.server.auditService

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
	serverGroupV1.POST("forgot-password", a.forgotPassword)
	serverGroupV1.POST("reset-password", a.server.authMiddleware.AuthenticatedMiddleware(), a.resetPassword)
	serverGroupV1.POST("forgot-passcode", a.server.authMiddleware.AuthenticatedMiddleware(), a.forgotPasscode)
	serverGroupV1.POST("reset-passcode", a.server.authMiddleware.AuthenticatedMiddleware(), a.resetPasscode)
	serverGroupV1.POST("create-passcode", a.server.authMiddleware.AuthenticatedMiddleware(), a.createPasscode)
	serverGroupV1.POST("create-pin", a.server.authMiddleware.AuthenticatedMiddleware(), a.createPin)
	serverGroupV1.POST("verify-pin", a.server.authMiddleware.AuthenticatedMiddleware(), a.verifyTransactionPin)
	serverGroupV1.PUT("update-pin", a.server.authMiddleware.AuthenticatedMiddleware(), a.updateTransactionPin)
	serverGroupV1.GET("profile", a.server.authMiddleware.AuthenticatedMiddleware(), a.profile)
	serverGroupV1.GET("user", a.server.authMiddleware.AuthenticatedMiddleware(), a.getUserID)
	serverGroupV1.DELETE("account", a.server.authMiddleware.AuthenticatedMiddleware(), a.deleteAccount)
	serverGroupV1.POST("send-otp", a.server.authMiddleware.AuthenticatedMiddleware(), a.SendOTPWithTwilio)
	serverGroupV1.POST("verify-otp", a.server.authMiddleware.AuthenticatedMiddleware(), a.VerifyOTPWithTwilio)
	serverGroupV1.POST("verify-email", a.server.authMiddleware.AuthenticatedMiddleware(), a.verifyEmail)
	serverGroupV1.POST("resend-email", a.server.authMiddleware.AuthenticatedMiddleware(), a.resendEmailVerification)
	serverGroupV1.POST("verify-admin-otp", a.VerifyAdminLoginOTP)
	serverGroupV1.POST("set-2fa", a.server.authMiddleware.AuthenticatedMiddleware(), a.SetTwoFA)
	serverGroupV1.POST("verify-2fa", a.verifyTwoFA)
	serverGroupV1.POST("logout", a.server.authMiddleware.AuthenticatedMiddleware(), a.logout)
	serverGroupV1.POST("logout-all", a.server.authMiddleware.AuthenticatedMiddleware(), a.logoutAll)

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

// testAuth godoc
// @Summary Test authentication endpoint
// @Description Test endpoint to verify authentication API is working
// @Tags auth
// @Accept json
// @Produce json
// @Success 200 {object} basemodels.SuccessResponse
// @Router /api/v1/auth/test [get]
func (a Auth) testAuth(ctx *gin.Context) {
	dr := basemodels.SuccessResponse{
		Status:  "success",
		Message: "Authentication API is active",
		Version: utils.REVISION,
	}

	ctx.JSON(http.StatusOK, dr)
}

// profile godoc
// @Summary Get user profile
// @Description Get the authenticated user's profile information
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/profile [get]
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

// login godoc
// @Summary User login [2fa UNTESTED]
// @Description Authenticate user with email and password
// @Description If 2FA is enabled, a temporary token will be returned for 2FA verification, if its a new device or no active session
// @Description If user is admin, an OTP will be sent to email for verification
// @Tags auth
// @Accept json
// @Produce json
// @Param user body models.UserLoginParams true "Login credentials"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserWithToken}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/login [post]
func (a *Auth) login(ctx *gin.Context) {
	user := new(models.UserLoginParams)

	if err := ctx.ShouldBindJSON(user); err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidPhoneEmailInput))
		return
	}

	// Fetch user from database
	dbUser, err := a.userService.FetchUserByEmail(ctx, user.Email)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
		if err.Error() == user_service.ErrUserNotFound.Error() {
			// Perform dummy hash to prevent timing attacks
			_ = utils.VerifyHashValue(user.Password, "$2a$10$CjwKljBvZBL1VZB7FZpE4eZzE4i9M7E3sVQxWnN0z6UQvD95z5o3G")
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Batch Redis reads using pipeline
	failedKey := fmt.Sprintf("failed_login:%s", user.Email)
	lastDeviceKey := fmt.Sprintf("user_device:%d", dbUser.ID)
	tokenKey := fmt.Sprintf("user:%d", dbUser.ID)

	pipe := a.server.redis.Pipeline()
	failedCountCmd := pipe.Get(ctx, failedKey)
	lastDeviceCmd := pipe.Get(ctx, lastDeviceKey)
	tokenDataCmd := pipe.Get(ctx, tokenKey)
	_, _ = pipe.Exec(ctx)

	// Parse Redis results
	failedCountStr, _ := failedCountCmd.Result()
	failedCount := 0
	if failedCountStr != "" {
		failedCount, _ = strconv.Atoi(failedCountStr)
	}
	lastDeviceData, _ := lastDeviceCmd.Result()
	tokenData, _ := tokenDataCmd.Result()

	// Verify password
	if err = utils.VerifyHashValue(user.Password, dbUser.HashedPassword.String); err != nil {
		// Handle failed login attempt
		failedCount++

		// Update Redis with new failed count
		pipe := a.server.redis.Pipeline()
		pipe.Set(ctx, failedKey, failedCount, 15*time.Minute)
		_, _ = pipe.Exec(ctx)

		// Send alert email asynchronously if threshold exceeded
		if failedCount > 3 {
			go func(u *db.User, count int, ip string) {
				defer func() { recover() }()
				err = a.server.emailService.SendFailedLoginAlert(u, count, ip)
				if err := a.server.emailService.SendFailedLoginAlert(u, count, ip); err != nil {
					a.server.logger.Warn(fmt.Sprintf("failed to send failed login alert: %v", err))
				}
			}(dbUser, failedCount, ctx.ClientIP())
		}

		// Log audit
		errMsg := apistrings.IncorrectEmailPass
		entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogin, fmt.Sprintf("User %s logged in", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.IncorrectEmailPass))
		return
	}

	// Successful password verification - check account status
	if !dbUser.IsActive {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.DeactivatedAccount))
		return
	}

	if !dbUser.Verified {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UserNotVerified))
		return
	}

	// Determine device and session status
	currentDevice := struct {
		IP        string
		UserAgent string
	}{
		IP:        ctx.ClientIP(),
		UserAgent: ctx.Request.UserAgent(),
	}
	currentDeviceStr := fmt.Sprintf("%s|%s", currentDevice.IP, currentDevice.UserAgent)
	isNewDevice := (lastDeviceData != currentDeviceStr)
	alreadyLoggedIn := (tokenData != "")

	// Determine if 2FA is required
	requiresTwoFA := (dbUser.TwofaEnabled.Bool && (isNewDevice || !alreadyLoggedIn)) ||
		(dbUser.Role != models.USER)

	// Handle 2FA requirement for users with 2FA enabled
	if dbUser.TwofaEnabled.Bool && requiresTwoFA {
		tmpToken, err := TokenController.CreateToken(utils.TokenObject{
			UserID: dbUser.ID,
		})
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Store temporary 2FA token
		if setErr := a.server.redis.Set(ctx, fmt.Sprintf("tmp2fa:%s", tmpToken), fmt.Sprintf("%d", dbUser.ID), 5*time.Minute); setErr != nil {
			a.server.logger.Error(fmt.Sprintf("redis set tmp2fa error: %v", setErr))
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Clear failed login attempts
		go a.server.redis.Delete(ctx, failedKey)

		// Send new device notification asynchronously if needed
		if isNewDevice {
			go a.server.emailService.SendNewDeviceAlert(dbUser, currentDevice)
		}

		ctx.JSON(http.StatusOK, gin.H{
			"message":        "2FA required",
			"twofa_required": true,
			"temp_token":     tmpToken,
		})
		return
	}

	// Handle admin OTP flow (for non-2FA admin users)
	if dbUser.Role != models.USER && !dbUser.TwofaEnabled.Bool {
		verificationCode := utils.GenerateOTP()
		redisKey := fmt.Sprintf("admin_login_otp:%s", user.Email)

		if err := a.server.redis.Set(ctx, redisKey, verificationCode, 10*time.Minute); err != nil {
			a.server.logger.Error(fmt.Sprintf("redis set admin OTP error: %v", err))
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}

		// Send OTP email asynchronously
		go a.server.emailService.SendAdminOTP(dbUser, user.Email, verificationCode)

		// Clear failed login attempts
		go a.server.redis.Delete(ctx, failedKey)

		ctx.JSON(http.StatusOK, basemodels.NewSuccess("admin OTP sent to email, please verify to continue", nil))
		return
	}

	// Generate final authentication token
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

	// Batch Redis writes using pipeline
	writePipe := a.server.redis.Pipeline()
	writePipe.Del(ctx, failedKey)                                        // Clear failed attempts
	writePipe.Set(ctx, lastDeviceKey, currentDeviceStr, 30*24*time.Hour) // Store device for 30 days
	writePipe.Set(ctx, tokenKey, token, 72*time.Hour)                    // Store token for 72 hours
	if _, err := writePipe.Exec(ctx); err != nil {
		a.server.logger.Error(fmt.Sprintf("redis pipeline error: %v", err))
		// Continue with login despite Redis errors (degraded mode)
	}

	// Prepare response
	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(dbUser),
		Token: token,
	}

	// Async operations (non-blocking)
	go func() {
		// Send new device notification
		if isNewDevice {
			a.server.emailService.SendNewDeviceAlert(dbUser, currentDevice)
		}

		// Create wallets if needed
		if !dbUser.HasWallets {
			if err := a.userService.CreateSwiftWalletForUser(context.Background(), dbUser.ID); err != nil {
				a.server.logger.Error(fmt.Sprintf("failed to create wallets: %v", err))
			}
		}
	}()

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogin, fmt.Sprintf("User %s logged in", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged in successfully", userWT))
}

type TwoFARequest struct {
	Enable bool `json:"enable"`
}

type TwoFAResponse struct {
	// OTPAuthURL is the URL used to generate the QR code for 2FA setup
	OTPAuthURL string `json:"otp_auth_url"`
	// Secret is the secret key used for generating TOTP codes
	Secret string `json:"secret"`
	QRCode string `json:"qr_code"`
}

// SetTwoFA godoc
// @Summary Set Two-Factor Authentication (2FA) [UNTESTED]
// @Description Enable or disable two-factor authentication for the authenticated user
// @Description When enabling 2FA, an OTPAuthURL and Secret will be returned for setting up the authenticator app
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param twoFARequest body TwoFARequest true "Two-Factor Authentication Request"
// @Success 200 {object} basemodels.SuccessResponse{data=TwoFAResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/set-2fa [post]
func (a *Auth) SetTwoFA(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	var req TwoFARequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please provide a valid request"))
		return
	}

	if activeUser.Role != models.USER {
		ctx.JSON(http.StatusForbidden, apistrings.UnauthorizedAccess)
		return
	}

	user, err := a.server.queries.GetUserByID(ctx, int64(activeUser.UserID))
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred retrieving user"))
		return
	}

	if req.Enable {
		if user.TwofaEnabled.Bool && user.TwofaEnabled.Valid {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("2FA is already enabled"))
			return
		}

		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "SwiftFiat",
			AccountName: user.Email,
		})
		if err != nil {
			a.server.logger.Error(err.Error())
			ctx.JSON(http.StatusInternalServerError, apistrings.ServerError)
			return
		}

		_, err = a.server.queries.SetUserTwoFA(ctx, db.SetUserTwoFAParams{
			ID:           int64(activeUser.UserID),
			TwofaSecret:  sql.NullString{String: key.Secret(), Valid: true},
			TwofaEnabled: sql.NullBool{Bool: true, Valid: true},
			UpdatedAt:    time.Now(),
		})
		if err != nil {
			a.server.logger.Error(err.Error())

			// Log audit
			errMsg := err.Error()
			entry := audit.NewAuthenticationLog(ctx, audit.Event2FAEnabled, fmt.Sprintf("User %s enabled 2FA", user.Email), &user.ID, &user.Email, user.Role, false, &errMsg)
			a.audit.Log(entry)

			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred enabling 2FA"))
			return
		}

		// Log 2FA setup attempt
		// Log audit
		entry := audit.NewAuthenticationLog(ctx, audit.Event2FAEnabled, fmt.Sprintf("User %s enabled 2FA", user.Email), &user.ID, &user.Email, user.Role, true, nil)
		a.audit.Log(entry)

		img, err := key.Image(200, 200)
		if err != nil {
			a.server.logger.Error(err.Error())
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
			return
		}

		var buf bytes.Buffer
		png.Encode(&buf, img)
		encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

		ctx.JSON(http.StatusOK, basemodels.NewSuccess("2FA enabled successfully", TwoFAResponse{
			OTPAuthURL: key.URL(),
			Secret:     key.Secret(),
			QRCode:     "data:image/png;base64," + encoded,
		}))

		return
	}

	// Disable 2FA
	updatedUser, err := a.server.queries.SetUserTwoFA(ctx, db.SetUserTwoFAParams{
		ID:           int64(activeUser.UserID),
		TwofaSecret:  sql.NullString{Valid: false},
		TwofaEnabled: sql.NullBool{Bool: false, Valid: true},
		UpdatedAt:    time.Now(),
	})
	if err != nil {
		a.server.logger.Error(err.Error())

		// Log audit
		errMsg := err.Error()
		entry := audit.NewAuthenticationLog(ctx, audit.Event2FAEnabled, fmt.Sprintf("User %s disabled 2FA", user.Email), &user.ID, &user.Email, user.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred disabling 2FA"))
		return
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.Event2FADisabled, fmt.Sprintf("User %s disabled 2FA", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("2FA disabled successfully", models.UserResponse{}.ToUserResponse(&updatedUser)))
}

type VerifyTwoFARequest struct {
	Code      string `json:"code" binding:"required"`
	TempToken string `json:"temp_token" binding:"required"`
}

// verifyTwoFA godoc
// @Summary Verify Two-Factor Authentication (2FA) code [NEW]
// @Description Verify the provided 2FA code for the authenticated user
// @Description On successful verification, a main session token will be issued
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param verifyTwoFARequest body VerifyTwoFARequest true "Verify 2FA Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserWithToken}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/verify-2fa [post]
func (a *Auth) verifyTwoFA(ctx *gin.Context) {
	var req VerifyTwoFARequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, "invalid input")
		return
	}

	// get user ID from temporary token
	userIDStr, err := a.server.redis.Get(ctx, fmt.Sprintf("tmp2fa:%s", req.TempToken))
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid or expired token"))
		return
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, apistrings.ServerError)
		return
	}

	user, err := a.server.queries.GetUserByID(ctx, int64(userID))
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, apistrings.ServerError)
		return
	}

	if user.TwofaSecret.String == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("2FA is not enabled for this user"))
		return
	}

	// Validate the provided TOTP code
	valid := totp.Validate(req.Code, user.TwofaSecret.String)
	if !valid {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid 2FA code"))
		return
	}

	// Create main token
	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   user.ID,
		Verified: user.Verified,
		Role:     user.Role,
	})
	if err != nil {
		a.server.logger.Log(logrus.DebugLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Clean up temp token and set main token
	pipe := a.server.redis.Pipeline()
	pipe.Del(ctx, fmt.Sprintf("temp_2fa:%s", req.TempToken))            // Delete temp token
	pipe.Set(ctx, fmt.Sprintf("user:%d", user.ID), token, 72*time.Hour) // Store token for 72 hours
	if _, err := pipe.Exec(ctx); err != nil {
		a.server.logger.Error(fmt.Sprintf("redis pipeline error: %v", err))
	}

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(&user),
		Token: token,
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.Event2FAVerified, fmt.Sprintf("User %s verified 2FA", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged in successfully", userWT))
}

// logout godoc
// @Summary User logout
// @Description Logs the user out by deleting their active session
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/logout [post]
func (a *Auth) logout(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Delete the token from Redis
	tokenKey := fmt.Sprintf("user:%d", activeUser.UserID)
	if err := a.server.redis.Delete(ctx, tokenKey); err != nil {
		a.server.logger.Error(fmt.Sprintf("redis delete token error: %v", err))

		// Log audit
		errMsg := err.Error()
		entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogout, fmt.Sprintf("User %d logged out", activeUser.UserID), &activeUser.UserID, nil, activeUser.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogout, fmt.Sprintf("User %d logged out", activeUser.UserID), &activeUser.UserID, nil, activeUser.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged out successfully", nil))
}

// logoutAll godoc
// @Summary Logout from all devices
// @Description Logs the user out from all devices by deleting all active sessions
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/logout-all [post]
func (a *Auth) logoutAll(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	// Keys to delete
	tokenKey := fmt.Sprintf("user:%d", activeUser.UserID)
	deviceKey := fmt.Sprintf("user_device:%d", activeUser.UserID)

	// Use pipeline for atomic deletion
	pipe := a.server.redis.Pipeline()
	pipe.Del(ctx, tokenKey)
	pipe.Del(ctx, deviceKey)

	if _, err := pipe.Exec(ctx); err != nil {
		a.server.logger.Error(fmt.Sprintf("redis pipeline error during logout all: %v", err))

		// Log audit
		errMsg := err.Error()
		entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogoutAllDevices, fmt.Sprintf("User %d logged out from all devices", activeUser.UserID), &activeUser.UserID, nil, activeUser.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogoutAllDevices, fmt.Sprintf("User %d logged out from all devices", activeUser.UserID), &activeUser.UserID, nil, activeUser.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("logged out from all devices successfully", nil))
}

type VerifyAdminOTPRequest struct {
	Email string `json:"email" binding:"required,email"`
	OTP   string `json:"otp" binding:"required"`
}

// verifyAdminLoginOTP godoc
// @Summary Verify admin login OTP [NEW]
// @Description Verify OTP for admin login
// @Tags auth
// @Accept json
// @Produce json
// @Param data body VerifyAdminOTPRequest true "verify admin OTP request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserWithToken}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/verify-admin-otp [post]
func (a *Auth) VerifyAdminLoginOTP(ctx *gin.Context) {
	var req VerifyAdminOTPRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid request"))
		return
	}

	redisKey := fmt.Sprintf("admin_login_otp:%s", req.Email)
	storedCode, err := a.server.redis.Get(ctx, redisKey)
	if err != nil || storedCode != req.OTP {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid or expired OTP"))
		return
	}

	dbUser, err := a.userService.FetchUserByEmail(ctx, req.Email)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
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
	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(dbUser),
		Token: token,
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogin, fmt.Sprintf("User %s logged in", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, true, nil)
	a.audit.Log(entry)

	a.server.redis.Delete(ctx, redisKey)
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("admin logged in successfully", userWT))
}

// loginWithPasscode godoc
// @Summary User login with passcode
// @Description Authenticate user with email and passcode
// @Tags auth
// @Accept json
// @Produce json
// @Param user body models.UserPasscodeLoginParams true "Login credentials"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserWithToken}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/login-passcode [post]
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

		// Log audit
		errMsg := "incorrect email or passcode"
		entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogin, fmt.Sprintf("User %s logged in", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusBadRequest, basemodels.NewError(errMsg))
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

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserLogin, fmt.Sprintf("User %s logged in", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user logged in successfully", userWT))
}

// register godoc
// @Summary User registration
// @Description Register a new user account
// @Tags auth
// @Accept json
// @Produce json
// @Param user body models.RegisterUserParams true "User registration data"
// @Success 201 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/register [post]
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

	// Generate verification code
	verificationCode := utils.GenerateOTP()

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

	// Generate referral key
	referralKey, err := utils.GenerateReferralCode("SWF")
	if err != nil {
		a.server.logger.Error(fmt.Sprintf("failed to generate referral code: %v", err))
	}

	// Create user referral
	_, err = a.userService.CreateUserReferral(ctx, newUser.ID, referralKey)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, fmt.Sprintf("failed to create referral for user %d: %v", newUser.ID, err))
	}

	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   newUser.ID,
		Verified: newUser.Verified,
		Role:     newUser.Role,
	})
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, fmt.Sprintf("failed to create token, login with your details instead: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Store verification code and token in Redis using pipeline
	pipe := a.server.redis.Pipeline()
	verificationKey := fmt.Sprintf("email_verification:%s", user.Email)
	tokenKey := fmt.Sprintf("user:%d", newUser.ID)
	pipe.Set(ctx, verificationKey, verificationCode, 10*time.Minute)
	pipe.Set(ctx, tokenKey, token, 72*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		a.server.logger.Error(fmt.Sprintf("redis pipeline error: %v", err))
		// Continue - Redis is cache, not source of truth
	}

	userWT := models.UserWithToken{
		User:  models.UserResponse{}.ToUserResponse(newUser),
		Token: token,
	}

	// Handle all post-registration tasks asynchronously
	go func() {
		bgCtx := context.Background()

		// Send verification email
		a.server.emailService.SendVerificationEmail(newUser, user.Email, verificationCode)

		// Create welcome notification
		title := "Welcome to SwiftFiat"
		message := fmt.Sprintf("Hello %s, welcome to SwiftFiat. Your referral code is %s", newUser.FirstName.String, referralKey)
		if _, err := a.notifr.Create(bgCtx, int32(newUser.ID), title, message); err != nil {
			a.server.logger.Error(fmt.Sprintf("failed to create welcome notification for user %d: %v", newUser.ID, err))
		}
	}()

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserRegistered, fmt.Sprintf("User %s registered", newUser.Email), &newUser.ID, &newUser.Email, newUser.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("account created succcessfully", userWT))
}

type VerifyEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

// verifyEmail godoc
// @Summary Verify user email
// @Description Verifies a user's email address using a verification code sent to their email
// @Tags auth
// @Accept json
// @Produce json
// @Param data body VerifyEmailRequest true "verification request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/verify-email [post]
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

		// Log audit
		errMsg := err.Error()
		entry := audit.NewAuthenticationLog(ctx, audit.EventEmailVerified, fmt.Sprintf("User %s verified email", user.Email), &user.ID, &user.Email, user.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Could not verify user"))
		return
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventEmailVerified, fmt.Sprintf("User %s verified email", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	a.server.redis.Delete(ctx, redisKey)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Email verified successfully", nil))
}

// ResendEmailRequest is used for resending verification emails.
type ResendEmailRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// resendEmailVerification godoc
// @Summary Resend email verification code
// @Description Sends a new email verification code to the user's email address
// @Tags auth
// @Accept json
// @Produce json
// @Param data body ResendEmailRequest true "email to resend code to"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/resend-email [post]
func (a *Auth) resendEmailVerification(ctx *gin.Context) {
	req := ResendEmailRequest{}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid request"))
		return
	}

	user, err := a.server.queries.GetUserByEmail(ctx, req.Email)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("User not found"))
		return
	}
	if user.Verified {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Email already verified"))
		return
	}

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

const (
	PostgresUniqueViolation = "23505"
)

type AdminRegistrationResponse struct {
	User          models.UserResponse `json:"user"`
	Token         string              `json:"token"`
	TwoFASecret   string              `json:"twofa_secret"`
	TwoFAQRCode   string              `json:"twofa_qr_code"`
	TwoFASetupURL string              `json:"twofa_setup_url"`
}

// registerAdmin godoc
// @Summary Admin registration
// @Description Register a new admin account. Requires a valid admin key.
// @Tags auth
// @Accept json
// @Produce json
// @Param user body models.RegisterAdminParams true "Admin registration data"
// @Success 201 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/register-admin [post]
func (a *Auth) registerAdmin(ctx *gin.Context) {
	var user models.RegisterAdminParams

	err := ctx.ShouldBindJSON(&user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidRequestData))
		return
	}

	/// Validate Presence of Placeholder Values
	validate := validator.New(validator.WithRequiredStructEnabled())
	err = validate.Struct(user)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// Rate limit admin registration attempts by IP
	rateLimitKey := fmt.Sprintf("admin_reg_attempt:%s", ctx.ClientIP())
	attempts, err := a.server.redis.Incr(ctx, rateLimitKey)
	if err == nil && attempts == 1 {
		a.server.redis.Expire(ctx, rateLimitKey, 1*time.Hour)
	}

	if attempts > 5 {
		a.server.logger.Warn(fmt.Sprintf("excessive admin registration attempts from IP: %s", ctx.ClientIP()))
		ctx.JSON(http.StatusTooManyRequests, basemodels.NewError("too many registration attempts, try again later"))
		return
	}

	role, ok := models.RoleKeys[user.AdminKey]
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid admin key"))
		return
	}

	// Validate role is actually an admin role
	if role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("invalid admin key"))
		return
	}

	hashedPassword, err := utils.GenerateHashValue(user.Password)
	if err != nil {
		a.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Generate TOTP secret for 2FA
	totpKey, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "SwiftFiat",
		AccountName: user.Email,
		SecretSize:  32,
	})
	if err != nil {
		a.server.logger.Error(fmt.Sprintf("failed to generate TOTP key: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Start database transaction for atomic operations
	tx, err := a.server.queries.DB.BeginTx(ctx, nil)
	if err != nil {
		a.server.logger.Error(fmt.Sprintf("failed to begin transaction: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	defer tx.Rollback()

	// Create queries with transaction
	qtx := a.server.queries.WithTx(tx)

	arg := db.CreateUserParams{
		FirstName:   sql.NullString{String: user.FirstName, Valid: true},
		LastName:    sql.NullString{String: user.LastName, Valid: true},
		Email:       user.Email,
		PhoneNumber: user.PhoneNumber,
		HashedPassword: sql.NullString{
			Valid:  true,
			String: hashedPassword,
		},
		Role: role,
	}

	newUser, err := qtx.CreateUser(context.Background(), arg)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == PostgresUniqueViolation {
				ctx.JSON(http.StatusConflict, basemodels.NewError(apistrings.UserDetailsAlreadyCreated))
				return
			}
			a.server.logger.Error(fmt.Sprintf("postgres error during admin registration: %s - %v", pqErr.Code.Name(), err))
		} else {
			a.server.logger.Error(fmt.Sprintf("admin registration error: %v", err))
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	a.server.queries.UpdateUserVerification(ctx, db.UpdateUserVerificationParams{
		Verified:  true,
		UpdatedAt: time.Now(),
		ID:        newUser.ID,
	})

	a.server.queries.UpdateUserVerification(ctx, db.UpdateUserVerificationParams{
		Verified:  true,
		UpdatedAt: time.Now(),
		ID:        newUser.ID,
	})

	// Enable 2FA for admin user with TOTP secret
	if _, err := qtx.SetUserTwoFA(ctx, db.SetUserTwoFAParams{
		ID:           int64(newUser.ID),
		TwofaEnabled: sql.NullBool{Bool: true, Valid: true},
		TwofaSecret:  sql.NullString{String: totpKey.Secret(), Valid: true},
		UpdatedAt:    time.Now(),
	}); err != nil {
		a.server.logger.Error(fmt.Sprintf("failed to enable 2FA for admin: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		a.server.logger.Error(fmt.Sprintf("failed to commit admin registration transaction: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Update user object with verified status and 2FA enabled
	newUser.Verified = true
	newUser.TwofaEnabled = sql.NullBool{Bool: true, Valid: true}

	// Generate authentication token
	token, err := TokenController.CreateToken(utils.TokenObject{
		UserID:   newUser.ID,
		Verified: true,
		Role:     newUser.Role,
	})
	if err != nil {
		a.server.logger.Error(fmt.Sprintf("failed to create token for admin: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Store token in Redis
	tokenKey := fmt.Sprintf("user:%d", newUser.ID)
	if err := a.server.redis.Set(ctx, tokenKey, token, 72*time.Hour); err != nil {
		a.server.logger.Error(fmt.Sprintf("redis set token error: %v", err))
		// Continue - Redis is cache, not source of truth
	}

	// Generate totpKey QRcode
	img, err := totpKey.Image(200, 200)
	if err != nil {
		a.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	qrcode := "data:image/png;base64," + encoded

	// Log activity asynchronously
	go func() {
		// Send notification email to admin about successful registration
		a.server.emailService.SendAdminRegistrationEmail(&newUser, totpKey.Secret(), qrcode, totpKey.URL())
	}()

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventUserRegistered, fmt.Sprintf("User %s registered", newUser.Email), &newUser.ID, &newUser.Email, newUser.Role, true, nil)
	a.audit.Log(entry)

	// Prepare response with 2FA setup information
	response := AdminRegistrationResponse{
		User:          *models.UserResponse{}.ToUserResponse(&newUser),
		Token:         token,
		TwoFASecret:   totpKey.Secret(),
		TwoFAQRCode:   qrcode,
		TwoFASetupURL: totpKey.URL(),
	}
	ctx.JSON(http.StatusCreated, basemodels.NewSuccess("admin account created successfully - please save your 2FA secret", response))
}

// forgotPassword godoc
// @Summary Forgot password
// @Description Initiate password reset process by sending an OTP to the user's email
// @Tags auth
// @Accept json
// @Produce json
// @Param data body models.ForgotPasswordParams true "forgot password request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/forgot-password [post]
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

		// Log audit
		errMsg := err.Error()
		entry := audit.NewAuthenticationLog(ctx, audit.EventPasswordResetRequested, fmt.Sprintf("User %s requested password reset", user.Email), &user.ID, &user.Email, user.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("error sending OTP please try again later"))
		return
	}

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventPasswordResetRequested, fmt.Sprintf("User %s requested password reset", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("OTP Sent successfully to your %v", em.Channel), struct{}{}))
}

// resetPassword godoc
// @Summary Reset passcode
// @Description Reset user's password using the provided OTP and new password
// @Tags auth
// @Accept json
// @Produce json
// @Param data body models.ResetPasswordParams true "reset password request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/reset-passcode [post]
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
		errMsg := err.Error()

		// Log audit
		entry := audit.NewAuthenticationLog(ctx, audit.EventPasscodeChanged, fmt.Sprintf("User %s changed passcode", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, false, &errMsg)
		a.audit.Log(entry)

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	/// Delete user token from redis
	a.server.redis.Delete(ctx, fmt.Sprintf("user:%d", dbUser.ID))

	// Log audit
	entry := audit.NewAuthenticationLog(ctx, audit.EventPasscodeChanged, fmt.Sprintf("User %s changed passcode", dbUser.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("passcode reset successful", userResponse))
}

// changePassword godoc
// @Summary Change password
// @Description Change the authenticated user's password
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param data body models.ChangePasswordParams true "change password request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/change-password [post]
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

	email := service.Plunk{Config: a.server.config, HttpClient: &http.Client{Timeout: time.Second * 10}}

	tplData := map[string]any{
		"FirstName": user.FirstName.String,
		"Field":     "Password",
		"Timestamp": time.Now().Format("02 Jan 2006 15:04 MST"),
		"Year":      time.Now().Year(),
	}
	body, err := utils.RenderEmailTemplate("templates/account_update.html", tplData)
	if err != nil {
		a.server.logger.Error(logrus.ErrorLevel, err.Error())
	} else {
		subject := "SwiftFiat - Account Password Changed"
		err = email.SendEmail(user.Email, subject, body)
		if err != nil {
			a.server.logger.Error(logrus.ErrorLevel, fmt.Sprintf("Failed to send change password email: %v", err))
		}
	}

	userResponse := models.UserResponse{}.ToUserResponse(&user)

	// audit log
	entry := audit.NewAuthenticationLog(ctx, audit.EventPasswordChanged, fmt.Sprintf("User %s changed password", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("password changed successfully", userResponse))
}

// createPasscode godoc
// @Summary Create passcode
// @Description Create a new passcode for the authenticated user
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param data body models.CreatePasscodeParams true "create passcode request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/create-passcode [post]
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

	entry := audit.NewAuthenticationLog(ctx, audit.EventPasscodeCreated, fmt.Sprintf("User %s created passcode", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	a.notifr.Create(ctx, int32(user.ID), "Passcode Created", fmt.Sprintf("Hello %s, your passcode has been created successfully", user.FirstName.String))

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("passcode created successfully", userResponse))
}

// forgotPasscode godoc
// @Summary Forgot passcode
// @Description Initiate passcode reset process by sending an OTP to the user's email
// @Tags auth
// @Accept json
// @Produce json
// @Param data body models.ForgotPasscodeParams true "forgot passcode request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/forgot-passcode [post]
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

// createPin godoc
// @Summary Create transaction pin
// @Description Create a new transaction pin for the authenticated user
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param data body models.CreatePinParams true "create pin request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/create-pin [post]
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

	entry := audit.NewAuthenticationLog(ctx, audit.EventPinCreated, fmt.Sprintf("User %s created pin", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pin created successfully", userResponse))
}

// verifyTransactionPin godoc
// @Summary Verify transaction pin
// @Description Verify the authenticated user's transaction pin
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param data body object{pin=string} true "verify pin request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/verify-pin [post]
func (a *Auth) verifyTransactionPin(ctx *gin.Context) {
	req := struct {
		Pin string `json:"pin" binding:"required"`
	}{}

	if err := ctx.ShouldBindJSON(&req); err != nil {
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

	// Verify provided pin against stored hashed pin
	if err := utils.VerifyHashValue(req.Pin, dbUser.HashedPin.String); err != nil {
		a.server.logger.Error(fmt.Sprintf("pin verification failed for user %d: %v", dbUser.ID, err))
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("invalid pin"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pin verified successfully", struct{}{}))
}

// updateTransactionPin godoc
// @Summary Update transaction pin
// @Description Update the authenticated user's transaction pin
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param data body models.UpdateTransactionPinParams true "update pin request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/update-pin [post]
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

	entry := audit.NewAuthenticationLog(ctx, audit.EventPinChanged, fmt.Sprintf("User %s changed pin", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("pin updated successfully", struct{}{}))
}

// resetPassword godoc
// @Summary Reset password
// @Description Reset user's password using the provided OTP and new password
// @Tags auth
// @Accept json
// @Produce json
// @Param data body models.ResetPasswordParams true "reset password request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/reset-password [post]
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

	entry := audit.NewAuthenticationLog(ctx, audit.EventPasswordChanged, fmt.Sprintf("User %s changed password", user.Email), &user.ID, &user.Email, user.Role, true, nil)
	a.audit.Log(entry)

	userResponse := models.UserResponse{}.ToUserResponse(&user)
	/// Delete user token from redis
	a.server.redis.Delete(ctx, fmt.Sprintf("user:%d", dbUser.ID))

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("password reset successful", userResponse))
}

// DeleteAccountRequest is the request payload for deleting an account.
type DeleteAccountRequest struct {
	Password string `json:"password" binding:"required"`
}

// deleteAccount godoc
// @Summary Delete account
// @Description Delete the authenticated user's account
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param data body DeleteAccountRequest true "delete account request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/account [delete]
func (a *Auth) deleteAccount(ctx *gin.Context) {
	request := DeleteAccountRequest{}

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

	entry := audit.NewAuthenticationLog(ctx, audit.EventAccountDeleted, fmt.Sprintf("User %s deleted account", dbUser.Email), &dbUser.ID, &dbUser.Email, dbUser.Role, true, nil)
	a.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("account deleted successfully", nil))
}

type OTPRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
}

type VerifyRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Code        string `json:"code" binding:"required"`
}

// sendOTPWithTwilio godoc
// @Summary Send OTP via Twilio
// @Description Sends a One-Time Password (OTP) to the specified phone number using Twilio
// @Tags auth
// @Accept json
// @Produce json
// @Param data body OTPRequest true "OTP request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/send-otp [post]
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

// verifyOTPWithTwilio godoc
// @Summary Verify OTP via Twilio
// @Description Verifies a One-Time Password (OTP) sent to the specified phone number using Twilio
// @Tags auth
// @Accept json
// @Produce json
// @Param data body VerifyRequest true "verification request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/auth/verify-otp [post]
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

	a.notifr.Create(c, int32(newUser.ID), "Account", "Your account is verified successfully")
	c.JSON(http.StatusOK, basemodels.CustomResponse{Message: "OTP verified successfully"})
}
