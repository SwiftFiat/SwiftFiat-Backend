package service

/// We need to set up FCM for this project

import (
	"context"
	"fmt"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	expo "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
	"google.golang.org/api/option"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
)

type PushProvider string

const (
	PushProviderFCM  = PushProvider("FCM")
	PushProviderExpo = PushProvider("EXPO")
)

type Config struct {
	GoogleAppCredentials string `mapstructure:"GOOGLE_APPLICATION_CREDENTIALS"`
}

type PushNotificationInfo struct {
	UserID         uuid.UUID    `json:"user_id"`
	Title          string       `json:"title"`
	Message        string       `json:"message"`
	Provider       PushProvider `json:"provider"`
	UserExpoToken  string       `json:"user_expo_token"`
	UserFCMToken   string       `json:"user_fcm_token"`
	Badge          int          `json:"badge"`
	AnalyticsLabel string       `json:"analytics"`
}

type PushNotificationService struct {
	client      *expo.PushClient
	app         *firebase.App
	logger      *logging.Logger
	userService *user_service.UserService
}

func NewPushNotificationService(logger *logging.Logger) *PushNotificationService {

	var config Config
	err := utils.LoadCustomConfig(utils.EnvPath, &config)
	if err != nil {
		logger.Error(fmt.Sprintf("Error loading JSON config file: %v", err))
		return nil
	}

	opt := option.WithCredentialsFile(config.GoogleAppCredentials)
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		logger.Error(fmt.Sprintf("Error starting firebase App: %v", err))
		return nil
	}

	logger.Info(fmt.Sprintf("Firebase App Config: %v", app))

	// Create a new Expo SDK client
	client := expo.NewPushClient(nil)

	return &PushNotificationService{
		client: client,
		app:    app,
		logger: logger,
	}
}

func (p *PushNotificationService) SendPush(ctx context.Context, info *PushNotificationInfo) error {

	if info.Provider == PushProviderExpo {
		err := p.SendPushExpo(ctx, info)
		return err
	}

	// Use background context for Firebase operations to avoid request context cancellations
	bgCtx := context.Background()
	client, err := p.app.Messaging(bgCtx)
	if err != nil {
		return err
	}

	// Add data payload for analytics and custom app handling (do NOT include title/body here)
	data := map[string]string{}
	if info.AnalyticsLabel != "" {
		data["analytics_label"] = info.AnalyticsLabel
	}

	newMessage := messaging.Message{
		Token: info.UserFCMToken,
		Data:  data,
		Notification: &messaging.Notification{
			Title: info.Title, // Assuming `info.Title` holds a more appropriate title.
			Body:  info.Message,
		},
		Android: &messaging.AndroidConfig{
			Priority: "high", // Ensures the message is delivered immediately.
			Notification: &messaging.AndroidNotification{
				Title: info.Title,
				Body:  info.Message,
				Color: "#f4bb44", // Notification icon color.
				Sound: "default", // Plays the default sound.
			},
		},
		APNS: &messaging.APNSConfig{
			Headers: map[string]string{
				"apns-priority":   "10",    // High priority for immediate delivery (valid values: 10 or 1).
				"apns-push-type":  "alert", // Ensures a visible alert is displayed.
				"apns-expiration": "3600",  // Message expires in 1 hour if not delivered.
			},
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Alert: &messaging.ApsAlert{
						Title: info.Title,
						Body:  info.Message,
					},
					Sound: "default", // Plays the default system sound.
				},
			},
			FCMOptions: &messaging.APNSFCMOptions{
				AnalyticsLabel: info.AnalyticsLabel, // Optional: useful for tracking analytics.
			},
		},
		Webpush: &messaging.WebpushConfig{
			Notification: &messaging.WebpushNotification{
				Title: info.Title,
				Body:  info.Message,
			},
		},
	}

	didSend, err := client.Send(bgCtx, &newMessage)
	if err != nil {
		p.handleTokenError(info.UserID, info.UserFCMToken, err)
		return err
	}

	p.logger.Info(fmt.Sprintf("Did send: %v", didSend))

	return nil
}

func (p *PushNotificationService) SendPushExpo(ctx context.Context, info *PushNotificationInfo) error {
	response, err := p.client.Publish(
		&expo.PushMessage{
			To:       []expo.ExponentPushToken{expo.ExponentPushToken(info.UserExpoToken)},
			Body:     info.Message,
			Data:     map[string]string{"withSome": "data"},
			Sound:    "default",
			Title:    info.Title,
			Priority: expo.DefaultPriority,
		},
	)

	// Check errors
	if err != nil {
		p.handleTokenError(info.UserID, info.UserExpoToken, err)
		return err
	}

	// Validate responses
	if err := response.ValidateResponse(); err != nil {
		p.handleTokenError(info.UserID, info.UserExpoToken, err)
		return fmt.Errorf("failed: %v", response.PushMessage.To)
	}

	return nil

}

