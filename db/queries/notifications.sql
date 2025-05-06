-- name: CreateNotification :one
INSERT INTO notifications (user_id, message)
VALUES ($1, $2)
RETURNING id, user_id, message, read, created_at;

-- name: ListNotificationsByUser :many
SELECT id, user_id, message, read, created_at
FROM notifications
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: DeleteNotification :exec
DELETE FROM notifications
WHERE id = $1 AND user_id = $2;
