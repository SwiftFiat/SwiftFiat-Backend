-- =====================================================
-- TRANSACTION VOLUME TRACKING QUERIES
-- Add these to your queries.sql file
-- =====================================================

-- 1. Get User Transaction Metrics (for VIP assignment)
-- name: GetUserTransactionMetrics :one
SELECT 
    u.id AS user_id,
    COALESCE(SUM(
        CASE 
            WHEN t.type IN ('airtime', 'data', 'tv', 'electricity', 'other') 
            THEN CAST(sm.sent_amount AS DECIMAL(20, 2))
            WHEN t.type IN ('swap', 'transfer')
            THEN CAST(stm.sent_amount AS DECIMAL(20, 2))
            ELSE 0
        END
    ), 0) AS total_volume,
    COALESCE(COUNT(DISTINCT t.id), 0) AS conversion_count,
    COALESCE(SUM(
        CASE 
            WHEN t.created_at >= NOW() - INTERVAL '30 days'
            AND t.type IN ('airtime', 'data', 'tv', 'electricity', 'other')
            THEN CAST(sm.sent_amount AS DECIMAL(20, 2))
            WHEN t.created_at >= NOW() - INTERVAL '30 days'
            AND t.type IN ('swap', 'transfer')
            THEN CAST(stm.sent_amount AS DECIMAL(20, 2))
            ELSE 0
        END
    ), 0) AS monthly_volume,
    MAX(t.created_at) AS last_transaction_date
FROM users u
LEFT JOIN swift_wallets w ON w.customer_id = u.id
LEFT JOIN transactions t ON t.id IN (
    SELECT sm.transaction_id FROM services_metadata sm WHERE sm.source_wallet = w.id
    UNION
    SELECT stm.transaction_id FROM swap_transfer_metadata stm WHERE stm.source_wallet = w.id
)
LEFT JOIN services_metadata sm ON sm.transaction_id = t.id
LEFT JOIN swap_transfer_metadata stm ON stm.transaction_id = t.id
WHERE u.id = $1
    AND t.status = 'success'
    AND t.deleted_at IS NULL
GROUP BY u.id;

-- 8. Get Users Eligible for VIP Upgrade
-- name: GetUsersEligibleForVIPUpgrade :many
SELECT 
    u.id AS user_id,
    u.email,
    COALESCE(uva.vip_level_id, (SELECT id FROM vip_levels WHERE is_default = TRUE LIMIT 1)) AS current_level_id,
    metrics.total_volume,
    metrics.conversion_count,
    next_level.id AS eligible_level_id,
    next_level.level_name AS eligible_level_name
FROM users u
LEFT JOIN user_vip_assignments uva ON uva.user_id = u.id AND uva.is_active = TRUE
CROSS JOIN LATERAL (
    SELECT 
        COALESCE(SUM(
            CASE 
                WHEN t.type IN ('airtime', 'data', 'tv', 'electricity', 'other')
                THEN CAST(sm.sent_amount AS DECIMAL(20, 2))
                WHEN t.type IN ('swap', 'transfer')
                THEN CAST(stm.sent_amount AS DECIMAL(20, 2))
                ELSE 0
            END
        ), 0) AS total_volume,
        COUNT(DISTINCT t.id) AS conversion_count
    FROM swift_wallets w
    LEFT JOIN transactions t ON t.id IN (
        SELECT sm.transaction_id FROM services_metadata sm WHERE sm.source_wallet = w.id
        UNION
        SELECT stm.transaction_id FROM swap_transfer_metadata stm WHERE stm.source_wallet = w.id
    )
    LEFT JOIN services_metadata sm ON sm.transaction_id = t.id
    LEFT JOIN swap_transfer_metadata stm ON stm.transaction_id = t.id
    WHERE w.customer_id = u.id
        AND t.status = 'success'
        AND t.deleted_at IS NULL
) AS metrics
CROSS JOIN LATERAL (
    SELECT *
    FROM vip_levels
    WHERE min_transaction_volume <= metrics.total_volume
        AND is_active = TRUE
        AND deleted_at IS NULL
        AND level_rank > COALESCE(
            (SELECT level_rank FROM vip_levels WHERE id = uva.vip_level_id),
            0
        )
    ORDER BY level_rank DESC
    LIMIT 1
) AS next_level
WHERE u.deleted_at IS NULL
    AND (uva.vip_level_id IS NULL OR next_level.id != uva.vip_level_id);

