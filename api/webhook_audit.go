package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// WebhookAuditService handles storing and replaying webhooks
type WebhookAuditService struct {
	store *db.Store
}

// NewWebhookAuditService creates a new audit service
func NewWebhookAuditService(store *db.Store) *WebhookAuditService {
	return &WebhookAuditService{store: store}
}

// StoreWebhook saves the incoming webhook for audit and replay
func (s *WebhookAuditService) StoreWebhook(
	ctx context.Context,
	signature string,
	orderID string,
	rawPayload []byte,
	sourceIP string,
) (uuid.UUID, error) {
	ip := net.ParseIP(sourceIP)
	if ip == nil {
		return uuid.Nil, fmt.Errorf("invalid source IP: %s", sourceIP)
	}

	// Determine mask based on IP version
	mask := net.CIDRMask(32, 32)
	if ip.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}

	arg := db.CreateCryptomusWebhookParams{
		Signature: signature,
		OrderID:   orderID,
		Payload:   json.RawMessage(rawPayload),
		SourceIp: pqtype.Inet{
			IPNet: net.IPNet{IP: ip, Mask: mask},
			Valid: true,
		},
		Status: "received",
	}

	// Since pqtype.Inet is tricky with just a string, we might need to use a raw query or fix the param
	// Actually, let's try to set the IPNet if sourceIP is valid
	// For simplicity, if s.store.CreateCryptomusWebhook fails due to Inet, I'll check how other parts handle it.

	// Let's use a more robust way to handle the Inet type if possible,
	// but usually sqlc handles strings for Inet if configured.
	// Looking at the generated code, it uses pqtype.Inet.

	// I'll use a helper or just try to pass it.

	webhook, err := s.store.CreateCryptomusWebhook(ctx, arg)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to store webhook: %v", err)
	}

	return webhook.ID, nil
}

// MarkWebhookProcessing marks a webhook as being processed
func (s *WebhookAuditService) MarkWebhookProcessing(
	ctx context.Context,
	webhookID uuid.UUID,
) error {
	_, err := s.store.UpdateCryptomusWebhookStatus(ctx, db.UpdateCryptomusWebhookStatusParams{
		ID:     webhookID,
		Status: "processing",
	})
	return err
}

// MarkWebhookProcessed marks a webhook as successfully processed
func (s *WebhookAuditService) MarkWebhookProcessed(
	ctx context.Context,
	webhookID uuid.UUID,
	transactionID uuid.UUID,
) error {
	_, err := s.store.UpdateCryptomusWebhookStatus(ctx, db.UpdateCryptomusWebhookStatusParams{
		ID:     webhookID,
		Status: "processed",
		ProcessedTransactionID: uuid.NullUUID{
			UUID:  transactionID,
			Valid: true,
		},
	})
	return err
}

// MarkWebhookFailed marks a webhook as failed
func (s *WebhookAuditService) MarkWebhookFailed(
	ctx context.Context,
	webhookID uuid.UUID,
	errorMsg string,
) error {
	_, err := s.store.UpdateCryptomusWebhookStatus(ctx, db.UpdateCryptomusWebhookStatusParams{
		ID:     webhookID,
		Status: "failed",
		ProcessingError: sql.NullString{
			String: errorMsg,
			Valid:  true,
		},
	})
	return err
}

// GetWebhookBySignature retrieves webhook by signature to detect duplicates
func (s *WebhookAuditService) GetWebhookBySignature(
	ctx context.Context,
	signature string,
) (*db.CryptomusWebhook, error) {
	webhook, err := s.store.GetCryptomusWebhookBySignature(ctx, signature)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &webhook, nil
}

// GetWebhookByOrderID retrieves latest webhook for an order
func (s *WebhookAuditService) GetWebhookByOrderID(
	ctx context.Context,
	orderID string,
) (*db.CryptomusWebhook, error) {
	webhook, err := s.store.GetCryptomusWebhookByOrderID(ctx, orderID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &webhook, nil
}

// GetWebhookByID retrieves a specific webhook by ID
func (s *WebhookAuditService) GetWebhookByID(
	ctx context.Context,
	webhookID uuid.UUID,
) (*db.CryptomusWebhook, error) {
	webhook, err := s.store.GetCryptomusWebhookByID(ctx, webhookID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &webhook, nil
}

// ReplayWebhook records a webhook replay attempt
func (s *WebhookAuditService) ReplayWebhook(
	ctx context.Context,
	webhookID uuid.UUID,
	replayedBy string,
	reason string,
) (uuid.UUID, error) {
	replay, err := s.store.CreateWebhookReplay(ctx, db.CreateWebhookReplayParams{
		WebhookID: webhookID,
		ReplayedBy: sql.NullString{
			String: replayedBy,
			Valid:  true,
		},
		Reason: sql.NullString{
			String: reason,
			Valid:  true,
		},
		Result: "pending",
	})
	if err != nil {
		return uuid.Nil, err
	}
	return replay.ID, nil
}

// UpdateReplayResult updates the result of a replay attempt
func (s *WebhookAuditService) UpdateReplayResult(
	ctx context.Context,
	replayID uuid.UUID,
	result string,
	errorMsg string,
) error {
	_, err := s.store.UpdateWebhookReplayResult(ctx, db.UpdateWebhookReplayResultParams{
		ID:     replayID,
		Result: result,
		ErrorMessage: sql.NullString{
			String: errorMsg,
			Valid:  errorMsg != "",
		},
	})
	return err
}

// ListWebhooks returns paginated list of webhooks for admin dashboard
func (s *WebhookAuditService) ListWebhooks(
	ctx context.Context,
	limit int,
	offset int,
	filterStatus string,
) ([]db.CryptomusWebhook, int64, error) {
	webhooks, err := s.store.ListCryptomusWebhooks(ctx, db.ListCryptomusWebhooksParams{
		Column1: filterStatus,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
	if err != nil {
		return nil, 0, err
	}

	count, err := s.store.CountCryptomusWebhooks(ctx, filterStatus)
	if err != nil {
		return nil, 0, err
	}

	return webhooks, count, nil
}
