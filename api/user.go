package api

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	bankaccounts "github.com/SwiftFiat/SwiftFiat-Backend/services/bank_accounts"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

type User struct {
	server             *Server
	walletService      *wallet.WalletService
	userService        *user_service.UserService
	bankAccountService *bankaccounts.BankAccountService
	notifyr            *service.Notification
	audit              *audit.Service
}

func (u User) router(server *Server) {
	u.server = server
	u.walletService = server.walletService
	u.userService = server.userService
	u.notifyr = server.inAppnotificationService
	u.bankAccountService = server.bankAccountService
	u.audit = server.auditService

	// serverGroupV1 := server.router.Group("/user")
	serverGroupV1 := server.router.Group("/api/v1/user")
	serverGroupV1.Use(u.server.authMiddleware.AuthenticatedMiddleware())
	serverGroupV1.GET("profile", u.profile)
	serverGroupV1.POST("usertag", u.userTag)
	serverGroupV1.POST("checktag", u.checkTag)
	serverGroupV1.POST("push-token", u.pushToken)
	serverGroupV1.POST("fresh-chat", u.freshChatID)
	serverGroupV1.PUT("phone-number", u.updatePhoneNumber)
	serverGroupV1.PUT("update-name", u.updateName)
	serverGroupV1.GET("/:user_id/avatar", u.getAvatar)
	serverGroupV1.PUT("avatar", u.updateAvatar)
	serverGroupV1.GET("referral", u.referral)
	serverGroupV1.GET("get-new-users-today", u.GetNewUsersToday)
	serverGroupV1.GET("list-users", u.ListUsers)
	serverGroupV1.GET("notifications", u.GetNotifications)
	serverGroupV1.POST("delete-user/:id", u.DeleteUser)
	serverGroupV1.GET("get-user/:id", u.GetUserByID)
	serverGroupV1.PUT("/notification/mark-as-read/:id", u.MarkNotificationAsRead)
	// serverGroupV1.DELETE("/notification/delete/:id", u.DeleteNotification)
	serverGroupV1.GET("/notification/mark-all-as-read", u.MarkAllNotificationsAsRead)
	serverGroupV1.GET("/notification/count-unread", u.CountUnreadNotifications)
	serverGroupV1.PUT("update-status/:id", u.UpdateUserStatus)
	serverGroupV1.POST("/bank-accounts", u.createBankAccount)
	serverGroupV1.GET("/bank-accounts", u.GetBankAccounts)
	serverGroupV1.GET("/bank-accounts/default", u.GetDefaultBankAccount)
	serverGroupV1.POST("/bank-accounts/:account_id/set-default", u.SetDefaultBankAccount)
	serverGroupV1.DELETE("bank-accounts/:account_id", u.DeleteBankAccount)
	serverGroupV1.GET("/admin/bank-accounts", u.GetAllBankAccounts)
	serverGroupV1.GET("/transactions", u.ListUserTransactions)
	serverGroupV1.GET("/transactions/:transaction_id", u.GetTransactionDetails)
	serverGroupV1.PUT("/toggle-rapid-ramp", u.ToggleRapidRamp)
	serverGroupV1.PUT("/toggle-biometric", u.UpdateUserBiometric)
	/// For test purposes only
	serverGroupV1.POST("get-push", u.testPush)
}

