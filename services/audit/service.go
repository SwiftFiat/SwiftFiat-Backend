package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
)

// Service handles audit logging business logic
type Service struct {
	store      *db.Store
	logChannel chan *LogEntry
	bufferSize int
}

// NewService creates a new audit service with async logging
func NewService(store *db.Store, bufferSize int) *Service {
	if bufferSize <= 0 {
		bufferSize = 1000 // Default buffer size
	}

	service := &Service{
		store:      store,
		logChannel: make(chan *LogEntry, bufferSize),
		bufferSize: bufferSize,
	}

	// Start async log processor
	go service.processLogs()

	return service
}

// Log creates an audit log entry asynchronously
func (s *Service) Log(entry *LogEntry) {
	// Enrich with timestamp if not provided
	if entry.Metadata == nil {
		entry.Metadata = make(map[string]any)
	}
	entry.Metadata["logged_at"] = time.Now().UTC()

	select {
	case s.logChannel <- entry:
		// Successfully queued
	default:
		// Channel full - log synchronously to avoid data loss
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.LogSync(ctx, entry)
	}
}

// LogSync creates an audit log entry synchronously
func (s *Service) LogSync(ctx context.Context, entry *LogEntry) error {
	params, err := s.buildCreateParams(entry)
	if err != nil {
		return fmt.Errorf("failed to build audit log params: %w", err)
	}

	_, err = s.store.CreateAuditLog(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}

	return nil
}

// processLogs handles async log writing
func (s *Service) processLogs() {
	for entry := range s.logChannel {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		if err := s.LogSync(ctx, entry); err != nil {
			// Todo: In production, you might want to send this to an error tracking service
			// For now, we'll just continue to prevent blocking
			fmt.Printf("Failed to write audit log: %v\n", err)
		}

		cancel()
	}
}

// buildCreateParams converts LogEntry to SQLC params
func (s *Service) buildCreateParams(entry *LogEntry) (db.CreateAuditLogParams, error) {
	params := db.CreateAuditLogParams{
		EventCategory: db.AuditEventCategory(entry.EventCategory),
		EventType:     entry.EventType,
		Severity:      db.AuditSeverity(entry.Severity),
		ActorType:     string(entry.ActorType),
		ActorID:       uuid.NullUUID{UUID: *entry.ActorID, Valid: true},
		EntityType:    entry.EntityType,
		EntityID:      entry.EntityID,
		Action:        string(entry.Action),
		Description:   entry.Description,
		IpAddress:     pqtype.Inet{IPNet: net.IPNet{IP: entry.IPAddress, Mask: net.CIDRMask(32, 32)}},
		UserAgent:     sql.NullString{String: entry.UserAgent, Valid: true},
		Success:       entry.Success,
		// ErrorMessage:  sql.NullString{String: entry.ErrorMessage, Valid: true},
	}

	// Marshal JSON fields
	if entry.OldValues != nil {
		oldJSON, err := json.Marshal(entry.OldValues)
		if err != nil {
			return params, fmt.Errorf("failed to marshal old_values: %w", err)
		}
		params.OldValues = pqtype.NullRawMessage{RawMessage: oldJSON, Valid: true}
	}

	if entry.NewValues != nil {
		newJSON, err := json.Marshal(entry.NewValues)
		if err != nil {
			return params, fmt.Errorf("failed to marshal new_values: %w", err)
		}
		params.NewValues = pqtype.NullRawMessage{RawMessage: newJSON, Valid: true}
	}

	if entry.Metadata != nil {
		metaJSON, err := json.Marshal(entry.Metadata)
		if err != nil {
			return params, fmt.Errorf("failed to marshal metadata: %w", err)
		}
		params.Metadata = pqtype.NullRawMessage{RawMessage: metaJSON, Valid: true}
	}

	return params, nil

}

// GetByID retrieves an audit log by ID
func (s *Service) GetByID(ctx context.Context, id int64) (*LogResponse, error) {
	log, err := s.store.GetAuditLogByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	return ToLogResponse(log), nil
}

