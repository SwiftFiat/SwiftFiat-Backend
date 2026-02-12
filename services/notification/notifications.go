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

func (n *Notification) Create(ctx context.Context, senderAdmin *int64, title, message, source string) (*db.Notification, error) {
	var sender sql.NullInt64
	if senderAdmin != nil {
		sender = sql.NullInt64{
			Int64: *senderAdmin,
			Valid: true,
		}
	}
	nots, err := n.store.CreateNotification(ctx, db.CreateNotificationParams{
		SenderAdminID: sender,
		Source: source,
		Title: sql.NullString{String: title, Valid: true},
		Message: message,
	})

	if err != nil {
		return nil, err
	}
	return &nots, nil
}

func(n *Notification) Get(ctx context.Context, nID int64) (*db.Notification, error) {
	not, err := n.store.GetNotificationByID(ctx, nID)
	if err != nil {
		return nil, err
	}

	return &not, nil
}

func (n *Notification) List(ctx context.Context, limit, offset int32) (*[]db.Notification, error) {
	nots, err := n.store.ListNotifications(ctx, db.ListNotificationsParams{
		Limit: limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	return &nots, nil
}

func (n *Notification) AddRecipent(ctx context.Context, userID, nID int64) error {
	err := n.store.AddNotificationRecipient(ctx, db.AddNotificationRecipientParams{
		NotificationID: nID,
		UserID: userID,
	})

	if err != nil {
		return err
	}
	return nil
}
func (n *Notification) AddBulkRecipients(ctx context.Context, nID int64, role string) error {
	err := n.store.AddNotificationRecipientsBulk(ctx, db.AddNotificationRecipientsBulkParams{
		NotificationID: nID,
		Role:     role,
	})

	if err != nil {
		return err
	}
	return nil
}

func (n *Notification) GetAllForUser(ctx context.Context, userID int64, limit, offset int32) (*[]db.GetUserNotificationsRow, error) {
	nots, err := n.store.GetUserNotifications(ctx, db.GetUserNotificationsParams{
		Limit:     limit,
		Offset: offset,
		UserID: userID,
	})
	if err != nil {
		return nil, err
	}

	return &nots, nil
}


func (n *Notification) CountUnreadForUser(ctx context.Context, userID int64) (*int64, error) {
	count, err := n.store.GetUnreadNotificationCount(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &count, nil
}

func (n *Notification) MarkAsRead(ctx context.Context, nID int64) error {
	err := n.store.MarkNotificationRead(ctx, nID)
	if err != nil {
		return err
	}
	return nil
}

func (n *Notification) MarkAllAsRead(ctx context.Context, userID int64) error {
	err := n.store.MarkAllNotificationsRead(ctx, userID)
	if err != nil {
		return err
	}
	return nil
}

// func (n *Notification) CountAllNotifications(ctx context.Context, userID int64) error {
// 	err := n.store.countAll(ctx, userID)
// 	if err != nil {
// 		return err
// 	}
// 	return nil
// }

func (n *Notification) CreateAdminAlert(ctx context.Context, severity, title, message, source string) (*db.AdminAlert, error) {
	alert, err := n.store.CreateAdminAlert(ctx, db.CreateAdminAlertParams{
		Severity: severity,
		Title: title,
		Message: message,
		Source: sql.NullString{String: source, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return &alert, nil
}

func (n *Notification) ListAdminAlerts(ctx context.Context, limit, offset int32) (*[]db.AdminAlert, error) {
	alerts, err := n.store.ListAdminAlerts(ctx, db.ListAdminAlertsParams{
		Limit: limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	return &alerts, nil
}

func(n *Notification) ListUnacknowledgedAdminAlerts(ctx context.Context) (*[]db.AdminAlert, error) {
	alerts, err := n.store.ListUnacknowledgedAdminAlerts(ctx)
	if err != nil {
		return nil, err
	}
	return &alerts, nil
}

func(n *Notification) AcknowledgeAdminAlert(ctx context.Context, nid int64) error {
	err := n.store.AcknowledgeAdminAlert(ctx, nid)
	if err != nil {
		return err
	}
	return nil
}

func (n *Notification) CreateWithRecipients(
	ctx context.Context,
	senderAdmin *int64,
	title, message, source string,
	recipients []int64,
) (*db.Notification, error) {

	var createdNotif db.Notification

	err := n.store.ExecTx(ctx, func(q *db.Queries) error {
		// handle optional sender
		var sender sql.NullInt64
		if senderAdmin != nil {
			sender = sql.NullInt64{
				Int64: *senderAdmin,
				Valid: true,
			}
		}

		notif, err := q.CreateNotification(ctx, db.CreateNotificationParams{
			SenderAdminID: sender,
			Source:        source,
			Title:         sql.NullString{String: title, Valid: title != ""},
			Message:       message,
		})
		if err != nil {
			return err
		}

		createdNotif = notif

		// add recipients
		for _, userID := range recipients {
			err := q.AddNotificationRecipient(ctx, db.AddNotificationRecipientParams{
				NotificationID: notif.ID,
				UserID:         userID,
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &createdNotif, nil
}
