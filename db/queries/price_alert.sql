-- name: CreatePriceAlert :one
INSERT INTO price_alerts (
    user_id,
    source_currency,
    target_currency,
    alert_condition,
    alert_type,
    priority,
    target_rate,
    percentage_change,
    range_min,
    range_max,
    baseline_rate,
    trailing_distance,
    max_trailing_rate,
    min_trailing_rate,
    description,
    label,
    expires_at,
    notify_push,
    notify_in_app
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19
) RETURNING *;

-- name: GetPriceAlert :one
SELECT * FROM price_alerts
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserAlerts :many
SELECT * FROM price_alerts
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetUserActiveAlerts :many
SELECT * FROM price_alerts
WHERE user_id = $1 AND is_active = true AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetActivePriceAlerts :many
SELECT * FROM price_alerts
WHERE is_active = true 
AND deleted_at IS NULL
AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY priority DESC, last_checked_at ASC NULLS FIRST;

-- name: GetActiveAlertCount :one
SELECT COUNT(*) FROM price_alerts
WHERE is_active = true 
AND deleted_at IS NULL
AND (expires_at IS NULL OR expires_at > NOW());

-- name: GetAllPriceAlerts :many
SELECT * FROM price_alerts
WHERE deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdatePriceAlert :one
UPDATE price_alerts
SET 
    target_rate = COALESCE(sqlc.narg(target_rate), target_rate),
    percentage_change = COALESCE(sqlc.narg(percentage_change), percentage_change),
    range_min = COALESCE(sqlc.narg(range_min), range_min),
    range_max = COALESCE(sqlc.narg(range_max), range_max),
    description = COALESCE(sqlc.narg(description), description),
    label = COALESCE(sqlc.narg(label), label),
    expires_at = COALESCE(sqlc.narg(expires_at), expires_at),
    notify_push = COALESCE(sqlc.narg(notify_push), notify_push),
    notify_in_app = COALESCE(sqlc.narg(notify_in_app), notify_in_app)
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateAlertStatus :exec
UPDATE price_alerts
SET is_active = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateAlertBaseline :exec
UPDATE price_alerts
SET baseline_rate = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateTrailingAlert :exec
UPDATE price_alerts
SET 
    max_trailing_rate = COALESCE(sqlc.narg(max_trailing_rate), max_trailing_rate),
    min_trailing_rate = COALESCE(sqlc.narg(min_trailing_rate), min_trailing_rate),
    target_rate = COALESCE(sqlc.narg(target_rate), target_rate)
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateAlertTrigger :exec
UPDATE price_alerts
SET 
    triggered_count = $2,
    last_triggered_at = $3,
    last_checked_at = $4,
    is_active = COALESCE(sqlc.narg(is_active), is_active),
    baseline_rate = COALESCE(sqlc.narg(baseline_rate), baseline_rate)
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateAlertLastChecked :exec
UPDATE price_alerts
SET last_checked_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;

-- name: DeletePriceAlert :exec
UPDATE price_alerts
SET deleted_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: DeleteExpiredAlerts :one
WITH deleted AS (
    DELETE FROM price_alerts
    WHERE expires_at < $1
    AND is_active = false
    AND deleted_at IS NULL
    RETURNING id
)
SELECT COUNT(*) FROM deleted;

-- name: CreateAlertTriggerHistory :one
INSERT INTO alert_trigger_history (
    alert_id,
    user_id,
    current_rate,
    previous_rate,
    change_percent,
    alert_condition,
    target_rate,
    push_notification_sent,
    in_app_notification_sent
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetAlertTriggerHistory :many
SELECT * FROM alert_trigger_history
WHERE alert_id = $1
ORDER BY triggered_at DESC
LIMIT $2 OFFSET $3;

-- name: GetUserAlertStats :one
SELECT 
    COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_alerts,
    COUNT(*) FILTER (WHERE is_active = true AND deleted_at IS NULL) as active_alerts,
    SUM(triggered_count) FILTER (WHERE deleted_at IS NULL) as triggered_count,
    jsonb_object_agg(
        alert_type,
        COUNT(*) FILTER (WHERE deleted_at IS NULL)
    ) as alerts_by_type,
    jsonb_object_agg(
        priority,
        COUNT(*) FILTER (WHERE deleted_at IS NULL)
    ) as alerts_by_priority
FROM price_alerts
WHERE user_id = $1
GROUP BY user_id;