package api

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/notification/notification_channel"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type User struct {
	server        *Server
	walletService *wallet.WalletService
	userService   *user_service.UserService
}

func (u User) router(server *Server) {
	u.server = server
	u.walletService = wallet.NewWalletService(
		u.server.queries,
		u.server.logger,
	)
	u.userService = user_service.NewUserService(
		u.server.queries,
		u.server.logger,
		u.walletService,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/user")
	serverGroupV1.GET("profile", u.server.authMiddleware.AuthenticatedMiddleware(), u.profile)
	serverGroupV1.POST("usertag", u.server.authMiddleware.AuthenticatedMiddleware(), u.userTag)
	serverGroupV1.POST("checktag", u.server.authMiddleware.AuthenticatedMiddleware(), u.checkTag)
	serverGroupV1.POST("push-token", u.server.authMiddleware.AuthenticatedMiddleware(), u.pushToken)
	serverGroupV1.POST("fresh-chat", u.server.authMiddleware.AuthenticatedMiddleware(), u.freshChatID)
	serverGroupV1.PUT("phone-number", u.server.authMiddleware.AuthenticatedMiddleware(), u.updatePhoneNumber)
	serverGroupV1.PUT("update-name", u.server.authMiddleware.AuthenticatedMiddleware(), u.updateName)
	serverGroupV1.GET("/:user_id/avatar", u.getAvatar)
	serverGroupV1.PUT("avatar", u.server.authMiddleware.AuthenticatedMiddleware(), u.updateAvatar)
	serverGroupV1.GET("referral", u.server.authMiddleware.AuthenticatedMiddleware(), u.referral)
	/// For test purposes only
	serverGroupV1.POST("get-push", u.server.authMiddleware.AuthenticatedMiddleware(), u.testPush)
}

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

	var provider notification_channel.PushProvider
	if request.FCMToken != "" {
		provider = notification_channel.PushProviderFCM
	}

	if request.ExpoToken != "" {
		provider = notification_channel.PushProviderExpo
	}

	err = u.server.pushNotification.SendPush(&notification_channel.PushNotificationInfo{
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

// Eventually use the cacheService to ensure requests are getting resolved in-memory
func (u *User) checkTag(ctx *gin.Context) {

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

func (u *User) userTag(ctx *gin.Context) {

	request := struct {
		Tag string `json:"tag" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter tag"))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userInfo, err := u.userService.UpdateUserTag(ctx, activeUser.UserID, request.Tag)

	if err != nil {
		u.server.logger.Error(err.Error())
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user tag set successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

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

func (u *User) pushToken(ctx *gin.Context) {

	request := struct {
		FCMToken   string `json:"fcm_token"`
		ExpoToken  string `json:"expo_token"`
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

	if request.FCMToken == "" && request.ExpoToken == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a either a valid fcm_token or an expo_token key"))
		return
	}

	if request.FCMToken != "" {
		tokenValue, err := u.userService.AddUserFCMToken(ctx, activeUser.UserID, request.FCMToken, request.DeviceUUID)
		if err != nil {
			u.server.logger.Error(err.Error())
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
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("user FCM Token upserted successfully", models.ToUserTokenResponse(tokenValue)))
		return
	}
}

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

	dbOTP, err := u.server.queries.GetOTPByUserID(ctx, int32(activeUser.UserID))
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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user phone number upserted successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("user name upserted successfully", models.UserResponse{}.ToUserResponse(userInfo)))
}

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
	encryptedUserID, err := models.EncryptID(models.ID(activeUser.UserID))
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
