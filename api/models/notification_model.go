package models

import (
	"database/sql"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type NotificationResponse struct {
	ID        int64          `json:"id"`
	Title     sql.NullString `json:"title"`
	Message   string         `json:"message"`
	Source    string         `json:"source"`
	Read      bool           `json:"read"`
	ReadAt    sql.NullTime   `json:"read_at"`
	CreatedAt time.Time      `json:"created_at"`
}

func ToNotificationResponse(n *db.GetUserNotificationsRow) *NotificationResponse {
	return &NotificationResponse{
		ID:        n.ID,
		Title:     n.Title,
		Message:   n.Message,
		Source:    n.Source,
		Read:      n.Read,
		ReadAt:    n.ReadAt,
		CreatedAt: n.CreatedAt,
	}
}
