-- name: CreateAuditLog :one
INSERT INTO audit_logs (
    event_category,
    event_type,
    severity,
    actor_id,
    actor_type,
    actor_email,
    entity_type,
    entity_id,
    ip_address,
    user_agent,
    request_id,
    action,
    description,
    old_values,
    new_values,
    metadata,
    success,
    error_message
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
) RETURNING *;

-- name: GetAuditLogByID :one
SELECT * FROM audit_logs
WHERE id = $1;

-- name: GetAuditLogsByActor :many
SELECT * FROM audit_logs
WHERE actor_id = $1
    AND created_at >= $2
    AND created_at <= $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: GetAuditLogsByEntity :many
SELECT * FROM audit_logs
WHERE entity_type = $1
    AND entity_id = $2
    AND created_at >= $3
    AND created_at <= $4
ORDER BY created_at DESC
LIMIT $5 OFFSET $6;

-- name: GetAuditLogsByEventType :many
SELECT * FROM audit_logs
WHERE event_type = $1
    AND created_at >= $2
    AND created_at <= $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: GetAuditLogsByCategory :many
SELECT * FROM audit_logs
WHERE event_category = $1
    AND created_at >= $2
    AND created_at <= $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: GetAllAuditLogs :many
SELECT * FROM audit_logs
WHERE created_at >= $1
    AND created_at <= $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: GetAuditLogsBySeverity :many
SELECT * FROM audit_logs
WHERE severity = $1
    AND created_at >= $2
    AND created_at <= $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: GetAuditLogsByRequestID :many
SELECT * FROM audit_logs
WHERE request_id = $1
ORDER BY created_at ASC;

-- name: GetAuditLogsByIPAddress :many
SELECT * FROM audit_logs
WHERE ip_address = $1
    AND created_at >= $2
    AND created_at <= $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: GetRecentCriticalEvents :many
SELECT * FROM audit_logs
WHERE severity IN ('error', 'critical')
    AND created_at >= NOW() - INTERVAL '7 days'
ORDER BY created_at DESC
LIMIT $1;

-- name: GetUserActivityTimeline :many
SELECT 
    al.id,
    al.event_category,
    al.event_type,
    al.severity,
    al.actor_id,
    al.actor_email,
    al.entity_type,
    al.entity_id,
    al.action,
    al.description,
    al.success,
    al.ip_address,
    al.user_agent,
    al.metadata,
    al.created_at
FROM audit_logs al
WHERE al.actor_id = $1
    AND al.created_at >= $2
    AND al.created_at <= $3
ORDER BY al.created_at DESC
LIMIT $4 OFFSET $5;

-- name: SearchAuditLogs :many
SELECT * FROM audit_logs
WHERE 
    ($1::audit_event_category IS NULL OR event_category = $1)
    AND ($2::TEXT IS NULL OR event_type = $2)
    AND ($3::audit_severity IS NULL OR severity = $3)
    AND ($4::BIGINT IS NULL OR actor_id = $4)
    AND ($5::TEXT IS NULL OR entity_type = $5)
    AND ($6::TEXT IS NULL OR entity_id = $6)
    AND created_at >= $7
    AND created_at <= $8
ORDER BY created_at DESC
LIMIT $9 OFFSET $10;

-- name: CountAuditLogsByActor :one
SELECT COUNT(*) FROM audit_logs
WHERE actor_id = $1
    AND created_at >= $2
    AND created_at <= $3;

-- name: CountAuditLogsByEntity :one
SELECT COUNT(*) FROM audit_logs
WHERE entity_type = $1
    AND entity_id = $2
    AND created_at >= $3
    AND created_at <= $4;

-- name: CountFailedLoginAttempts :one
SELECT COUNT(*) FROM audit_logs
WHERE event_type LIKE 'user.login.fail%'
    AND actor_email = $1
    AND created_at >= $2;

-- name: CountEventsByCategory :many
SELECT 
    event_category,
    COUNT(*) as count
FROM audit_logs
WHERE created_at >= $1
    AND created_at <= $2
GROUP BY event_category
ORDER BY count DESC;

-- name: CountEventsBySeverity :many
SELECT 
    severity,
    COUNT(*) as count
FROM audit_logs
WHERE created_at >= $1
    AND created_at <= $2
GROUP BY severity
ORDER BY 
    CASE severity
        WHEN 'critical' THEN 1
        WHEN 'error' THEN 2
        WHEN 'warning' THEN 3
        WHEN 'info' THEN 4
    END;

-- name: GetSuspiciousActivities :many
SELECT 
    actor_id,
    actor_email,
    ip_address,
    COUNT(*) as event_count,
    MAX(created_at)::timestamptz as last_event
FROM audit_logs
WHERE 
    severity IN ('error', 'critical')
    AND created_at >= $1
GROUP BY actor_id, actor_email, ip_address
HAVING COUNT(*) >= $2
ORDER BY event_count DESC, last_event DESC
LIMIT $3;

-- name: GetEntityChangeHistory :many
SELECT 
    id,
    event_type,
    action,
    actor_id,
    actor_email,
    old_values,
    new_values,
    description,
    created_at
FROM audit_logs
WHERE entity_type = $1
    AND entity_id = $2
    AND action IN ('create', 'update', 'delete')
ORDER BY created_at ASC;

-- name: GetIPAddressActivity :many
SELECT 
    ip_address,
    COUNT(DISTINCT actor_id) as unique_users,
    COUNT(*) as total_events,
    COUNT(*) FILTER (WHERE success = false) as failed_events,
    MIN(created_at) as first_seen,
    MAX(created_at) as last_seen
FROM audit_logs
WHERE created_at >= $1
    AND ip_address IS NOT NULL
GROUP BY ip_address
HAVING COUNT(*) >= $2
ORDER BY total_events DESC
LIMIT $3;

-- name: GetAuditStatsByDateRange :one
SELECT 
    COUNT(*) as total_events,
    COUNT(DISTINCT actor_id) as unique_actors,
    COUNT(DISTINCT entity_id) as unique_entities,
    COUNT(*) FILTER (WHERE success = true) as successful_events,
    COUNT(*) FILTER (WHERE success = false) as failed_events,
    COUNT(*) FILTER (WHERE severity = 'critical') as critical_events,
    COUNT(*) FILTER (WHERE severity = 'error') as error_events,
    COUNT(*) FILTER (WHERE severity = 'warning') as warning_events
FROM audit_logs
WHERE created_at >= $1
    AND created_at <= $2;

-- name: GetRecentEntityActivity :many
SELECT 
    entity_type,
    entity_id,
    COUNT(*) as activity_count,
    MAX(created_at) as last_activity,
    COUNT(DISTINCT actor_id) as unique_actors
FROM audit_logs
WHERE created_at >= $1
GROUP BY entity_type, entity_id
ORDER BY activity_count DESC
LIMIT $2;

-- name: CheckRateLimit :one
SELECT COUNT(*) as request_count
FROM audit_logs
WHERE event_type = $1
    AND actor_id = $2
    AND created_at >= NOW() - $3::INTERVAL;