-- name: CreateActivityLog :one
INSERT INTO activity_logs (
    user_id, action, entity_type, entity_id, ip_address, user_agent, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetActivityLogsByUser :many
SELECT * FROM activity_logs
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRecentActivityLogs :many
SELECT * FROM activity_logs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;