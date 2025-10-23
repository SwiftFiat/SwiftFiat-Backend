-- name: CreateAuditLog :exec
INSERT INTO audit_logs (user_id, action, ip, user_agent)
VALUES ($1, $2, $3, $4);


-- name: GetAuditLogsByUser :many
SELECT * FROM audit_logs
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetAuditLogs :many
SELECT * FROM audit_logs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountActiveUsers :one
SELECT COUNT(DISTINCT user_id) as active_users
FROM audit_logs
WHERE created_at >= $1 AND created_at < $2
  AND user_id IS NOT NULL;

-- name: DeleteOldAuditLogs :exec
DELETE FROM audit_logs
WHERE created_at < NOW() - INTERVAL '3 days';

-- name: DeleteAllAuditLogs :exec
DELETE FROM audit_logs;