-- 9. Get Expired VIP Assignments
-- name: GetExpiredVIPAssignments :many
SELECT 
    uva.*,
    u.email,
    vl.level_name
FROM user_vip_assignments uva
JOIN users u ON u.id = uva.user_id
JOIN vip_levels vl ON vl.id = uva.vip_level_id
WHERE uva.is_active = TRUE
    AND uva.expires_at IS NOT NULL
    AND uva.expires_at <= NOW();

-- 10. Track Bill Transaction Volume (Real-time)
-- name: RecordBillTransactionVolume :exec
-- This should be called after each successful bill transaction
INSERT INTO user_transaction_Volumes (
    user_id,
    transaction_type,
    amount,
    currency,
    transaction_date
) VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (user_id, transaction_date)
DO UPDATE SET
    daily_volume = user_transaction_volumes.daily_volume + EXCLUDED.amount,
    daily_count = user_transaction_volumes.daily_count + 1,
    updated_at = NOW();

-- =====================================================
-- MATERIALIZED VIEW FOR FASTER QUERIES (Optional but Recommended)
-- =====================================================

-- Create a materialized view for user metrics (refresh periodically)
CREATE MATERIALIZED VIEW IF NOT EXISTS user_vip_metrics AS
SELECT 
    u.id AS user_id,
    u.email,
    u.phone_number,
    COALESCE(SUM(
        CASE 
            WHEN t.type IN ('airtime', 'data', 'tv', 'electricity', 'other')
            THEN CAST(sm.sent_amount AS DECIMAL(20, 2))
            WHEN t.type IN ('swap', 'transfer')
            THEN CAST(stm.sent_amount AS DECIMAL(20, 2))
            ELSE 0
        END
    ), 0) AS total_transaction_volume,
    COALESCE(COUNT(DISTINCT t.id), 0) AS total_conversion_count,
    COALESCE(SUM(
        CASE 
            WHEN t.created_at >= NOW() - INTERVAL '30 days'
            AND t.type IN ('airtime', 'data', 'tv', 'electricity', 'other')
            THEN CAST(sm.sent_amount AS DECIMAL(20, 2))
            WHEN t.created_at >= NOW() - INTERVAL '30 days'
            AND t.type IN ('swap', 'transfer')
            THEN CAST(stm.sent_amount AS DECIMAL(20, 2))
            ELSE 0
        END
    ), 0) AS monthly_volume,
    MAX(t.created_at) AS last_transaction_date,
    uva.vip_level_id AS current_vip_level_id,
    vl.level_name AS current_vip_level_name,
    vl.level_rank AS current_vip_level_rank
FROM users u
LEFT JOIN swift_wallets w ON w.customer_id = u.id
LEFT JOIN transactions t ON t.id IN (
    SELECT sm.transaction_id FROM services_metadata sm WHERE sm.source_wallet = w.id
    UNION
    SELECT stm.transaction_id FROM swap_transfer_metadata stm WHERE stm.source_wallet = w.id
)
LEFT JOIN services_metadata sm ON sm.transaction_id = t.id
LEFT JOIN swap_transfer_metadata stm ON stm.transaction_id = t.id
LEFT JOIN user_vip_assignments uva ON uva.user_id = u.id AND uva.is_active = TRUE
LEFT JOIN vip_levels vl ON vl.id = uva.vip_level_id
WHERE u.deleted_at IS NULL
    AND (t.status = 'success' OR t.id IS NULL)
    AND (t.deleted_at IS NULL OR t.id IS NULL)
GROUP BY u.id, u.email, u.phone_number, uva.vip_level_id, vl.level_name, vl.level_rank;

-- Create index on materialized view
CREATE INDEX idx_user_vip_metrics_user_id ON user_vip_metrics(user_id);
CREATE INDEX idx_user_vip_metrics_volume ON user_vip_metrics(total_transaction_volume DESC);

-- Refresh function (call this periodically via scheduler)
-- name: RefreshUserVIPMetrics :exec
REFRESH MATERIALIZED VIEW CONCURRENTLY user_vip_metrics_cache;