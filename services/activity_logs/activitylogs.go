package activitylogs

import (
	"context"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type ActivityLog struct {
	store *db.Store
}

func NewActivityLog(store *db.Store) *ActivityLog {
	return &ActivityLog{
		store: store,
	}
}

func (a *ActivityLog) Create(ctx context.Context, params db.CreateAuditLogParams) error {
	err := a.store.Queries.CreateAuditLog(ctx, db.CreateAuditLogParams{
		UserID: params.UserID,
		Action: params.Action,
	})
	if err != nil {
		return err
	}
	return nil
}

func (a *ActivityLog) GetByUser(ctx context.Context, userID int32, limit, offset int32) ([]db.AuditLog, error) {
	return a.store.GetAuditLogsByUser(ctx, db.GetAuditLogsByUserParams{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
	})
}

func (a *ActivityLog) GetRecent(ctx context.Context, limit, offset int32) ([]db.AuditLog, error) {
	return a.store.GetAuditLogs(ctx, db.GetAuditLogsParams{
		Limit:  limit,
		Offset: offset,
	})
}

func (a *ActivityLog) CountActiveUsers(ctx context.Context, start, end time.Time) (int64, error) {
	count, err := a.store.CountActiveUsers(ctx, db.CountActiveUsersParams{CreatedAt: start, CreatedAt_2: end})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (a *ActivityLog) DeleteOldLogs(ctx context.Context) error {
	return a.store.DeleteOldAuditLogs(ctx)
}

type AuditLogResponse struct {
	ID        int32
	UserID    int32
	Action    string
	CreatedAt time.Time
	Ip        string
	UserAgent string
}

func ToAuditLogResponse(log db.AuditLog) AuditLogResponse {
	return AuditLogResponse{
		ID:        log.ID,
		UserID:    log.UserID,
		Action:    log.Action,
		CreatedAt: log.CreatedAt,
		Ip:        log.Ip.String,
		UserAgent: log.UserAgent.String,
	}
}