// testPush godoc
// @Summary Test Push Notification
// @Description Send a test push notification to the provided FCM or Expo token
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param pushRequest body object{fcm_token=string,expo_token=string} true "Push Notification Request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/get-push [post]
func (u *User) testPush(ctx *gin.Context) {
	request := struct {
		FCMToken  string `json:"fcm_token"`
		ExpoToken string `json:"expo_token"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid FCM Token"))
		return
	}

	var provider service.PushProvider
	if request.FCMToken != "" {
		provider = service.PushProviderFCM
	}

	if request.ExpoToken != "" {
		provider = service.PushProviderExpo
	}

	activeUser, _ := utils.GetActiveUser(ctx)

	err = u.server.pushNotification.SendPush(ctx, &service.PushNotificationInfo{
		UserID:         activeUser.UserID,
		Title:          "Test Push",
		Message:        "Current USER Testing Push Notifications",
		Provider:       provider,
		UserFCMToken:   request.FCMToken,
		UserExpoToken:  request.ExpoToken,
		Badge:          1,
		AnalyticsLabel: "test_push",
	})

	if err != nil {
		u.server.logger.Error(fmt.Sprintf("an error occurred with push notifications: %v", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred sending push notifications"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("message sent successfully", nil))

}

// GetUserID godoc
// @Summary Get User ID
// @Description Retrieve the user ID based on the provided encrypted ID
// @Tags user
// @Accept json
// @Produce json
// @Param userRequest body object{id=string} true "User ID Request"
// @Success 200 {object} basemodels.SuccessResponse{data=object{userID=int64}}
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /api/v1/user/get-user-id [post]
func (u *User) GetUserID(ctx *gin.Context) {
	request := struct {
		Id models.ID `json:"id" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"userID": int64(request.Id)})
}

// profile godoc
// @Summary Get user profile
// @Description Get the authenticated user's profile information
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/profile [get]
func (u *User) profile(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	dbUser, err := u.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("user does not exist"))
		return
	}
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user retrieved successfully", models.UserResponse{}.ToUserResponse(&dbUser)))
}

