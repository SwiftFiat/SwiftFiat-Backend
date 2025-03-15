package notification_channel

/// We need to set up FCM for this project

import (
	"context"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
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
	client *expo.PushClient
	app    *firebase.App
	logger *logging.Logger
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
			Body:     "This is a test notification",
			Data:     map[string]string{"withSome": "data"},
			Sound:    "default",
			Title:    "Notification Title",
			Priority: expo.DefaultPriority,
		},
	)

	// Check errors
	if err != nil {
		return err
	}

	// Validate responses
	if response.ValidateResponse() != nil {
		p.logger.Error(fmt.Sprintf("failed: %v", response.ValidateResponse()))
		return fmt.Errorf("failed: %v", response.PushMessage.To)
	}

	return nil

}
