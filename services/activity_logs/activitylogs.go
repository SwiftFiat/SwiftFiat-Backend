package activitylogs

import (
	"context"
	"database/sql"
	"net"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/sqlc-dev/pqtype"
)

type ActivityLog struct {
	store db.Store
}

func NewActivityLog(store db.Store) *ActivityLog {
	return &ActivityLog{
		store: store,
	}
}

type CreateActivityLogParams struct {
	UserID     *int32
	Action     string
	EntityType *string
	EntityID   *int32
	IPAddress  string
	UserAgent  string
	CreatedAt  time.Time
}

func (a *ActivityLog) Create(ctx context.Context, params CreateActivityLogParams) (db.ActivityLog, error) {
	return a.store.Queries.CreateActivityLog(ctx, db.CreateActivityLogParams{
		UserID:     toNullInt32(params.UserID),
		Action:     params.Action,
		EntityType: toNullString(params.EntityType),
		EntityID:   toNullInt32(params.EntityID),
		IpAddress:  toInet(params.IPAddress),
		UserAgent:  toNullString(&params.UserAgent),
		CreatedAt:  params.CreatedAt,
	})
}

func (r *ActivityLog) GetByUser(ctx context.Context, userID int32, limit, offset int32) ([]db.ActivityLog, error) {
	return r.store.GetActivityLogsByUser(ctx, db.GetActivityLogsByUserParams{
		UserID: toNullInt32(&userID),
		Limit:  limit,
		Offset: offset,
	})
}

func (r *ActivityLog) GetRecent(ctx context.Context, limit, offset int32) ([]db.ActivityLog, error) {
	return r.store.GetRecentActivityLogs(ctx, db.GetRecentActivityLogsParams{
		Limit:  limit,
		Offset: offset,
	})
}

// Helper functions
func toNullInt32(i *int32) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *i, Valid: true}
}

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func toInet(ip string) pqtype.Inet {
	if ip == "" {
		return pqtype.Inet{Valid: false}
	}

	// Try parsing as CIDR (e.g., "192.168.1.0/24")
	if _, ipNet, err := net.ParseCIDR(ip); err == nil {
		return pqtype.Inet{
			IPNet: *ipNet,
			Valid: true,
		}
	}

	// Try parsing as a single IP address (e.g., "192.168.1.1")
	if parsedIP := net.ParseIP(ip); parsedIP != nil {
		// Convert to a CIDR with full mask (/32 for IPv4, /128 for IPv6)
		var mask net.IPMask
		if parsedIP.To4() != nil {
			mask = net.CIDRMask(32, 32) // IPv4
		} else {
			mask = net.CIDRMask(128, 128) // IPv6
		}
		ipNet := &net.IPNet{
			IP:   parsedIP,
			Mask: mask,
		}
		return pqtype.Inet{
			IPNet: *ipNet,
			Valid: true,
		}
	}

	// Invalid IP or CIDR, return invalid
	return pqtype.Inet{Valid: false}
}