// checkTag godoc
// @Summary Check User Tag Availability
// @Description Check if a user tag is available for use
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param tagRequest body object{tag=string} true "User Tag Request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/checktag [post]
func (u *User) checkTag(ctx *gin.Context) {
	// Eventually use the cacheService to ensure requests are getting resolved in-memory
	request := struct {
		Tag string `json:"tag" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter tag"))
		return
	}

	tagExists, err := u.userService.UserTagExists(ctx, request.Tag)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	if tagExists {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("tag unavailable"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("tag available to add", nil))
}

// userTag godoc
// @Summary Set User Tag
// @Description Set or update the authenticated user's tag
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param tagRequest body object{tag=string} true "User Tag Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/usertag [post]
func (u *User) userTag(ctx *gin.Context) {

	// request := struct {
	// 	Tag string `json:"tag" binding:"required"`
	// }{}

	// err := ctx.ShouldBindJSON(&request)
	// if err != nil {
	// 	ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter tag"))
	// 	return
	// }

	// activeUser, err := utils.GetActiveUser(ctx)
	// if err != nil {
	// 	u.server.logger.Error(err.Error())
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	// userInfo, err := u.userService.UpdateUserTag(ctx, activeUser.UserID, request.Tag)

	// if err != nil {
	// 	u.server.logger.Error(err.Error())
	// 	ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
	// 	return
	// }

	// // Audit log
	// logentry := audit.NewUserLog(
	// 	ctx,
	// 	audit.EventUserTagUpdated,
	// 	fmt.Sprint(userInfo.ID),
	// 	activeUser.Role,
	// 	fmt.Sprintf("User %d updated their user tag", activeUser.UserID),
	// 	&activeUser.UserID,
	// 	audit.SeverityInfo,
	// 	audit.ActionCreate,
	// 	true,
	// )

	// u.audit.Log(logentry)
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("DEPRECATED", nil))
}

// freshChatID godoc
// @Summary Set FreshChat ID
// @Description Set or update the authenticated user's FreshChat ID
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param freshChatRequest body object{fresh_chat_id=string} true "FreshChat ID Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/fresh-chat [post]
func (u *User) freshChatID(ctx *gin.Context) {

	request := struct {
		FreshChatID string `json:"fresh_chat_id" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a fresh_chat_id"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userInfo, err := u.userService.UpdateUserFreshChatID(ctx, activeUser.UserID, request.FreshChatID)

	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user fresh tag ID set successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

// pushToken godoc
// @Summary Add Push Notification Token
// @Description Add or update the authenticated user's push notification token (FCM, Expo, or Web)
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param pushTokenRequest body object{fcm_token=string,expo_token=string,web_token=string,device_uuid=string} true "Push Token Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserTokenResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/push-token [post]
func (u *User) pushToken(ctx *gin.Context) {

	request := struct {
		FCMToken   string `json:"fcm_token"`
		ExpoToken  string `json:"expo_token"`
		WebToken   string `json:"web_token"`
		DeviceUUID string `json:"device_uuid"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid Token"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if request.FCMToken == "" && request.ExpoToken == "" && request.WebToken == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid fcm_token, expo_token, or web_token"))
		return
	}

	if request.FCMToken != "" {
		tokenValue, err := u.userService.AddUserFCMToken(ctx, activeUser.UserID, request.FCMToken, request.DeviceUUID)
		if err != nil {
			u.server.logger.Error(err.Error())
			msg := err.Error()
			if strings.Contains(msg, "device_uuid is required") ||
				strings.Contains(msg, "not a valid FCM token shape") {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError(msg))
				return
			}
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting token %v", err.Error())))
			return
		}
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("user FCM Token upserted successfully", models.ToUserTokenResponse(tokenValue)))
		return
	}

	if request.ExpoToken != "" {
		tokenValue, err := u.userService.AddUserExpoToken(ctx, activeUser.UserID, request.ExpoToken, request.DeviceUUID)
		if err != nil {
			u.server.logger.Error(err.Error())
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting token %v", err.Error())))
			return
		}

		// Audit log
		logentry := audit.NewUserLog(
			ctx,
			audit.EventUserPushTokenUpdated,
			string(tokenValue.ID),
			activeUser.Role,
			fmt.Sprintf("User %d updated their Expo push notification token", activeUser.UserID),
			&activeUser.UserID,
			audit.SeverityInfo,
			audit.ActionCreate,
			true,
		)
		u.audit.Log(logentry)

		ctx.JSON(http.StatusOK, basemodels.NewSuccess("user FCM Token upserted successfully", models.ToUserTokenResponse(tokenValue)))
		return
	}

	if request.WebToken != "" {
		tokenValue, err := u.userService.AddUserWebToken(ctx, activeUser.UserID, request.WebToken, request.DeviceUUID)
		if err != nil {
			u.server.logger.Error(err.Error())
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting token %v", err.Error())))
			return
		}
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("user Web Token upserted successfully", models.ToUserTokenResponse(tokenValue)))
		return
	}
}

// updatePhoneNumber godoc
// @Summary Update Phone Number
// @Description Update the authenticated user's phone number using OTP verification
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param phoneNumberRequest body object{phone_number=string,otp=string} true "Phone Number Update Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/phone-number [put]
func (u *User) updatePhoneNumber(ctx *gin.Context) {
	request := struct {
		PhoneNumber string `json:"phone_number"`
		OTP         string `json:"otp"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid Token"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if request.PhoneNumber == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a either a valid phone number"))
		return
	}

	if request.OTP == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid OTP"))
		return
	}

	validate := validator.New()
	err = validate.Var(request.PhoneNumber, "e164")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidPhone))
		return
	}

	dbOTP, err := u.server.queries.GetOTPByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	ok := utils.CompareOTP(request.OTP, utils.OTPObject{
		OTP:    dbOTP.Otp,
		Expiry: dbOTP.ExpiresAt,
	})
	if !ok {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid or expired OTP"))
		return
	}

	userInfo, err := u.userService.UpdateUserPhoneNumber(ctx, activeUser.UserID, request.PhoneNumber)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting phone number %v", err.Error())))
		return
	}

	// audit log
	logentry := audit.NewUserLog(
		ctx,
		audit.EventUserPhoneNumberUpdated,
		userInfo.ID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d updated their phone number", activeUser.UserID),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionUpdate,
		true,
	)
	u.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user phone number upserted successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

// updateName godoc
// @Summary Update User Name
// @Description Update the authenticated user's first and last name
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param nameRequest body object{first_name=string,last_name=string} true "Name Update Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/update-name [put]
func (u *User) updateName(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	request := struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}{}

	err = ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid first name and last name"))
		return
	}

	userInfo, err := u.userService.UpdateUserNames(ctx, activeUser.UserID, request.FirstName, request.LastName)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting first name %v", err.Error())))
		return
	}

	// audit log
	logentry := audit.NewUserLog(
		ctx,
		audit.EventUserNameUpdated,
		userInfo.ID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d updated their first and last name", activeUser.UserID),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionUpdate,
		true,
	)
	u.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user name upserted successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