// GetAllAuditLogs retrieves all audit logs
func (s *Service) GetAllAuditLogs(ctx context.Context, time1, time2 time.Time, limit, offset int32) ([]LogResponse, error) {
	logs, err := s.store.GetAllAuditLogs(ctx, db.GetAllAuditLogsParams{
		CreatedAt:   time1,
		CreatedAt_2: time2,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get all audit logs: %w", err)
	}

	responses := make([]LogResponse, len(logs))
	for i, log := range logs {
		responses[i] = *ToLogResponse(log)
	}

	return responses, nil
}

// GetUserActivity retrieves activity timeline for a user
func (s *Service) GetUserActivity(ctx context.Context, userID uuid.UUID, startDate, endDate time.Time, limit, offset int32) ([]db.GetUserActivityTimelineRow, error) {
	params := db.GetUserActivityTimelineParams{
		ActorID:     uuid.NullUUID{UUID: userID, Valid: true},
		CreatedAt:   startDate,
		CreatedAt_2: endDate,
		Limit:       limit,
		Offset:      offset,
	}

	logs, err := s.store.GetUserActivityTimeline(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get user activity: %w", err)
	}

	return logs, nil
}

// GetEntityHistory retrieves change history for an entity
func (s *Service) GetEntityHistory(ctx context.Context, entityType, entityID string) ([]LogResponse, error) {
	params := db.GetEntityChangeHistoryParams{
		EntityType: entityType,
		EntityID:   entityID,
	}

	logs, err := s.store.GetEntityChangeHistory(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity history: %w", err)
	}

	// Convert to full LogResponse
	responses := make([]LogResponse, len(logs))
	for i, log := range logs {
		var oldValues, newValues map[string]any
		if log.OldValues.Valid {
			_ = json.Unmarshal(log.OldValues.RawMessage, &oldValues)
		}
		if log.NewValues.Valid {
			_ = json.Unmarshal(log.NewValues.RawMessage, &newValues)
		}

		responses[i] = LogResponse{
			ID:          log.ID,
			EventType:   log.EventType,
			Action:      Action(log.Action),
			ActorID:     &log.ActorID.UUID,
			ActorEmail:  &log.ActorEmail.String,
			OldValues:   oldValues,
			NewValues:   newValues,
			Description: log.Description,
			CreatedAt:   log.CreatedAt,
		}
	}

	return responses, nil
}

// GetStats retrieves aggregated statistics for a date range
func (s *Service) GetStats(ctx context.Context, startDate, endDate time.Time) (*AuditStats, error) {
	params := db.GetAuditStatsByDateRangeParams{
		CreatedAt:   startDate,
		CreatedAt_2: endDate,
	}

	stats, err := s.store.GetAuditStatsByDateRange(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get audit stats: %w", err)
	}

	return &AuditStats{
		TotalEvents:      stats.TotalEvents,
		UniqueActors:     stats.UniqueActors,
		UniqueEntities:   stats.UniqueEntities,
		SuccessfulEvents: stats.SuccessfulEvents,
		FailedEvents:     stats.FailedEvents,
		CriticalEvents:   stats.CriticalEvents,
		ErrorEvents:      stats.ErrorEvents,
		WarningEvents:    stats.WarningEvents,
		StartDate:        startDate,
		EndDate:          endDate,
	}, nil
}

// GetSuspiciousActivity identifies potentially malicious behavior
func (s *Service) GetSuspiciousActivity(ctx context.Context, sinceDate time.Time, minEvents int32, limit int32) ([]SuspiciousActivity, error) {
	params := db.GetSuspiciousActivitiesParams{
		CreatedAt: sinceDate,
		Column2:   minEvents,
		Limit:     limit,
	}

	activities, err := s.store.GetSuspiciousActivities(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get suspicious activities: %w", err)
	}

	results := make([]SuspiciousActivity, len(activities))
	for i, act := range activities {
		// Convert IPNet to string and take its address so IPAddress matches *string type
		var ipStr string
		if act.IpAddress.IPNet.IP != nil {
			ipStr = act.IpAddress.IPNet.String()
		}

		results[i] = SuspiciousActivity{
			ActorID:    &act.ActorID.UUID,
			ActorEmail: &act.ActorEmail.String,
			IPAddress:  &ipStr,
			EventCount: act.EventCount,
			LastEvent:  act.LastEvent,
		}
	}

	return results, nil
}

// CheckLoginAttempts checks failed login attempts for rate limiting
func (s *Service) CheckLoginAttempts(ctx context.Context, email string, duration time.Duration) (int64, error) {
	params := db.CountFailedLoginAttemptsParams{
		ActorEmail: sql.NullString{String: email, Valid: true},
		CreatedAt:  time.Now().Add(-duration),
	}

	count, err := s.store.CountFailedLoginAttempts(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("failed to count login attempts: %w", err)
	}

	return count, nil
}

// GetRecentCritical retrieves recent critical/error events
func (s *Service) GetRecentCritical(ctx context.Context, limit int32) ([]LogResponse, error) {
	logs, err := s.store.GetRecentCriticalEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get critical events: %w", err)
	}

	return ToLogResponses(logs), nil
}