func (p *PushNotificationService) handleTokenError(userID uuid.UUID, token string, err error) {
	if err == nil || token == "" {
		return
	}

	errMsg := err.Error()
	// FCM specific errors
	isInvalidFCM := messaging.IsRegistrationTokenNotRegistered(err) ||
		messaging.IsInvalidArgument(err) ||
		strings.Contains(errMsg, "registration token is not a valid FCM registration token") ||
		strings.Contains(errMsg, "invalid-registration-token") ||
		strings.Contains(errMsg, "not a valid FCM registration token")

	// Expo specific errors
	isInvalidExpo := strings.Contains(errMsg, "DeviceNotRegistered") ||
		strings.Contains(errMsg, "InvalidToken")

	if isInvalidFCM || isInvalidExpo {
		p.logger.Warn(fmt.Sprintf("Removing invalid push token for user %d: %v", userID, err))
		if p.userService != nil {
			// Use background context to avoid request context cancellations
			bgCtx := context.Background()
			_ = p.userService.RemoveUserToken(bgCtx, userID, token)
		}
	}
}

// SetUserService wires the user service into the push notification service.
func (p *PushNotificationService) SetUserService(us *user_service.UserService) {
	p.userService = us
}

func (p *PushNotificationService) getUserPushTokens(userID uuid.UUID) (*struct {
	FCMToken  string
	ExpoToken string
}, error) {
	// Guard: userService must be configured
	if p.userService == nil {
		// Not fatal for notification flows—log and return empty tokens so caller no-ops.
		p.logger.Error("userService is nil in PushNotificationService")
		return &struct {
			FCMToken  string
			ExpoToken string
		}{}, nil
	}
	// Use background context for database operations to avoid request context cancellations
	// affecting background notification tasks
	bgCtx := context.Background()
	tokens, err := p.userService.GetUserPushTokens(bgCtx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error retrieving push tokens for user %d: %v", userID, err))
		return nil, err
	}

	p.logger.Info(fmt.Sprintf("Retrieved %d push tokens for user %d", len(*tokens), userID))

	var fcmToken, expoToken string
	for _, token := range *tokens {
		// p.logger.Debug(fmt.Sprintf("Token for user %d: Provider=%s, Token=%s...", userID, token.Provider, token.Token[uuid.Nil]))
		switch PushProvider(token.Provider) {
		case PushProviderFCM:
			if fcmToken == "" {
				fcmToken = token.Token
			}
		case PushProviderExpo:
			if expoToken == "" {
				expoToken = token.Token
			}
		}
	}

	p.logger.Info(fmt.Sprintf("Selected tokens for user %d: FCM=%t, Expo=%t", userID, fcmToken != "", expoToken != ""))

	return &struct {
		FCMToken  string
		ExpoToken string
	}{
		FCMToken:  fcmToken,
		ExpoToken: expoToken,
	}, nil
}