// referral godoc
// @Summary Get User Referral
// @Description Retrieve the referral information for the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} db.Referral
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/referral [get]
func (u *User) referral(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	referral, err := u.userService.GetUserReferral(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("user referral not found"))
			return
		}
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user referral fetched successfully", referral))
}

// updateAvatar godoc
// @Summary Update User Avatar
// @Description Update the authenticated user's avatar image
// @Tags user
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param avatar formData file true "Avatar Image File"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/avatar [put]
func (u *User) updateAvatar(ctx *gin.Context) {
	file, _, err := ctx.Request.FormFile("avatar")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please supply a valid image"))
		return
	}
	defer file.Close()

	imageBytes, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid avatar"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// baseURL := "https://swiftfiat.s3.amazonaws.com/user"
	baseURL := "api/v1/user"
	encryptedUserID, err := models.EncryptID(activeUser.UserID)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting avatar %v", err.Error())))
		return
	}
	avatarName := fmt.Sprintf("%s/%s/avatar", baseURL, encryptedUserID)

	userInfo, err := u.userService.UpdateUserAvatar(ctx, activeUser.UserID, avatarName, imageBytes)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred upserting avatar %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user avatar upserted successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

// getAvatar godoc
// @Summary Get User Avatar
// @Description Retrieve the avatar image for a specified user
// @Tags user
// @Accept json
// @Produce image/png
// @Param user_id path string true "Encrypted User ID"
// @Success 200 {file} binary
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/{user_id}/avatar [get]
func (u *User) getAvatar(ctx *gin.Context) {
	param := ctx.Param("user_id")

	baseURL := "api/v1/user"
	avatarName := fmt.Sprintf("%s/%s/avatar", baseURL, param)
	userInfo, err := u.server.queries.GetUserAvatar(ctx, sql.NullString{
		String: avatarName,
		Valid:  avatarName != "",
	})
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("user avatar not found"))
			return
		}
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.Header("Content-Type", "image/png")
	ctx.DataFromReader(http.StatusOK, int64(len(userInfo.AvatarBlob)), "image/png", bytes.NewReader(userInfo.AvatarBlob), nil)
}

// GetNewUsersToday godoc
// @Summary Get New Users Today
// @Description Retrieve a list of users who registered today (admin only)
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=[]models.UserResponse}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/get-new-users-today [get]
func (u *User) GetNewUsersToday(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}
	newUsers, err := u.userService.GetNewUsersToday(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("new users fetched successfully", newUsers))
}

// ListUsers godoc
// @Summary List Users
// @Description Retrieve a paginated list of users (admin only)
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param limit query int false "Number of users to retrieve" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} basemodels.SuccessResponse{data=object{users=[]models.UserResponse,total_users=int,offset=int,limit=int}}
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/list-users [get]
func (u *User) ListUsers(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(ctx.DefaultQuery("offset", "0"))

	users, err := u.userService.ListUsers(ctx, int32(limit), int32(offset))
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("users fetched successfully", gin.H{
		"users":       users,
		"total_users": len(users),
		"offset":      offset,
		"limit":       limit,
	}))
}

type KYCListResponse struct {
	KYCs  []*models.UserKYCInformation `json:"kycs"`
	Count int                          `json:"count"`
}

type NotificationListResponse struct {
	Notifications []*models.NotificationResponse `json:"notifications"`
	Count         int                            `json:"count"`
}

// GetNotifications godoc
// @Summary Get User Notifications
// @Description Retrieve the notifications for the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]any
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/notifications [get]
func (u *User) GetNotifications(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	notifications, err := u.notifyr.GetAllForUser(ctx, activeUser.UserID, 50, 0)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	var responses []*models.NotificationResponse
	for _, not := range *notifications {
		v := models.ToNotificationResponse(&not)
		responses = append(responses, v)
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user notifications fetched successfully", NotificationListResponse{
		Notifications: responses,
		Count:         len(responses),
	}))
}

