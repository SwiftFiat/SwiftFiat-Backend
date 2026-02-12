-- name: CreateNotification :one
INSERT INTO notifications (
  sender_admin_id,
  source,
  title,
  message,
  metadata
) VALUES (
  $1, $2, $3, $4, $5
)
RETURNING *;


-- name: GetNotificationByID :one
SELECT *
FROM notifications
WHERE id = $1;


-- name: ListNotifications :many
SELECT *
FROM notifications
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;


-- name: AddNotificationRecipient :exec
INSERT INTO notification_recipients (
  notification_id,
  user_id
) VALUES (
  $1, $2
)
ON CONFLICT DO NOTHING;


-- name: AddNotificationRecipientsBulk :exec
INSERT INTO notification_recipients (
  notification_id,
  user_id
)
SELECT $1, id
FROM users
WHERE role = $2; -- e.g. "user", "admin"


-- name: GetUserNotifications :many
SELECT
  nr.id,
  n.title,
  n.message,
  n.source,
  n.metadata,
  nr.read,
  nr.read_at,
  n.created_at
FROM notification_recipients nr
JOIN notifications n ON n.id = nr.notification_id
WHERE nr.user_id = $1
ORDER BY n.created_at DESC
LIMIT $2 OFFSET $3;


-- name: GetUnreadNotificationCount :one
SELECT COUNT(*)
FROM notification_recipients
WHERE user_id = $1
  AND read = FALSE;


-- name: MarkNotificationRead :exec
UPDATE notification_recipients
SET
  read = TRUE,
  read_at = NOW()
WHERE id = $1
  AND read = FALSE;


-- name: MarkAllNotificationsRead :exec
UPDATE notification_recipients
SET
  read = TRUE,
  read_at = NOW()
WHERE user_id = $1
  AND read = FALSE;


-- name: CreateAdminAlert :one
INSERT INTO admin_alerts (
  severity,
  title,
  message,
  source
) VALUES (
  $1, $2, $3, $4
)
RETURNING *;

-- name: ListAdminAlerts :many
SELECT *
FROM admin_alerts
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListUnacknowledgedAdminAlerts :many
SELECT *
FROM admin_alerts
WHERE acknowledged = FALSE
ORDER BY severity DESC, created_at DESC;

-- name: AcknowledgeAdminAlert :exec
UPDATE admin_alerts
SET
  acknowledged = TRUE,
  acknowledged_at = NOW()
WHERE id = $1
  AND acknowledged = FALSE;