// ======================================
// Vault Savings
// ======================================
func (p *PushNotificationService) SendVaultGoalCreatedPush(ctx context.Context, userID uuid.UUID, name string) error {
	p.logger.Info(fmt.Sprintf("Attempting to send vault goal created push for user %d, goal name: %s", userID, name))

	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info(fmt.Sprintf("No push tokens found for user %d", userID))
		return nil
	}

	Title := "Vault Goal Created"
	Message := fmt.Sprintf("Your vault goal '%s' as been created", name)

	if tokens.FCMToken != "" {
		p.logger.Info(fmt.Sprintf("Sending FCM push to user %d", userID))
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		p.logger.Info(fmt.Sprintf("Sending Expo push to user %d", userID))
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendGoalCompletedPush(ctx context.Context, userID uuid.UUID, name string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Vault Goal Completed"
	Message := fmt.Sprintf("Congratulations! Your vault goal '%s' has been completed.", name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendDepositSuccessPush(ctx context.Context, userID uuid.UUID, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Deposit Successful"
	Message := fmt.Sprintf("Your vault deposit of %s %s to '%s' was successful.", amount, currency, name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendWithdrawalSuccessPush(ctx context.Context, userID uuid.UUID, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Withdrawal Successful"
	Message := fmt.Sprintf("Your vault withdrawal of %s %s from '%s' was successful.", amount, currency, name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendRecurringDepositSuccessPush(ctx context.Context, userID uuid.UUID, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Recurring Deposit Successful"
	Message := fmt.Sprintf("Your vault recurring deposit of %s %s to '%s' was successful.", amount, currency, name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendRecurringDepositFailedPush(ctx context.Context, userID uuid.UUID, name, reason string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Recurring Deposit Failed"
	Message := fmt.Sprintf("Your vault recurring deposit to '%s' has failed. Reason: %s", name, reason)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendYieldCredited(ctx context.Context, userID uuid.UUID, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Yield Credited"
	Message := fmt.Sprintf("Your vault '%s' has been credited with %s %s as yield.", name, amount, currency)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendRewardNotification(ctx context.Context, userID uuid.UUID, message, txType string, pointEarned int64) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Reward Earned"
	Message := fmt.Sprintf("%s You have earned %d points from %s.", message, pointEarned, txType)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) RecieveWalletTransfer(ctx context.Context, userID uuid.UUID, amount float64) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Wallet Transfer Received"
	Message := fmt.Sprintf("You have received a wallet transfer of %.2f.", amount)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendWalletTransfer(ctx context.Context, userID uuid.UUID, amount float64) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Wallet Transfer"
	Message := fmt.Sprintf("Your wallet transfer of %.2f was successful.", amount)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) AdminTerminateCardNotification(ctx context.Context, userID uuid.UUID, name string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Card Terminated"
	Message := fmt.Sprintf("Your virtual card %s has been terminated by an administrator.", name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) AdminFreezeCardNotification(ctx context.Context, userID uuid.UUID, name string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Card Frozen"
	Message := fmt.Sprintf("Your virtual card %s has been frozen by an administrator.", name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) AdminUnfreezeCardNotification(ctx context.Context, userID uuid.UUID, name string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Card Unfrozen"
	Message := fmt.Sprintf("Your virtual card %s has been unfrozen by an administrator.", name)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SuccessfulAirtimePurchase(ctx context.Context, userID uuid.UUID, amount int64, phoneNumber string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Airtime Purchase Successful"
	Message := fmt.Sprintf("Your airtime purchase of ₦%d from %s to %s was successful.", amount, "SWIIFT", phoneNumber)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SuccessfulDataPurchase(ctx context.Context, userID uuid.UUID, plan string, phoneNumber string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Data Purchase Successful"
	Message := fmt.Sprintf("%s plan to %s was successful.", plan, phoneNumber)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SuccessfulTvSub(ctx context.Context, userID uuid.UUID, plan string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "TV Subscription Successful"
	Message := fmt.Sprintf("Your %s tv subscription was successful.", plan)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SuccessfulElectricityPurchase(ctx context.Context, userID uuid.UUID, amount int64, meterNumber string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Electricity Purchase Successful"
	Message := fmt.Sprintf("Your electricity purchase of ₦%d for meter %s was successful.", amount, meterNumber)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) ReferralBonusEarned(ctx context.Context, userID uuid.UUID, amount string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Referral Bonus Earned"
	Message := fmt.Sprintf("You have earned a referral bonus of %s.", amount)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) NewReferral(ctx context.Context, userID uuid.UUID, userTag string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "New Referral Alert"
	Message := fmt.Sprintf("🎉 %s just signed up using your referral code.", userTag)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) CreditAlert(ctx context.Context, userID uuid.UUID, amount float64, currency string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Credit Alert"
	Message := fmt.Sprintf("🎉 You have received $%.2f %s to your wallet.", amount, currency)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) DebitAlert(ctx context.Context, userID uuid.UUID, amount float64, currency string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Debit Alert"
	Message := fmt.Sprintf("⚠️ A debit of %.2f %s has been made from your wallet.", amount, currency)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) ConversionBonusEarned(ctx context.Context, userID uuid.UUID, amount string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Conversion Bonus Earned"
	Message := fmt.Sprintf("You have earned a conversion bonus of %s.", amount)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendKYCVerifiedPushNotification(ctx context.Context, userID uuid.UUID) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "KYC Verified"
	Message := "Congratulations! Your KYC has been successfully verified."

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendKYCRejectedPushNotification(ctx context.Context, userID uuid.UUID, reason string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "KYC Rejected"
	Message := fmt.Sprintf("Unfortunately, your KYC verification was rejected. Reason: %s", reason)

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}

func (p *PushNotificationService) SendPushNotification(ctx context.Context, userID uuid.UUID, title string, message string) error {
	tokens, err := p.getUserPushTokens(userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == uuid.Nil || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := title
	Message := message

	if tokens.FCMToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:       userID,
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
			return err
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(ctx, &PushNotificationInfo{
			UserID:        userID,
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
			return err
		}
	}
	return nil
}