// MarkNotificationAsRead godoc
// @Summary Mark Notification as Read
// @Description Mark a specific notification as read for the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Notification ID"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/notification/mark-as-read/{id} [put]
func (u *User) MarkNotificationAsRead(ctx *gin.Context) {
	notID := ctx.Param("id")

	notificationID, err := strconv.Atoi(notID)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("invalid notification ID"))
		return
	}
	err = u.notifyr.MarkAsRead(ctx, int64(notificationID))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.SuccessResponse{Message: "marked as read"})

}

// MarkAllNotificationsAsRead godoc
// @Summary Mark All Notifications as Read
// @Description Mark all notifications as read for the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/notifications/mark-all-as-read [put]
func (u *User) MarkAllNotificationsAsRead(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	err = u.notifyr.MarkAllAsRead(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.ServerError))
		return
	}

	c.JSON(http.StatusOK, basemodels.SuccessResponse{Message: "marked all as read"})
}

// CountUnreadNotifications godoc
// @Summary Count Unread Notifications
// @Description Count the number of unread notifications for the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} basemodels.SuccessResponse{data=object{count=int}}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/notifications/count-unread [get]
func (u *User) CountUnreadNotifications(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	count, err := u.notifyr.CountUnreadForUser(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.ServerError))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", count))
}

// DeleteUser godoc
// @Summary Delete User
// @Description Delete a user by ID (admin only)
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param deleteUserRequest body object{phone_number=string,email=string,first_name=string} true "Delete User Request"
// @Success 200 {object} basemodels.SuccessResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/delete-user/{id} [delete]
func (u *User) DeleteUser(c *gin.Context) {
	iD := c.Param("id")
	userID, err := uuid.Parse(iD)
	if err != nil {
		u.server.logger.Error("error deleting user", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid param"))
	}
	var req struct {
		PhoneNumber string `json:"phone_number" binding:"required"`
		Email       string `json:"email" binding:"required"`
		FirstName   string `json:"first_name" binding:"required"`
		TwoFACode   string `json:"two_fa_code" binding:"required"`
	}
	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid request"))
		return
	}
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	admin, err := u.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !admin.TwofaEnabled.Bool {
		c.JSON(http.StatusForbidden, basemodels.NewError("2FA must be enabled to perform this action"))
		return
	}

	valid := totp.Validate(req.TwoFACode, admin.TwofaSecret.String)
	if !valid {
		c.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid 2FA code"))
		return
	}

	if activeUser.Role != models.SUPER_ADMIN && activeUser.Role != models.ADMIN {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	param := db.DeleteUserParams{
		PhoneNumber: sql.NullString{String: req.PhoneNumber, Valid: true},
		Email:       req.Email,
		FirstName:   sql.NullString{String: req.FirstName, Valid: true},
		ID:          userID,
	}

	_, err = u.server.queries.DeleteUser(c, param)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred deleting the user %v", err.Error())))
		return
	}

	// Audit log
	logentry := audit.NewUserLog(
		c,
		audit.EventUserDeleted,
		userID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d deleted user %d", activeUser.UserID, userID),
		&activeUser.UserID,
		audit.SeverityCritical,
		audit.ActionDelete,
		true,
	)
	u.audit.Log(logentry)
	c.JSON(http.StatusOK, basemodels.NewSuccess("user deleted successfully", nil))
}

type UserDetailResponse struct {
	User     *models.UserResponse     `json:"user"`
	Wallets  *[]models.WalletResponse `json:"wallets"`
	Referral map[string]any           `json:"referral"`
}

