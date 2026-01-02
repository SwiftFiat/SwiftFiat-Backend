-- =====================================================
-- RATE MANAGER SQLC QUERIES
-- =====================================================

-- =====================================================
-- VIP LEVELS QUERIES
-- =====================================================

-- name: CreateVIPLevel :one
INSERT INTO vip_levels (
    level_name,
    level_code,
    level_rank,
    min_conversion_volume,
    description,
    benefits_description,
    badge_color,
    icon_url,
    is_active,
    created_by,
    updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: GetVIPLevelByID :one
SELECT * FROM vip_levels
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetVIPLevelByCode :one
SELECT * FROM vip_levels
WHERE level_code = $1 AND deleted_at IS NULL;

-- name: GetVIPLevelByRank :one
SELECT * FROM vip_levels
WHERE level_rank = $1 AND deleted_at IS NULL AND is_active = TRUE;

-- name: ListVIPLevels :many
SELECT * FROM vip_levels
WHERE deleted_at IS NULL
ORDER BY level_rank ASC;

-- name: ListActiveVIPLevels :many
SELECT * FROM vip_levels
WHERE deleted_at IS NULL AND is_active = TRUE
ORDER BY level_rank ASC;

-- name: GetDefaultVIPLevel :one
SELECT * FROM vip_levels
WHERE is_default = TRUE AND deleted_at IS NULL AND is_active = TRUE
LIMIT 1;

-- name: GetVIPLevelForVolume :one
SELECT * FROM vip_levels
WHERE min_conversion_volume <= $1 
  AND deleted_at IS NULL 
  AND is_active = TRUE
ORDER BY min_conversion_volume DESC
LIMIT 1;

-- name: UpdateVIPLevel :one
UPDATE vip_levels
SET 
    level_name = COALESCE(sqlc.narg('level_name'), level_name),
    level_code = COALESCE(sqlc.narg('level_code'), level_code),
    level_rank = COALESCE(sqlc.narg('level_rank'), level_rank),
    min_conversion_volume = COALESCE(sqlc.narg('min_conversion_volume'), min_conversion_volume),
    description = COALESCE(sqlc.narg('description'), description),
    benefits_description = COALESCE(sqlc.narg('benefits_description'), benefits_description),
    badge_color = COALESCE(sqlc.narg('badge_color'), badge_color),
    icon_url = COALESCE(sqlc.narg('icon_url'), icon_url),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    updated_by = $2
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteVIPLevel :one
UPDATE vip_levels
SET deleted_at = NOW(), updated_by = $2
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CheckVIPLevelNameExists :one
SELECT EXISTS(
    SELECT 1 FROM vip_levels
    WHERE level_name = $1 AND deleted_at IS NULL AND id != COALESCE($2, '00000000-0000-0000-0000-000000000000'::UUID)
) AS exists;

-- name: CheckVIPLevelCodeExists :one
SELECT EXISTS(
    SELECT 1 FROM vip_levels
    WHERE level_code = $1 AND deleted_at IS NULL AND id != COALESCE($2, '00000000-0000-0000-0000-000000000000'::UUID)
) AS exists;

-- name: CheckVIPLevelRankExists :one
SELECT EXISTS(
    SELECT 1 FROM vip_levels
    WHERE level_rank = $1 AND deleted_at IS NULL AND id != COALESCE($2, '00000000-0000-0000-0000-000000000000'::UUID)
) AS exists;

-- name: CountVIPLevelUsers :one
SELECT COUNT(*) FROM user_vip_assignments
WHERE vip_level_id = $1 AND is_active = TRUE;

-- =====================================================
-- RATE ADJUSTMENT RULES QUERIES
-- =====================================================

-- name: CreateRateAdjustmentRule :one
INSERT INTO rate_adjustment_rules (
    rule_name,
    rule_description,
    vip_level_id,
    is_global_rule,
    source_currency,
    target_currency,
    adjustment_type,
    adjustment_value,
    adjustment_direction,
    priority,
    min_conversion_amount,
    max_conversion_amount,
    valid_from,
    valid_until,
    is_active,
    created_by,
    updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
) RETURNING *;

-- name: GetRateAdjustmentRuleByID :one
SELECT * FROM rate_adjustment_rules
WHERE id = $1;

-- name: ListRateAdjustmentRules :many
SELECT 
    r.*,
    v.level_name as vip_level_name,
    v.level_code as vip_level_code
FROM rate_adjustment_rules r
LEFT JOIN vip_levels v ON r.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE r.deleted_at IS NULL
ORDER BY r.priority DESC, r.created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountRateAdjustmentRules :one
SELECT COUNT(*) FROM rate_adjustment_rules
WHERE deleted_at IS NULL;

-- name: ListActiveRateAdjustmentRules :many
SELECT 
    r.*,
    v.level_name as vip_level_name,
    v.level_code as vip_level_code
FROM rate_adjustment_rules r
LEFT JOIN vip_levels v ON r.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE r.deleted_at IS NULL 
  AND r.is_active = TRUE
  AND (r.valid_from IS NULL OR r.valid_from <= NOW())
  AND (r.valid_until IS NULL OR r.valid_until > NOW())
ORDER BY r.priority DESC, r.created_at DESC;

-- name: CountActiveRateAdjustmentRules :one
SELECT COUNT(*) FROM rate_adjustment_rules
WHERE is_active = TRUE 
    AND deleted_at IS NULL;

-- name: GetActiveGlobalRule :one
SELECT * FROM rate_adjustment_rules
WHERE is_global_rule = TRUE 
  AND is_active = TRUE 
  AND deleted_at IS NULL
  AND source_currency = $1
  AND target_currency = $2
  AND (valid_from IS NULL OR valid_from <= NOW())
  AND (valid_until IS NULL OR valid_until > NOW())
LIMIT 1;

-- name: GetActiveRuleForVIPLevel :one
SELECT * FROM rate_adjustment_rules
WHERE vip_level_id = $1
  AND is_active = TRUE
  AND deleted_at IS NULL
  AND source_currency = $2
  AND target_currency = $3
  AND (valid_from IS NULL OR valid_from <= NOW())
  AND (valid_until IS NULL OR valid_until > NOW())
  AND (min_conversion_amount IS NULL OR min_conversion_amount <= $4)
  AND (max_conversion_amount IS NULL OR max_conversion_amount >= $4)
ORDER BY priority DESC
LIMIT 1;

-- name: GetApplicableRulesForUser :many
SELECT 
    r.*,
    v.level_name as vip_level_name,
    v.level_rank as vip_level_rank
FROM rate_adjustment_rules r
LEFT JOIN vip_levels v ON r.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE r.deleted_at IS NULL
  AND r.is_active = TRUE
  AND r.source_currency = $1
  AND r.target_currency = $2
  AND (r.valid_from IS NULL OR r.valid_from <= NOW())
  AND (r.valid_until IS NULL OR r.valid_until > NOW())
  AND (r.min_conversion_amount IS NULL OR r.min_conversion_amount <= $3)
  AND (r.max_conversion_amount IS NULL OR r.max_conversion_amount >= $3)
  AND (
    r.is_global_rule = TRUE 
    OR r.vip_level_id IN (
        SELECT vip_level_id FROM user_vip_assignments 
        WHERE user_id = $4 AND is_active = TRUE
    )
  )
ORDER BY r.priority DESC, v.level_rank DESC NULLS LAST
LIMIT 1;

-- name: UpdateRateAdjustmentRule :one
UPDATE rate_adjustment_rules
SET 
    rule_name = COALESCE(sqlc.narg('rule_name'), rule_name),
    rule_description = COALESCE(sqlc.narg('rule_description'), rule_description),
    adjustment_type = COALESCE(sqlc.narg('adjustment_type'), adjustment_type),
    adjustment_value = COALESCE(sqlc.narg('adjustment_value'), adjustment_value),
    adjustment_direction = COALESCE(sqlc.narg('adjustment_direction'), adjustment_direction),
    priority = COALESCE(sqlc.narg('priority'), priority),
    min_conversion_amount = COALESCE(sqlc.narg('min_conversion_amount'), min_conversion_amount),
    max_conversion_amount = COALESCE(sqlc.narg('max_conversion_amount'), max_conversion_amount),
    valid_from = COALESCE(sqlc.narg('valid_from'), valid_from),
    valid_until = COALESCE(sqlc.narg('valid_until'), valid_until),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    updated_by = $2
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteRateAdjustmentRule :exec
DELETE FROM rate_adjustment_rules
WHERE id = $1;

-- name: ToggleRateAdjustmentRule :one
UPDATE rate_adjustment_rules
SET is_active = $2, updated_by = $3
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CountRateAdjustmentRulesByVIPLevel :one
SELECT COUNT(*) FROM rate_adjustment_rules
WHERE vip_level_id = $1 AND deleted_at IS NULL;

-- name: GetNextVIPLevel :one
SELECT * FROM vip_levels
WHERE level_rank > $1
    AND is_active = TRUE
    AND deleted_at IS NULL
ORDER BY level_rank ASC
LIMIT 1;

-- name: GetActiveRulesForUser :many
SELECT rar.*
FROM rate_adjustment_rules rar
JOIN user_vip_assignments uva ON uva.vip_level_id = rar.vip_level_id
WHERE uva.vip_level_id = $1
    AND uva.is_active = TRUE
    AND rar.is_active = TRUE
    AND rar.deleted_at IS NULL
    AND (rar.valid_from IS NULL OR rar.valid_from <= NOW())
    AND (rar.valid_until IS NULL OR rar.valid_until >= NOW());

-- =====================================================
-- USER VIP ASSIGNMENTS QUERIES
-- =====================================================

-- name: AssignUserToVIPLevel :one
INSERT INTO user_vip_assignments (
    user_id,
    vip_level_id,
    assigned_by,
    assignment_type,
    total_conversion_volume,
    -- total_conversion_count,
    expires_at 
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (user_id) WHERE is_active = TRUE
DO UPDATE SET
    vip_level_id = EXCLUDED.vip_level_id,
    assigned_at = NOW(),
    assigned_by = EXCLUDED.assigned_by,
    assignment_type = EXCLUDED.assignment_type,
    total_conversion_volume = EXCLUDED.total_conversion_volume,
    expires_at = EXCLUDED.expires_at,
    updated_at = NOW()
RETURNING *;

-- name: GetActiveVIPAssignment :one
SELECT * FROM user_vip_assignments
WHERE user_id = $1
    AND is_active = TRUE
    AND (expires_at IS NULL OR expires_at > NOW())
LIMIT 1;

-- name: GetUserVIPAssignment :one
SELECT 
    uva.*,
    v.level_name,
    v.level_code,
    v.level_rank,
    v.badge_color,
    v.benefits_description
FROM user_vip_assignments uva
JOIN vip_levels v ON uva.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE uva.user_id = $1 AND uva.is_active = TRUE
LIMIT 1;

-- name: GetUserVIPLevel :one
SELECT v.*
FROM user_vip_assignments uva
JOIN vip_levels v ON uva.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE uva.user_id = $1 AND uva.is_active = TRUE
LIMIT 1;

-- name: DeactivateUserVIPAssignment :one
UPDATE user_vip_assignments
SET is_active = FALSE, updated_at = NOW()
WHERE user_id = $1 AND is_active = TRUE
RETURNING *;

-- name: ListUserVIPHistory :many
SELECT 
    uva.*,
    v.level_name,
    v.level_code,
    v.level_rank
FROM user_vip_assignments uva
JOIN vip_levels v ON uva.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE uva.user_id = $1
ORDER BY uva.assigned_at DESC
LIMIT $2 OFFSET $3;

-- name: ListUsersInVIPLevel :many
SELECT 
    uva.*,
    u.email,
    u.first_name,
    u.last_name
FROM user_vip_assignments uva
JOIN users u ON uva.user_id = u.id AND u.deleted_at IS NULL
WHERE uva.vip_level_id = $1 AND uva.is_active = TRUE
ORDER BY uva.assigned_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUsersInVIPLevel :one
SELECT COUNT(*) FROM user_vip_assignments
WHERE vip_level_id = $1 AND is_active = TRUE;

-- name: CheckExpiredVIPAssignments :many
SELECT * FROM user_vip_assignments
WHERE is_active = TRUE 
  AND expires_at IS NOT NULL 
  AND expires_at <= NOW();

-- =====================================================
-- RATE CHANGE HISTORY QUERIES
-- =====================================================

-- name: RecordRateChange :one
INSERT INTO rate_change_history (
    source_currency,
    target_currency,
    base_rate,
    adjusted_rate,
    adjustment_amount,
    rule_id,
    rule_name,
    vip_level_id,
    vip_level_name,
    rate_provider,
    applied_to_user_id,
    conversion_id,
    change_reason,
    changed_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: GetRateChangeByID :one
SELECT * FROM rate_change_history
WHERE id = $1;

-- name: ListRateChanges :many
SELECT 
    rch.*,
    u.email as user_email
FROM rate_change_history rch
LEFT JOIN users u ON rch.applied_to_user_id = u.id
WHERE ($1::varchar IS NULL OR rch.source_currency = $1)
  AND ($2::varchar IS NULL OR rch.target_currency = $2)
  AND ($3::timestamptz IS NULL OR rch.created_at >= $3)
  AND ($4::timestamptz IS NULL OR rch.created_at <= $4)
ORDER BY rch.created_at DESC
LIMIT $5 OFFSET $6;

-- name: GetRateChangesForUser :many
SELECT * FROM rate_change_history
WHERE applied_to_user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRateChangesForRule :many
SELECT * FROM rate_change_history
WHERE rule_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRateStatistics :one
SELECT 
    COUNT(*) as total_changes,
    AVG(adjustment_amount) as avg_adjustment,
    MIN(base_rate) as min_base_rate,
    MAX(base_rate) as max_base_rate,
    MIN(adjusted_rate) as min_adjusted_rate,
    MAX(adjusted_rate) as max_adjusted_rate
FROM rate_change_history
WHERE source_currency = $1 
  AND target_currency = $2
  AND created_at >= $3
  AND created_at <= $4;

-- =====================================================
-- ADMIN NOTIFICATIONS QUERIES
-- =====================================================

-- name: CreateRateAdminNotification :one
INSERT INTO rate_admin_notifications (
    notification_type,
    severity,
    title,
    message,
    related_entity_type,
    related_entity_id,
    metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetRateAdminNotificationByID :one
SELECT * FROM rate_admin_notifications
WHERE id = $1;

-- name: ListRateAdminNotifications :many
SELECT * FROM rate_admin_notifications
WHERE ($1::boolean IS NULL OR is_read = $1)
  AND ($2::varchar IS NULL OR severity = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: MarkRateAdminNotificationAsRead :one
UPDATE rate_admin_notifications
SET is_read = TRUE, read_at = NOW(), read_by = $2
WHERE id = $1
RETURNING *;

-- name: MarkAllRateAdminNotificationsAsRead :exec
UPDATE rate_admin_notifications
SET is_read = TRUE, read_at = NOW(), read_by = $1
WHERE is_read = FALSE;

-- name: CountUnreadRateAdminNotifications :one
SELECT COUNT(*) FROM rate_admin_notifications
WHERE is_read = FALSE;

-- name: DeleteRateAdminNotification :exec
DELETE FROM rate_admin_notifications
WHERE id = $1;

-- =====================================================
-- ANALYTICS & REPORTING QUERIES
-- =====================================================

-- name: GetVIPLevelDistribution :many
SELECT 
    v.level_name,
    v.level_code,
    v.level_rank,
    COUNT(uva.id) as user_count,
    SUM(uva.total_conversion_volume) as total_volume
FROM vip_levels v
LEFT JOIN user_vip_assignments uva ON v.id = uva.vip_level_id AND uva.is_active = TRUE
WHERE v.deleted_at IS NULL AND v.is_active = TRUE
GROUP BY v.id, v.level_name, v.level_code, v.level_rank
ORDER BY v.level_rank ASC;

-- name: GetRateAdjustmentImpact :one
SELECT 
    COUNT(*) as total_adjustments,
    SUM(adjustment_amount) as total_adjustment_value,
    AVG(adjustment_amount) as avg_adjustment_value,
    COUNT(DISTINCT applied_to_user_id) as unique_users_affected
FROM rate_change_history
WHERE rule_id = $1
  AND created_at >= $2
  AND created_at <= $3;

-- name: GetTopVIPUsers :many
SELECT 
    u.id,
    u.email,
    u.first_name,
    u.last_name,
    uva.total_conversion_volume,
    v.level_name as vip_level
FROM user_vip_assignments uva
JOIN users u ON uva.user_id = u.id AND u.deleted_at IS NULL
JOIN vip_levels v ON uva.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE uva.is_active = TRUE
ORDER BY uva.total_conversion_volume DESC
LIMIT $1;

-- name: DeactivateVIPAssignment :one
UPDATE user_vip_assignments
SET is_active = FALSE, updated_at = NOW()
WHERE id = $1
RETURNING *;


-- name: GetUserVIPStatus :one
SELECT 
    uva.id,
    uva.user_id,
    uva.vip_level_id,
    uva.is_active,
    uva.total_conversion_volume,
    v.level_name as vip_level,
    v.level_code as vip_code,
    v.level_rank as vip_rank,
    v.min_conversion_volume as vip_min_volume,
    v.description as vip_description,
    v.benefits_description as vip_benefits_description,
    v.badge_color as vip_badge_color,
    v.icon_url as vip_icon_url,
    v.is_active as vip_is_active,
    v.created_at as vip_created_at,
    v.updated_at as vip_updated_at,
    v.deleted_at as vip_deleted_at
FROM user_vip_assignments uva
JOIN vip_levels v ON uva.vip_level_id = v.id AND v.deleted_at IS NULL
WHERE uva.user_id = $1 AND uva.is_active = TRUE
LIMIT 1;

-- name: IncrementUserConversionVolume :exec
UPDATE user_vip_assignments
SET total_conversion_volume = (CAST(total_conversion_volume AS DECIMAL(20, 2)) + $2)::TEXT,
    updated_at = NOW()
WHERE user_id = $1 AND is_active = TRUE;

-- name: GetTotalConversionVolumeForUser :one
SELECT CAST(COALESCE(SUM(CAST(total_conversion_volume AS DECIMAL(20, 2))), 0) AS INTEGER) AS total_volume
FROM user_vip_assignments
WHERE user_id = $1 AND is_active = TRUE;

-- name: GetUserWithVIPFields :one
SELECT u.id, u.total_conversion_volume, u.total_transaction_volume, u.current_vip_level_id,
       vl.level_name, vl.level_code, vl.level_rank, vl.min_conversion_volume, vl.badge_color, vl.benefits_description
FROM users u
LEFT JOIN vip_levels vl ON u.current_vip_level_id = vl.id AND vl.deleted_at IS NULL
WHERE u.id = $1 AND u.deleted_at IS NULL;

-- name: UpdateUserVIPFields :exec
UPDATE users
SET total_conversion_volume = $2,
    total_transaction_volume = $3,
    current_vip_level_id = $4,
    updated_at = NOW()
WHERE id = $1;