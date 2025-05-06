package service

import (
	"context"
	"database/sql"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type Notification struct {
	store *db.Store
}

func NewNotificationService(store *db.Store) *Notification {
	return &Notification{store}
}

func (n *Notification) Create(ctx context.Context, userID int32, message string) (*db.Notification, error) {
	nots, err := n.store.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  sql.NullInt32{Int32: userID, Valid: true},
		Message: message,
	})

	if err != nil {
		return nil, err
	}
	return &nots, nil
}

func (n *Notification) Get(ctx context.Context, userID int32) ([]db.Notification, error) {
	nots, err := n.store.ListNotificationsByUser(ctx, sql.NullInt32{Int32: userID, Valid: true})

	if err != nil {
		return nil, err
	}
	return nots, nil
}
func (n *Notification) Delete(ctx context.Context, userID int32, id int32) error {
	err := n.store.DeleteNotification(ctx, db.DeleteNotificationParams{
		UserID: sql.NullInt32{Int32: userID, Valid: true},
		ID:     id,
	})

	if err != nil {
		return err
	}
	return nil
}