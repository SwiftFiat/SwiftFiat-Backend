-- ===============================================
-- TRANSACTION STREAKS QUERIES
-- ===============================================

-- name: GetUserStreak :one
-- Retrieves current streak information for a user
SELECT * FROM transaction_streaks
WHERE user_id = $1;

-- name: GetUserStreakWithBadgeCount :one
-- Get streak info along with count of earned badges
SELECT 
    ts.*,
    COUNT(ub.id)::INT AS badges_earned
FROM transaction_streaks ts
LEFT JOIN user_badges ub ON ub.user_id = ts.user_id
WHERE ts.user_id = $1
GROUP BY ts.id;

-- name: GetOrCreateUserStreak :one
-- Get existing streak or create new one if doesn't exist
INSERT INTO transaction_streaks (
    user_id,
    current_streak,
    best_streak,
    total_transaction_days,
    last_transaction_date,
    streak_started_at
) VALUES ($1, 0, 0, 0, NULL, NOW())
ON CONFLICT (user_id) 
DO UPDATE SET updated_at = NOW()
RETURNING *;

-- name: UpdateUserStreak :one
-- Manually update user streak (admin function)
UPDATE transaction_streaks
SET 
    current_streak = $2,
    best_streak = GREATEST($3, best_streak),
    total_transaction_days = $4,
    last_transaction_date = $5,
    updated_at = NOW()
WHERE user_id = $1
RETURNING *;

-- name: ResetUserStreak :one
-- Reset current streak to 0 while preserving best_streak and total_days
UPDATE transaction_streaks
SET 
    current_streak = 0,
    updated_at = NOW()
WHERE user_id = $1
RETURNING *;

-- name: GetTopStreakLeaderboard :many
-- Get users with highest current streaks
SELECT 
    ts.user_id,
    u.first_name,
    u.last_name,
    u.avatar_url,
    ts.current_streak,
    ts.best_streak,
    ts.total_transaction_days,
    COUNT(ub.id)::INT AS badges_earned
FROM transaction_streaks ts
INNER JOIN users u ON u.id = ts.user_id
LEFT JOIN user_badges ub ON ub.user_id = ts.user_id
WHERE ts.current_streak > 0
GROUP BY ts.id, u.id
ORDER BY ts.current_streak DESC, ts.best_streak DESC
LIMIT $1 OFFSET $2;

-- name: GetStreakStatistics :one
-- Get platform-wide streak statistics
SELECT 
    COUNT(*)::INT AS total_users_with_streaks,
    AVG(current_streak)::DECIMAL(10,2) AS avg_current_streak,
    MAX(current_streak)::INT AS highest_current_streak,
    MAX(best_streak)::INT AS highest_best_streak,
    SUM(total_transaction_days)::BIGINT AS total_platform_transaction_days
FROM transaction_streaks
WHERE current_streak > 0;

-- name: GetUsersWithBrokenStreaks :many
-- Find users who had streaks but haven't transacted recently (for re-engagement)
SELECT 
    ts.user_id,
    u.email,
    u.first_name,
    u.last_name,
    ts.current_streak,
    ts.best_streak,
    ts.last_transaction_date,
    CURRENT_DATE - ts.last_transaction_date AS days_inactive
FROM transaction_streaks ts
INNER JOIN users u ON u.id = ts.user_id
WHERE ts.current_streak > 0
AND ts.last_transaction_date < CURRENT_DATE - INTERVAL '1 day'
ORDER BY ts.best_streak DESC
LIMIT $1 OFFSET $2;

-- ===============================================
-- BADGES QUERIES
-- ===============================================

-- name: GetAllBadges :many
-- List all available badges
SELECT * FROM badges
WHERE is_active = TRUE
ORDER BY tier_level ASC, display_order ASC;

-- name: GetBadgeByID :one
-- Get specific badge details
SELECT * FROM badges
WHERE id = $1;

-- name: GetBadgeByName :one
-- Get badge by name
SELECT * FROM badges
WHERE name = $1 AND is_active = TRUE;

