-- name: CreateActivityLog :exec
INSERT INTO activity_logs (user_id, action)
VALUES ($1, $2);


-- name: GetActivityLogsByUser :many
SELECT * FROM activity_logs
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRecentActivityLogs :many
SELECT * FROM activity_logs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountActiveUsers :one
SELECT COUNT(DISTINCT user_id) as active_users
FROM activity_logs
WHERE created_at >= $1 AND created_at < $2
  AND user_id IS NOT NULL;

-- name: DeleteOldActivityLogs :exec
DELETE FROM activity_logs
WHERE created_at < NOW() - INTERVAL '3 days';

-- name: DeleteAllActivityLogs :exec
DELETE FROM activity_logs;
