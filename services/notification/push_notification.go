package service

/// We need to set up FCM for this project

import (
	"context"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"google.golang.org/api/option"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
)

type Config struct {
	GoogleAppCredentials string `mapstructure:"GOOGLE_APPLICATION_CREDENTIALS"`
}

type PushNotificationInfo struct {
	Title          string `json:"title"`
	Message        string `json:"message"`
	UserFCMToken   string `json:"user_fcm_token"`
	Badge          int    `json:"badge"`
	AnalyticsLabel string `json:"analytics"`
}

type PushNotificationService struct {
	app    *firebase.App
	logger *logging.Logger
}

func NewPushNotificationService(logger *logging.Logger) *PushNotificationService {

	var config Config
	err := utils.LoadCustomConfig(utils.EnvPath, &config)
	if err != nil {
		logger.Error(err)
		return nil
	}

	opt := option.WithCredentialsFile(config.GoogleAppCredentials)
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		logger.Error(err)
		return nil
	}

	return &PushNotificationService{
		app:    app,
		logger: logger,
	}
}

func (p *PushNotificationService) SendPush(info *PushNotificationInfo) error {

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
