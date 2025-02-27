package notification_channel

// Channel represents the type of notification channel
type Channel string

const (
	EMAIL Channel = "EMAIL"
	SMS   Channel = "SMS"
	PUSH  Channel = "PUSH"
)
