package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
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
	serverGroupV1.GET("profile", AuthenticatedMiddleware(), u.profile)
	serverGroupV1.POST("usertag", AuthenticatedMiddleware(), u.userTag)
	serverGroupV1.POST("checktag", AuthenticatedMiddleware(), u.checkTag)
	serverGroupV1.POST("push-token", AuthenticatedMiddleware(), u.pushToken)
	serverGroupV1.POST("fresh-chat", AuthenticatedMiddleware(), u.freshChatID)
	/// For test purposes only
	serverGroupV1.POST("get-push", AuthenticatedMiddleware(), u.testPush)
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

	var provider service.PushProvider
	if request.FCMToken != "" {
		provider = service.PushProviderFCM
	}

	if request.ExpoToken != "" {
		provider = service.PushProviderExpo
	}

	err = u.server.pushNotification.SendPush(&service.PushNotificationInfo{
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
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter a valid FCM Token"))
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
