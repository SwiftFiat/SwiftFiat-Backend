-- name: CreateNotification :one
INSERT INTO notifications (user_id, title, message)
VALUES ($1, $2, $3)
RETURNING id, user_id, title, message, read, created_at;

-- name: ListNotificationsByUser :many
SELECT id, user_id, title, message, read, created_at
FROM notifications
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: DeleteNotification :exec
DELETE FROM notifications
WHERE id = $1 AND user_id = $2;

-- name: MarkNotificationAsRead :exec
UPDATE notifications
SET read = TRUE
WHERE id = $1 AND user_id = $2;

-- name: MarkAllNotificationsAsRead :exec
UPDATE notifications
SET read = TRUE
WHERE user_id = $1;

-- name: CountUnreadNotifications :one
SELECT COUNT(*) AS count
FROM notifications
WHERE user_id = $1 AND read = FALSE;

-- name: CountAllNotifications :one
SELECT COUNT(*) AS count
FROM notifications
WHERE user_id = $1;

-- name: DeleteAllNotifications :exec
DELETE FROM notifications
WHERE user_id = $1;

-- name: DeleteAllReadNotifications :exec
DELETE FROM notifications
WHERE user_id = $1 AND read = TRUE;