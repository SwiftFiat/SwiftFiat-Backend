package service

/// We need to set up FCM for this project

import (
	"context"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	expo "github.com/oliveroneill/exponent-server-sdk-golang/sdk"
	"google.golang.org/api/option"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
)

type PushProvider string

const (
	PushProviderFCM  = PushProvider("fcm")
	PushProviderExpo = PushProvider("expo")
)

type Config struct {
	GoogleAppCredentials string `mapstructure:"GOOGLE_APPLICATION_CREDENTIALS"`
}

type PushNotificationInfo struct {
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

func (p *PushNotificationService) SendPush(info *PushNotificationInfo) error {

	if info.Provider == PushProviderExpo {
		err := p.SendPushExpo(info)
		return err
	}

	client, err := p.app.Messaging(context.Background())
	if err != nil {
		return err
	}

	newMessage := messaging.Message{
		Token: info.UserFCMToken,
		Notification: &messaging.Notification{
			Title: info.Title, // Assuming `info.Title` holds a more appropriate title.
			Body:  info.Message,
		},
		Android: &messaging.AndroidConfig{
			Priority: "high", // Ensures the message is delivered immediately.
			Notification: &messaging.AndroidNotification{
				Color: "#f4bb44", // Notification icon color.
				Sound: "default", // Plays the default sound.
			},
		},
		APNS: &messaging.APNSConfig{
			Headers: map[string]string{
				"apns-priority":  "10",    // High priority for immediate delivery.
				"apns-push-type": "alert", // Ensures a visible alert is displayed.
			},
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Alert: &messaging.ApsAlert{
						Title: info.Title,
						Body:  info.Message,
					},
					Badge: &info.Badge, // Assuming `info.Badge` holds a badge count.
					Sound: "default",   // Plays the default system sound.
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
				Icon:  "https://example.com/icon.png", // Replace with a valid URL for web push icon.
			},
		},
	}

	didSend, err := client.Send(context.Background(), &newMessage)
	if err != nil {
		return err
	}

	p.logger.Info(fmt.Sprintf("Did send: %v", didSend))

	return nil
}

func (p *PushNotificationService) SendPushExpo(info *PushNotificationInfo) error {
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
		return err
	}

	// Validate responses
	if response.ValidateResponse() != nil {
		return fmt.Errorf("failed: %v", response.PushMessage.To)
	}

	return nil

}

func (p *PushNotificationService) getUserPushTokens(ctx context.Context, userID int64) (*struct {
	FCMToken  string
	ExpoToken string
}, error) {
	tokens, err := p.userService.GetUserPushTokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	var fcmToken, expoToken string
	for _, token := range tokens {
		switch token.Provider {
		case "fcm":
			fcmToken = token.Token
		case "expo":
			expoToken = token.Token
		}
	}
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
func (p *PushNotificationService) SendVaultGoalCreatedPush(ctx context.Context, userID int64, name string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Vault Goal Created"
	Message := fmt.Sprintf("Your vault goal '%s' as been created", name)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendGoalCompletedPush(ctx context.Context, userID int64, name string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Vault Goal Completed"
	Message := fmt.Sprintf("Congratulations! Your vault goal '%s' has been completed.", name)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendDepositSuccessPush(ctx context.Context, userID int64, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Deposit Successful"
	Message := fmt.Sprintf("Your vault deposit of %s %s to '%s' was successful.", amount, currency, name)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendWithdrawalSuccessPush(ctx context.Context, userID int64, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Withdrawal Successful"
	Message := fmt.Sprintf("Your vault withdrawal of %s %s from '%s' was successful.", amount, currency, name)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendRecurringDepositSuccessPush(ctx context.Context, userID int64, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Recurring Deposit Successful"
	Message := fmt.Sprintf("Your vault recurring deposit of %s %s to '%s' was successful.", amount, currency, name)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendRecurringDepositFailedPush(ctx context.Context, userID int64, name, reason string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Recurring Deposit Failed"
	Message := fmt.Sprintf("Your vault recurring deposit to '%s' has failed. Reason: %s", name, reason)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendYieldCredited(ctx context.Context, userID int64, name, amount, currency string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Yield Credited"
	Message := fmt.Sprintf("Your vault '%s' has been credited with %s %s as yield.", name, amount, currency)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendRewardNotification(ctx context.Context, userID int64, message, txType string, pointEarned int64) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Reward Earned"
	Message := fmt.Sprintf("%s You have earned %d points from %s.", message, pointEarned, txType)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}

func (p *PushNotificationService) SendWalletTransfer(ctx context.Context, userID int64, amount float64, message string) error {
	tokens, err := p.getUserPushTokens(ctx, userID)
	if err != nil {
		p.logger.Error(fmt.Sprintf("Error getting user push tokens: %v", err))
		return err
	}

	if userID == 0 || (tokens.FCMToken == "" && tokens.ExpoToken == "") {
		p.logger.Info("No push tokens found for user")
		return nil
	}

	Title := "Wallet Transfer"
	Message := fmt.Sprintf("Your wallet transfer of %.2f was successful. %s", amount, message)

	if tokens.FCMToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:        Title,
			Message:      Message,
			Provider:     PushProviderFCM,
			UserFCMToken: tokens.FCMToken,
			Badge:        1,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending FCM push notification: %v", err))
		}
	}

	if tokens.ExpoToken != "" {
		err = p.SendPush(&PushNotificationInfo{
			Title:         Title,
			Message:       Message,
			Provider:      PushProviderExpo,
			UserExpoToken: tokens.ExpoToken,
		})
		if err != nil {
			p.logger.Error(fmt.Sprintf("Error sending Expo push notification: %v", err))
		}
	}
	return nil
}