// GetUserByID godoc
// @Summary Get User by ID
// @Description Retrieve user details by ID (admin only)
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} UserDetailResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
// @Router /api/v1/user/get-user/{id} [get]
func (u *User) GetUserByID(c *gin.Context) {
	id := c.Param("id")
	userID, err := uuid.Parse(id)
	if err != nil {
		u.server.logger.Error("error getting user", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid id"))
		return
	}
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	user, err := u.server.queries.GetUserByID(c, userID)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ref, _ := u.server.queries.GetReferralByUserID(c, activeUser.UserID)
	// if err != nil {
	// 	if errors.Is(err, sql.ErrNoRows) {
	// 		c.JSON(http.StatusBadRequest, basemodels.NewError("user referral not found"))
	// 		return
	// 	}
	// 	u.server.logger.Error(err.Error())
	// 	c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
	// 	return
	// }

	earnings, err := u.server.queries.GetReferralEarnings(c, user.ID)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	refs, err := u.server.queries.GetUserReferrals(c, user.ID)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	wallets, err := u.server.queries.ListWallets(c, activeUser.UserID)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	var walletResponses []models.WalletResponse
	for _, wallet := range wallets {
		walletResponses = append(walletResponses, *models.ToWalletResponse(&wallet))
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("user retrieved successfully", UserDetailResponse{
		User:    models.UserResponse{}.ToUserResponse(&user),
		Wallets: &walletResponses,
		Referral: map[string]any{
			"key":       ref.ReferralKey,
			"earnings":  earnings,
			"referrals": refs,
		},
	}))
}

// UpdateUserStatus godoc
// @Summary Update User Status
// @Description Activate or deactivate a user by ID (admin only)
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param updateUserStatusRequest body object{is_active=string} true "Update User Status Request"
// @Success 200 {object} basemodels.SuccessResponse{data=models.UserResponse}
// @Failure 400 {object} basemodels.ErrorResponse
// @Failure 401 {object} basemodels.ErrorResponse
// @Failure 403 {object} basemodels.ErrorResponse
// @Failure 500 {object} basemodels.ErrorResponse
func (u *User) UpdateUserStatus(ctx *gin.Context) {
	// @Router /api/v1/user/update-user-status/{id} [put]
	id := ctx.Param("id")
	userID, err := uuid.Parse(id)
	if err != nil {
		u.server.logger.Error("error updating user status", err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid user ID"))
	}
	request := struct {
		IsActive  string `json:"is_active" binding:"required"`
		TwoFACode string `json:"two_fa_code" binding:"required"`
	}{}

	err = ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please provide a valid request"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	admin, err := u.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !admin.TwofaEnabled.Bool {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("2FA must be enabled to perform this action"))
		return
	}

	valid := totp.Validate(request.TwoFACode, admin.TwofaSecret.String)
	if !valid {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid 2FA code"))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	var updatedUser db.User
	if request.IsActive == "true" {
		updatedUser, err = u.server.queries.ActivateUser(ctx, userID)
	} else {
		updatedUser, err = u.server.queries.DeactivateUser(ctx, userID)
	}

	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("an error occurred updating the user status"))
		return
	}

	status := "activated"
	if request.IsActive == "false" {
		status = "deactivated"
	}

	// Audit log
	logentry := audit.NewUserLog(
		ctx,
		audit.EventUserStatusUpdated,
		userID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d %s user %d", activeUser.UserID, status, userID),
		&activeUser.UserID,
		audit.SeverityWarning,
		audit.ActionUpdate,
		true,
	)
	u.audit.Log(logentry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("user successfully %s", status), models.UserResponse{}.ToUserResponse(&updatedUser)))
}

// ============================================================
// BANK ACCOUNT ENDPOINTS
// ============================================================

// CreateBankAccount godoc
// @Summary Add a new bank account
// @Description Adds and verifies a new bank account for the user
// @Tags user
// @Accept json
// @Produce json
// @Param request body bankaccounts.CreateBankAccountRequest true "Bank account details"
// @Success 201 {object} bankaccounts.BankAccountResponse
// @Failure 400 {object} basemodels.ErrorResponse
// @Router /api/v1/user/bank-accounts [post]
// @Security BearerAuth
func (u *User) createBankAccount(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	var req bankaccounts.CreateBankAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
		return
	}

	bankAccount, err := u.bankAccountService.CreateBankAccount(c.Request.Context(), activeUser.UserID, &req)
	if err != nil {
		u.server.logger.Error("Failed to create bank account", "error", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError("failed to verify bank account"))
		return
	}

	// audit log
	logentry := audit.NewUserLog(
		c,
		audit.EventBankAccountAdded,
		bankAccount.ID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d added a new bank account %s", activeUser.UserID, bankAccount.AccountNumber),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionCreate,
		true,
	)
	u.audit.Log(logentry)

	c.JSON(http.StatusCreated, basemodels.NewSuccess("Bank account added and verified successfully", bankAccount))
}

