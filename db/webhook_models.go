package db

import (
	"time"

	"github.com/google/uuid"
)

// CryptomusWebhook represents a webhook received from Cryptomus
type CryptomusWebhook struct {
	ID                     uuid.UUID
	Signature              string
	OrderID                string
	Payload                string // JSON string
	SourceIP               string
	Status                 string // received, processing, processed, failed
	ProcessedTransactionID *uuid.UUID
	RetryCount             int
	ProcessingError        *string
	ReceivedAt             time.Time
	ProcessedAt            *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// WebhookReplay represents a manual webhook replay attempt
type WebhookReplay struct {
	ID           uuid.UUID
	WebhookID    uuid.UUID
	ReplayedBy   *string
	Reason       *string
	Result       string // pending, success, failed
	ErrorMessage *string
	ReplayedAt   time.Time
	CreatedAt    time.Time
}
