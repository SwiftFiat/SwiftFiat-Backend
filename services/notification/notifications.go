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

func (n *Notification) MaskAsRead(ctx context.Context, userID int32, notID int32) error {
	if err := n.store.MarkNotificationAsRead(ctx, db.MarkNotificationAsReadParams{
		ID:     notID,
		UserID: sql.NullInt32{Valid: true, Int32: userID},
	}); err != nil {
		return err
	}

	return nil
}


func (n *Notification) MaskAllNotificationsAsRead(ctx context.Context, userID int32) error {
	err := n.store.MarkAllNotificationsAsRead(ctx, sql.NullInt32{Valid: true, Int32: userID})
	if err != nil {
		return err
	}
	return nil
}

func (n *Notification) CountUnreadNotifications(ctx context.Context, userID int32) (int64, error) {
	count, err := n.store.CountUnreadNotifications(ctx, sql.NullInt32{Valid: true, Int32: userID})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (n *Notification) CountAllNotifications(ctx context.Context, userID int32) (int64, error) {
	count, err := n.store.CountAllNotifications(ctx, sql.NullInt32{Valid: true, Int32: userID})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (n *Notification) DeleteAllNotifications(ctx context.Context, userID int32) error {
	err := n.store.DeleteAllNotifications(ctx, sql.NullInt32{Valid: true, Int32: userID})
	if err != nil {
		return err
	}
	return nil
}

func (n *Notification) DeleteAllReadNotifications(ctx context.Context, userID int32) error {
	err := n.store.DeleteAllReadNotifications(ctx, sql.NullInt32{Valid: true, Int32: userID})
	if err != nil {
		return err
	}
	return nil
}