// GetBankAccounts godoc
// @Summary Get all bank accounts
// @Description Retrieves all bank accounts for the authenticated user
// @Tags user
// @Produce json
// @Success 200 {object} []bankaccounts.BankAccountResponse
// @Router /api/v1/user/bank-accounts [get]
// @Security BearerAuth
func (u *User) GetBankAccounts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	accounts, err := u.bankAccountService.GetBankAccounts(c.Request.Context(), activeUser.UserID)
	if err != nil {
		u.server.logger.Error("Failed to fetch bank accounts", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch bank accounts"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", accounts))
}

// GetAllBankAccounts godoc
// @Summary Get all bank accounts
// @Description Retrieves all bank accounts for the authenticated user
// @Tags user
// @Produce json
// @Success 200 {object} []bankaccounts.BankAccountResponse
// @Router /api/v1/user/admin/bank-accounts [get]
// @Security BearerAuth
func (u *User) GetAllBankAccounts(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	if activeUser.Role == models.USER {
		c.JSON(http.StatusForbidden, basemodels.NewError("forbidden"))
		return
	}

	accounts, err := u.bankAccountService.GetAllBankAccounts(c.Request.Context())
	if err != nil {
		u.server.logger.Error("Failed to fetch bank accounts", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch bank accounts"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", accounts))
}

// GetDefaultBankAccount godoc
// @Summary Get default bank account
// @Description Retrieves the user's default bank account
// @Tags user
// @Produce json
// @Success 200 {object} bankaccounts.BankAccountResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/user/bank-accounts/default [get]
// @Security BearerAuth
func (u *User) GetDefaultBankAccount(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	account, err := u.bankAccountService.GetDefaultBankAccount(c.Request.Context(), activeUser.UserID)
	if err != nil {
		if err == utils.ErrBankAccountNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError(utils.ErrBankAccountNotFound.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch default bank account"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", account))
}

