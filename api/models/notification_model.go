package models

import (
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type NotificationResponse struct {
	ID        int32     `json:"id"`
	UserID    int32     `json:"user_id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

func ToNotificationResponse(n *db.ListNotificationsByUserRow) *NotificationResponse {
	return &NotificationResponse{
		ID:        n.ID,
		UserID:    n.UserID.Int32,
		Title:     n.Title,
		Message:   n.Message,
		Read:      n.Read.Bool,
		CreatedAt: n.CreatedAt.Time,
	}
}