-- name: GetNextMilestone :one
-- Get the next badge a user can unlock
SELECT b.*
FROM badges b
WHERE b.required_streak_days > $1
AND b.is_active = TRUE
AND NOT EXISTS (
    SELECT 1 FROM user_badges ub 
    WHERE ub.user_id = $2 AND ub.badge_id = b.id
)
ORDER BY b.required_streak_days ASC
LIMIT 1;

-- name: CreateBadge :one
-- Create a new badge (admin function)
INSERT INTO badges (
    name,
    description,
    required_streak_days,
    tier_level,
    display_order,
    icon_url
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateBadge :one
-- Update badge details (admin function)
UPDATE badges
SET 
    name = COALESCE($2, name),
    description = COALESCE($3, description),
    required_streak_days = COALESCE($4, required_streak_days),
    tier_level = COALESCE($5, tier_level),
    icon_url = COALESCE($6, icon_url),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeactivateBadge :one
-- Soft delete a badge
UPDATE badges
SET is_active = FALSE, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- ===============================================
-- USER BADGES QUERIES
-- ===============================================

-- name: GetUserBadges :many
-- Get all badges earned by a user with badge details
SELECT 
    ub.id,
    ub.earned_at,
    ub.streak_at_unlock,
    b.name AS badge_name,
    b.description AS badge_description,
    b.required_streak_days,
    b.tier_level,
    b.icon_url
FROM user_badges ub
INNER JOIN badges b ON b.id = ub.badge_id
WHERE ub.user_id = $1
ORDER BY b.tier_level ASC;

-- name: GetUserBadgesWithLockStatus :many
-- Get all badges with locked/unlocked status for a specific user
SELECT 
    b.id AS badge_id,
    b.name,
    b.description,
    b.required_streak_days,
    b.tier_level,
    b.icon_url,
    b.display_order,
    CASE WHEN ub.id IS NOT NULL THEN TRUE ELSE FALSE END AS is_unlocked,
    ub.earned_at,
    ub.streak_at_unlock,
    CASE 
        WHEN ub.id IS NOT NULL THEN 0
        ELSE b.required_streak_days - COALESCE($2, 0)
    END::int AS days_remaining
FROM badges b
LEFT JOIN user_badges ub ON ub.badge_id = b.id AND ub.user_id = $1
WHERE b.is_active = TRUE
ORDER BY b.tier_level ASC, b.display_order ASC;

-- name: CheckUserHasBadge :one
-- Check if user has earned a specific badge
SELECT EXISTS (
    SELECT 1 FROM user_badges
    WHERE user_id = $1 AND badge_id = $2
) AS has_badge;

-- name: AwardBadgeToUser :one
-- Manually award a badge to a user (called by trigger or admin)
INSERT INTO user_badges (
    user_id,
    badge_id,
    streak_at_unlock
) VALUES ($1, $2, $3)
ON CONFLICT (user_id, badge_id) DO NOTHING
RETURNING *;

-- name: GetRecentBadgeUnlocks :many
-- Get recently unlocked badges across platform (for notifications/feed)
SELECT 
    ub.id,
    ub.user_id,
    ub.earned_at,
    u.first_name,
    u.last_name,
    u.avatar_url,
    b.name AS badge_name,
    b.tier_level,
    b.icon_url AS badge_icon
FROM user_badges ub
INNER JOIN users u ON u.id = ub.user_id
INNER JOIN badges b ON b.id = ub.badge_id
WHERE ub.earned_at > NOW() - INTERVAL '7 days'
ORDER BY ub.earned_at DESC
LIMIT $1 OFFSET $2;

-- name: GetBadgeLeaderboard :many
-- Get users with most badges earned
SELECT 
    u.id AS user_id,
    u.first_name,
    u.last_name,
    u.avatar_url,
    COUNT(ub.id)::INT AS total_badges,
    MAX(b.tier_level)::INT AS highest_tier_achieved,
    MAX(ub.earned_at) AS latest_badge_earned
FROM users u
INNER JOIN user_badges ub ON ub.user_id = u.id
INNER JOIN badges b ON b.id = ub.badge_id
GROUP BY u.id
ORDER BY total_badges DESC, highest_tier_achieved DESC
LIMIT $1 OFFSET $2;

-- name: RevokeBadge :exec
-- Remove a badge from a user (admin function)
DELETE FROM user_badges
WHERE user_id = $1 AND badge_id = $2;

-- ===============================================
-- STREAK HISTORY QUERIES
-- ===============================================

-- name: GetUserStreakHistory :many
-- Get historical streak changes for a user
SELECT 
    id,
    transaction_id,
    previous_streak,
    new_streak,
    transaction_date,
    event_type,
    metadata,
    created_at
FROM transaction_streak_history
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetStreakHistoryByDateRange :many
-- Get streak history within a date range
SELECT 
    tsh.id,
    tsh.user_id,
    u.first_name,
    u.last_name,
    tsh.previous_streak,
    tsh.new_streak,
    tsh.transaction_date,
    tsh.event_type,
    tsh.created_at
FROM transaction_streak_history tsh
INNER JOIN users u ON u.id = tsh.user_id
WHERE tsh.transaction_date BETWEEN $1 AND $2
ORDER BY tsh.created_at DESC
LIMIT $3 OFFSET $4;

-- name: CreateStreakHistoryEntry :one
-- Manually log a streak event (used by triggers or admin tools)
INSERT INTO transaction_streak_history (
    user_id,
    transaction_id,
    previous_streak,
    new_streak,
    transaction_date,
    event_type,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetStreakBreakEvents :many
-- Get all streak break events for analytics
SELECT 
    tsh.user_id,
    u.first_name,
    u.last_name,
    tsh.previous_streak AS streak_lost,
    tsh.transaction_date AS break_date,
    tsh.metadata
FROM transaction_streak_history tsh
INNER JOIN users u ON u.id = tsh.user_id
WHERE tsh.event_type = 'streak_broken'
ORDER BY tsh.created_at DESC
LIMIT $1 OFFSET $2;

-- ===============================================
-- ANALYTICS QUERIES
-- ===============================================

-- name: GetDailyActiveUsers :one
-- Count users who made transactions today
SELECT COUNT(DISTINCT user_id)::INT AS daily_active_users
FROM transaction_streaks
WHERE last_transaction_date = CURRENT_DATE;

-- name: GetStreakRetentionRate :one
-- Calculate what % of users maintain their streaks
SELECT 
    COUNT(CASE WHEN current_streak > 0 THEN 1 END)::DECIMAL / 
    NULLIF(COUNT(*)::DECIMAL, 0) * 100 AS retention_rate_percentage,
    COUNT(CASE WHEN current_streak > 0 THEN 1 END)::INT AS active_streaks,
    COUNT(*)::INT AS total_users_with_history
FROM transaction_streaks;

-- name: GetStreakDistribution :many
-- Get distribution of current streaks (histogram data)
SELECT 
    CASE 
        WHEN current_streak = 0 THEN '0'
        WHEN current_streak BETWEEN 1 AND 3 THEN '1-3'
        WHEN current_streak BETWEEN 4 AND 7 THEN '4-7'
        WHEN current_streak BETWEEN 8 AND 14 THEN '8-14'
        WHEN current_streak BETWEEN 15 AND 30 THEN '15-30'
        WHEN current_streak BETWEEN 31 AND 60 THEN '31-60'
        WHEN current_streak BETWEEN 61 AND 90 THEN '61-90'
        ELSE '90+'
    END AS streak_range,
    COUNT(*)::INT AS user_count
FROM transaction_streaks
GROUP BY streak_range
ORDER BY 
    CASE streak_range
        WHEN '0' THEN 0
        WHEN '1-3' THEN 1
        WHEN '4-7' THEN 2
        WHEN '8-14' THEN 3
        WHEN '15-30' THEN 4
        WHEN '31-60' THEN 5
        WHEN '61-90' THEN 6
        ELSE 7
    END;

-- name: GetBadgeDistribution :many
-- Get count of users who have earned each badge
SELECT 
    b.name AS badge_name,
    b.tier_level,
    b.required_streak_days,
    COUNT(ub.id)::INT AS users_earned
FROM badges b
LEFT JOIN user_badges ub ON ub.badge_id = b.id
WHERE b.is_active = TRUE
GROUP BY b.id
ORDER BY b.tier_level ASC;

-- name: GetUserEngagementTrend :many
-- Get daily transaction count trend
SELECT 
    transaction_date,
    COUNT(DISTINCT user_id)::INT AS unique_users,
    COUNT(*)::INT AS total_events,
    COUNT(CASE WHEN event_type = 'streak_continued' THEN 1 END)::INT AS streaks_continued,
    COUNT(CASE WHEN event_type = 'streak_broken' THEN 1 END)::INT AS streaks_broken
FROM transaction_streak_history
WHERE transaction_date >= $1
GROUP BY transaction_date
ORDER BY transaction_date ASC;

-- ===============================================
-- ADMIN/MAINTENANCE QUERIES
-- ===============================================

-- name: RecalculateUserStreak :one
-- Recalculate streak from transaction history (recovery/fix tool)
WITH user_transaction_dates AS (
    SELECT 
        t.user_id,
        DATE(t.created_at) AS transaction_date
    FROM transactions t
    WHERE t.user_id = $1
      AND t.status = 'successful'
      AND t.deleted_at IS NULL
    GROUP BY t.user_id, DATE(t.created_at)
    ORDER BY transaction_date DESC
),
streak_gaps AS (
    SELECT
        utd.user_id,
        utd.transaction_date,
        LAG(utd.transaction_date) OVER (PARTITION BY utd.user_id ORDER BY utd.transaction_date DESC) AS prev_date,
        utd.transaction_date 
            - LAG(utd.transaction_date) OVER (PARTITION BY utd.user_id ORDER BY utd.transaction_date DESC) AS gap
    FROM user_transaction_dates utd
),
streak_calculation AS (
    SELECT 
        utd.user_id,
        COUNT(*)::INT AS total_days,
        MAX(utd.transaction_date) AS last_date,
        COUNT(*) FILTER (
            WHERE utd.transaction_date >= (
                SELECT MIN(sg.transaction_date)
                FROM streak_gaps sg
                WHERE sg.user_id = utd.user_id
                  AND sg.gap > 1
            )
        )::INT AS current_streak_calc
    FROM user_transaction_dates utd
    GROUP BY utd.user_id
)
UPDATE transaction_streaks ts
SET 
    total_transaction_days = sc.total_days,
    last_transaction_date = sc.last_date,
    current_streak = COALESCE(sc.current_streak_calc, 0),
    best_streak = GREATEST(ts.best_streak, COALESCE(sc.current_streak_calc, 0)),
    updated_at = NOW()
FROM streak_calculation sc
WHERE ts.user_id = sc.user_id
RETURNING ts.*;

-- name: BulkResetBrokenStreaks :exec
-- Manual execution of streak reset (called by cron)
UPDATE transaction_streaks
SET 
    current_streak = 0,
    updated_at = NOW()
WHERE last_transaction_date < CURRENT_DATE - INTERVAL '1 day'
AND current_streak > 0;

-- name: DeleteStreakHistory :exec
-- Clean up old history records (data retention)
DELETE FROM transaction_streak_history
WHERE created_at < $1;

-- name: GetSystemHealthCheck :one
-- Quick health check for the streaks system
SELECT 
    (SELECT COUNT(*) FROM transaction_streaks)::INT AS total_streak_records,
    (SELECT COUNT(*) FROM user_badges)::INT AS total_badges_awarded,
    (SELECT COUNT(*) FROM transaction_streak_history WHERE created_at > NOW() - INTERVAL '24 hours')::INT AS events_last_24h,
    (SELECT COUNT(*) FROM transaction_streaks WHERE current_streak > 0)::INT AS active_streaks,
    (SELECT COUNT(*) FROM badges WHERE is_active = TRUE)::INT AS active_badge_types;