// SetDefaultBankAccount godoc
// @Summary Set default bank account
// @Description Sets a bank account as the user's default
// @Tags Bank Accounts
// @Produce json
// @Param account_id path string true "Bank Account ID" format(uuid)
// @Success 200 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/user/bank-accounts/{account_id}/set-default [post]
// @Security BearerAuth
func (u *User) SetDefaultBankAccount(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	accountID, err := uuid.Parse(c.Param("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account ID"})
		return
	}

	err = u.bankAccountService.SetDefaultBankAccount(c.Request.Context(), accountID, activeUser.UserID)
	if err != nil {
		if err == utils.ErrBankAccountNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError(utils.ErrBankAccountNotFound.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to set default bank account"))
		return
	}

	// audit log
	logentry := audit.NewUserLog(
		c,
		audit.EventDefaultBankAccountUpdated,
		accountID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d set bank account %s as default", activeUser.UserID, accountID.String()),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionUpdate,
		true,
	)
	u.audit.Log(logentry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Default bank account updated successfully", nil))
}

// DeleteBankAccount godoc
// @Summary Delete a bank account
// @Description Soft deletes a bank account
// @Tags Bank Accounts
// @Produce json
// @Param account_id path string true "Bank Account ID" format(uuid)
// @Success 200 {object} basemodels.ErrorResponse
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/user/bank-accounts/{account_id} [delete]
// @Security BearerAuth
func (u *User) DeleteBankAccount(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	accountID, err := uuid.Parse(c.Param("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid account ID"))
		return
	}

	err = u.bankAccountService.DeleteBankAccount(c.Request.Context(), accountID, activeUser.UserID)
	if err != nil {
		if err == utils.ErrBankAccountNotFound {
			c.JSON(http.StatusNotFound, basemodels.NewError(utils.ErrBankAccountNotFound.Message))
			return
		}
		u.server.logger.Error("Failed to delete bank account", "error", err)
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	// audit log
	logentry := audit.NewUserLog(
		c,
		audit.EventBankAccountDeleted,
		accountID.String(),
		activeUser.Role,
		fmt.Sprintf("User %d deleted bank account %s", activeUser.UserID, accountID.String()),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionDelete,
		true,
	)
	u.audit.Log(logentry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("Bank account deleted successfully", nil))
}

// ListUserTransactions godoc
// @Summary List user transactions
// @Description Retrieves the list of transactions for the user
// @Tags user
// @Produce json
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/user/transactions [get]
// @Security BearerAuth
func (u *User) ListUserTransactions(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	transactions, err := u.userService.ListUserTransactions(c.Request.Context(), activeUser.UserID)
	if err != nil {
		u.server.logger.Error("Failed to list user transactions", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to list user transactions"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", transactions))
}

// GetTransactionDetails godoc
// @Summary Get transaction details
// @Description Retrieves the details of a specific transaction
// @Tags user
// @Produce json
// @Param transaction_id path string true "Transaction ID" format(uuid)
// @Failure 404 {object} basemodels.ErrorResponse
// @Router /api/v1/user/transactions/{transaction_id} [get]
// @Security BearerAuth
func (u *User) GetTransactionDetails(c *gin.Context) {
	transactionID, err := uuid.Parse(c.Param("transaction_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("invalid transaction ID"))
		return
	}

	transaction, err := u.userService.GetTansactionDetails(c.Request.Context(), transactionID)
	if err != nil {
		u.server.logger.Error("Failed to get transaction details", "error", err)
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to get transaction details"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", transaction))
}

func (u *User) ToggleRapidRamp(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}

	b, err := u.userService.ToggleRapidRamp(c, activeUser.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError(err.Error()))
		return
	}

	var status, msg string

	if !b {
		status = "inactive"
		msg = fmt.Sprintf("user %s deactivated rapid ramp", activeUser.UserTag)
	} else {
		status = "active"
		msg = fmt.Sprintf("user %s activated rapid ramp", activeUser.UserTag)
	}

	logEntry := audit.NewUserLog(
		c,
		audit.EventRapidRampToggle,
		"",
		activeUser.Role,
		msg,
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionUpdate,
		true,
	)

	u.audit.Log(logEntry)

	c.JSON(http.StatusOK, basemodels.NewSuccess("rapid ramp toggled", status))
}

type ToggleBiometricRequest struct {
	Enable    bool   `json:"enable"`
	TwoFACode string `json:"two_fa_code"`
}

func (u *User) UpdateUserBiometric(c *gin.Context) {
	activeUser, err := utils.GetActiveUser(c)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
		return
	}
	var req ToggleBiometricRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	user, err := u.server.queries.GetUserByID(c, activeUser.UserID)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to fetch user details"))
		return
	}

	if user.TwofaEnabled.Bool && req.TwoFACode == "" {
		c.JSON(http.StatusForbidden, basemodels.NewError("2FA code is required to update biometric setting"))
		return
	}

	if req.TwoFACode != "" {
		valid := totp.Validate(req.TwoFACode, user.TwofaSecret.String)
		if !valid {
			c.JSON(http.StatusUnauthorized, basemodels.NewError("Invalid 2FA code"))
			return
		}
	}
	err = u.userService.ToggleUserBiometric(c, activeUser.UserID, req.Enable)
	if err != nil {
		u.server.logger.Error(err.Error())
		c.JSON(http.StatusInternalServerError, basemodels.NewError("failed to update biometric setting"))
		return
	}

	status := "disabled"
	if req.Enable {
		status = "enabled"
	}

	logEntry := audit.NewUserLog(
		c,
		audit.EventBiometricToggle,
		"",
		activeUser.Role,
		fmt.Sprintf("User %d %s biometric authentication", activeUser.UserID, status),
		&activeUser.UserID,
		audit.SeverityInfo,
		audit.ActionUpdate,
		true,
	)

	u.audit.Log(logEntry)

	c.JSON(http.StatusOK, basemodels.NewSuccess(fmt.Sprintf("biometric authentication %s", status), nil))

}
