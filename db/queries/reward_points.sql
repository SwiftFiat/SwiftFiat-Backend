-- ============================================================================
-- REWARD POINTS SYSTEM - SQLC QUERIES
-- ============================================================================
-- This file contains all SQLC queries for the reward points system
-- Organized by functionality:
-- 1. Admin: Reward Configuration Management
-- 2. User: Reward Balance & Summary
-- 3. User: Reward History
-- 4. System: Award & Redeem Points
-- 5. Analytics: Reward Statistics
-- ============================================================================

-- ============================================================================
-- 1. ADMIN: REWARD CONFIGURATION MANAGEMENT
-- ============================================================================

-- name: CreateRewardConfiguration :one 
INSERT INTO reward_configurations (
    config_name,
    reward_rate,
    transaction_type,
    min_transaction_amount,
    max_points_per_transaction,
    is_active,
    valid_from,
    valid_until,
    created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetRewardConfigurationByID :one
SELECT * FROM reward_configurations
WHERE id = $1;

-- name: GetActiveRewardConfiguration :one
-- Get the currently active reward configuration for a transaction type
SELECT * FROM reward_configurations
WHERE transaction_type = $1
  AND is_active = true
  AND valid_from <= NOW()
  AND (valid_until IS NULL OR valid_until >= NOW())
ORDER BY created_at DESC
LIMIT 1;

-- name: ListRewardConfigurations :many
-- List all reward configurations with pagination
SELECT * FROM reward_configurations
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListActiveRewardConfigurations :many
-- List all currently active reward configurations
SELECT * FROM reward_configurations
WHERE is_active = true
  AND valid_from <= NOW()
  AND (valid_until IS NULL OR valid_until >= NOW())
ORDER BY created_at DESC;

-- name: UpdateRewardConfiguration :one
UPDATE reward_configurations
SET config_name = COALESCE(sqlc.narg('config_name'), config_name),
    reward_rate = COALESCE(sqlc.narg('reward_rate'), reward_rate),
    transaction_type = COALESCE(sqlc.narg('transaction_type'), transaction_type),
    min_transaction_amount = COALESCE(sqlc.narg('min_transaction_amount'), min_transaction_amount),
    max_points_per_transaction = sqlc.narg('max_points_per_transaction'), -- Can be set to NULL
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    valid_from = COALESCE(sqlc.narg('valid_from'), valid_from),
    valid_until = sqlc.narg('valid_until'), -- Can be set to NULL
    updated_at = NOW()
WHERE id = $1
RETURNING *;
-- name: ActivateRewardConfiguration :one
UPDATE reward_configurations
SET is_active = true,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeactivateRewardConfiguration :one
UPDATE reward_configurations
SET is_active = false,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteRewardConfiguration :exec
DELETE FROM reward_configurations
WHERE id = $1;

-- ============================================================================
-- 2. USER: REWARD BALANCE & SUMMARY
-- ============================================================================

-- name: GetUserRewardBalance :one
-- Get user's current reward balance and totals
SELECT 
    reward_balance,
    total_reward_earned,
    total_reward_redeemed
FROM users
WHERE id = $1;

-- name: GetUserRewardSummary :one
-- Get comprehensive reward summary for a user
SELECT 
    u.id AS user_id,
    u.reward_balance AS current_balance,
    u.total_reward_earned AS total_earned,
    u.total_reward_redeemed AS total_redeemed,
    COUNT(rt.id) FILTER (WHERE rt.transaction_type = 'earned') AS total_earn_transactions,
    COUNT(rt.id) FILTER (WHERE rt.transaction_type = 'redeemed') AS total_redeem_transactions,
    u.updated_at AS last_updated
FROM users u
LEFT JOIN reward_transactions rt ON rt.user_id = u.id AND rt.status = 'completed'
WHERE u.id = $1
GROUP BY u.id;

-- name: UpdateUserRewardBalance :one
-- Manual update of user reward balance (for admin corrections)
UPDATE users
SET reward_balance = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- ============================================================================
-- 3. USER: REWARD HISTORY
-- ============================================================================

-- name: CreateRewardTransaction :one
INSERT INTO reward_transactions (
    user_id,
    transaction_id,
    transaction_type,
    source_transaction_type,
    transaction_amount,
    points_amount,
    naira_value,
    reward_config_id,
    description,
    status,
    balance_after,
    metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: GetRewardTransactionByID :one
SELECT * FROM reward_transactions
WHERE id = $1;

-- name: ListUserRewardTransactions :many
-- List all reward transactions for a user with pagination
SELECT * FROM reward_transactions
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListUserRewardTransactionsByType :many
-- List reward transactions filtered by type (earned/redeemed)
SELECT * FROM reward_transactions
WHERE user_id = $1
  AND transaction_type = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListUserRewardTransactionsByDateRange :many
-- List reward transactions within a date range
SELECT * FROM reward_transactions
WHERE user_id = $1
  AND created_at >= $2
  AND created_at <= $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

-- name: ListUserRewardTransactionsByTypeAndDateRange :many
-- List reward transactions filtered by type and date range
SELECT * FROM reward_transactions
WHERE user_id = $1
  AND transaction_type = $2
  AND created_at >= $3
  AND created_at <= $4
ORDER BY created_at DESC
LIMIT $5 OFFSET $6;

-- name: CountUserRewardTransactions :one
-- Count total reward transactions for a user
SELECT COUNT(*) FROM reward_transactions
WHERE user_id = $1
  AND status = 'completed';

-- name: CountUserRewardTransactionsByType :one
-- Count reward transactions by type
SELECT COUNT(*) FROM reward_transactions
WHERE user_id = $1
  AND transaction_type = $2
  AND status = 'completed';

-- name: GetRecentRewardActivity :many
-- Get recent reward activity for dashboard display
SELECT 
    id,
    transaction_type,
    points_amount,
    description,
    created_at
FROM reward_transactions
WHERE user_id = $1
  AND status = 'completed'
ORDER BY created_at DESC
LIMIT $2;

-- name: UpdateRewardTransactionStatus :one
-- Update transaction status (for reversals, failures, etc.)
UPDATE reward_transactions
SET status = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- ============================================================================
-- 4. SYSTEM: AWARD & REDEEM POINTS
-- ============================================================================

-- name: AwardRewardPoints :one
-- Award reward points to user and create transaction record
-- This should be called within a transaction with balance update
WITH updated_user AS (
    UPDATE users
    SET reward_balance = reward_balance + $2,
        total_reward_earned = total_reward_earned + $2,
        updated_at = NOW()
    WHERE id = $1
    RETURNING reward_balance
)
INSERT INTO reward_transactions (
    user_id,
    transaction_id,
    transaction_type,
    source_transaction_type,
    transaction_amount,
    points_amount,
    naira_value,
    reward_config_id,
    description,
    status,
    balance_after
)
SELECT 
    $1, -- user_id
    $3, -- transaction_id
    'earned',
    $4, -- source_transaction_type
    $5, -- transaction_amount
    $2, -- points_amount
    $2, -- naira_value (1:1 ratio)
    $6, -- reward_config_id
    $7, -- description
    'completed',
    updated_user.reward_balance
FROM updated_user
RETURNING *;

-- name: RedeemRewardPointsSimple :one
-- Redeem reward points (deduct from balance)
-- This should be called within a transaction
WITH updated_user AS (
    UPDATE users
    SET reward_balance = reward_balance - $2,
        total_reward_redeemed = total_reward_redeemed + $2,
        updated_at = NOW()
    WHERE id = $1
      AND reward_balance >= $2
    RETURNING reward_balance
)
INSERT INTO reward_transactions (
    user_id,
    transaction_id,
    transaction_type,
    source_transaction_type,
    transaction_amount,
    points_amount,
    naira_value,
    description,
    status,
    balance_after
)
SELECT 
    $1, -- user_id
    $3, -- transaction_id
    'redeemed',
    'bill_payment',
    $4, -- transaction_amount
    $2, -- points_amount
    $2, -- naira_value (1:1 ratio)
    $5, -- description
    'completed',
    updated_user.reward_balance
FROM updated_user
RETURNING *;

-- name: CreateRewardRedemption :one
INSERT INTO reward_redemptions (
    reward_transaction_id,
    user_id,
    bill_payment_transaction_id,
    points_redeemed,
    discount_amount,
    original_bill_amount,
    final_amount_paid,
    service_type,
    service_provider
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetRewardRedemptionByID :one
SELECT * FROM reward_redemptions
WHERE id = $1;

-- name: GetRewardRedemptionByRewardTransactionID :one
SELECT * FROM reward_redemptions
WHERE reward_transaction_id = $1;

-- name: ListUserRewardRedemptions :many
-- List all redemptions for a user
SELECT * FROM reward_redemptions
WHERE user_id = $1
ORDER BY redeemed_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRedemptionDetailsByBillTransaction :one
-- Get redemption details for a specific bill payment
SELECT * FROM reward_redemptions
WHERE bill_payment_transaction_id = $1;

-- ============================================================================
-- 5. ANALYTICS: REWARD STATISTICS (for Admin Dashboard)
-- ============================================================================

-- name: GetTotalRewardsIssued :one
-- Get total reward points issued across all users
SELECT COALESCE(SUM(total_reward_earned), 0) AS total_points_issued
FROM users
WHERE deleted_at IS NULL;

-- name: GetTotalRewardsRedeemed :one
-- Get total reward points redeemed across all users
SELECT COALESCE(SUM(total_reward_redeemed), 0) AS total_points_redeemed
FROM users
WHERE deleted_at IS NULL;

-- name: GetOutstandingRewardLiability :one
-- Get total outstanding reward points (current liability)
SELECT COALESCE(SUM(reward_balance), 0) AS outstanding_liability
FROM users
WHERE deleted_at IS NULL;

-- name: GetRewardStatisticsSummary :one
-- Get comprehensive reward statistics for admin dashboard
SELECT 
    COALESCE(SUM(reward_balance), 0)::TEXT AS outstanding_liability,
    COALESCE(SUM(total_reward_earned), 0)::TEXT AS total_points_issued,
    COALESCE(SUM(total_reward_redeemed), 0)::TEXT AS total_points_redeemed,
    COUNT(*) FILTER (WHERE reward_balance > 0) AS users_with_balance,
    COUNT(*) AS total_users
FROM users
WHERE deleted_at IS NULL;


-- name: GetTopUsersByRewardsEarned :many
-- Get top users by total rewards earned
SELECT 
    u.id,
    u.first_name,
    u.last_name,
    u.email,
    u.reward_balance,
    u.total_reward_earned,
    u.total_reward_redeemed
FROM users u
WHERE u.deleted_at IS NULL
  AND u.total_reward_earned > 0
ORDER BY u.total_reward_earned DESC
LIMIT $1;

-- name: GetRewardTransactionsByDateRange :many
-- Get all reward transactions within a date range (for analytics)
SELECT * FROM reward_transactions
WHERE created_at >= $1
  AND created_at <= $2
  AND status = 'completed'
ORDER BY created_at DESC;

-- name: GetRewardEarningsBreakdownByType :many
-- Get breakdown of reward earnings by source transaction type
SELECT 
    source_transaction_type,
    COUNT(*) AS transaction_count,
    COALESCE(SUM(points_amount), 0) AS total_points_earned,
    COALESCE(AVG(points_amount), 0) AS avg_points_per_transaction
FROM reward_transactions
WHERE transaction_type = 'earned'
  AND status = 'completed'
  AND created_at >= $1
  AND created_at <= $2
GROUP BY source_transaction_type
ORDER BY total_points_earned DESC;

-- name: GetDailyRewardStatistics :many
-- Get daily reward statistics for trending analysis
SELECT 
    DATE(created_at) AS date,
    transaction_type,
    COUNT(*) AS transaction_count,
    COALESCE(SUM(points_amount), 0) AS total_points
FROM reward_transactions
WHERE created_at >= $1
  AND created_at <= $2
  AND status = 'completed'
GROUP BY DATE(created_at), transaction_type
ORDER BY date DESC, transaction_type;

-- name: GetMonthlyRewardStatistics :many
-- Get monthly reward statistics
SELECT 
    DATE_TRUNC('month', created_at) AS month,
    transaction_type,
    COUNT(*) AS transaction_count,
    COALESCE(SUM(points_amount), 0) AS total_points
FROM reward_transactions
WHERE created_at >= $1
  AND created_at <= $2
  AND status = 'completed'
GROUP BY DATE_TRUNC('month', created_at), transaction_type
ORDER BY month DESC, transaction_type;

-- name: CheckUserRewardBalanceSufficiency :one
-- Check if user has sufficient reward balance for redemption
SELECT 
    id,
    reward_balance,
    CASE WHEN reward_balance >= $2 THEN true ELSE false END AS has_sufficient_balance
FROM users
WHERE id = $1;

-- name: GetRewardConfigurationUsageStats :many
-- Get usage statistics for each reward configuration
SELECT 
    rc.id,
    rc.config_name,
    rc.reward_rate,
    rc.transaction_type,
    COUNT(rt.id) AS times_used,
    COALESCE(SUM(rt.points_amount), 0) AS total_points_awarded,
    MIN(rt.created_at) AS first_used,
    MAX(rt.created_at) AS last_used
FROM reward_configurations rc
LEFT JOIN reward_transactions rt ON rt.reward_config_id = rc.id
WHERE rt.transaction_type = 'earned' AND rt.status = 'completed'
GROUP BY rc.id
ORDER BY total_points_awarded DESC;

-- ============================================================================
-- 6. AUDIT & VERIFICATION
-- ============================================================================

-- name: VerifyUserRewardBalance :one
-- Verify user's reward balance matches transaction history
-- Returns discrepancy if any
SELECT 
    u.id,
    u.reward_balance AS current_balance,
    COALESCE(SUM(CASE WHEN rt.transaction_type = 'earned' THEN rt.points_amount ELSE 0 END), 0) AS calculated_earned,
    COALESCE(SUM(CASE WHEN rt.transaction_type = 'redeemed' THEN rt.points_amount ELSE 0 END), 0) AS calculated_redeemed,
    (COALESCE(SUM(CASE WHEN rt.transaction_type = 'earned' THEN rt.points_amount ELSE 0 END), 0) - 
     COALESCE(SUM(CASE WHEN rt.transaction_type = 'redeemed' THEN rt.points_amount ELSE 0 END), 0)) AS calculated_balance,
    (u.reward_balance - 
     (COALESCE(SUM(CASE WHEN rt.transaction_type = 'earned' THEN rt.points_amount ELSE 0 END), 0) - 
      COALESCE(SUM(CASE WHEN rt.transaction_type = 'redeemed' THEN rt.points_amount ELSE 0 END), 0))) AS discrepancy
FROM users u
LEFT JOIN reward_transactions rt ON rt.user_id = u.id AND rt.status = 'completed'
WHERE u.id = $1
GROUP BY u.id;

-- name: GetRewardTransactionsByTransactionID :many
-- Get all reward transactions associated with a specific transaction
SELECT * FROM reward_transactions
WHERE transaction_id = $1
ORDER BY created_at DESC;

-- name: GetTotalRewardPaid :one
SELECT CAST(
    COALESCE(SUM(u.total_reward_redeemed), 0)
  + COALESCE(SUM(re.withdrawn_balance), 0) AS INTEGER
) AS total_reward_paid
FROM users u
LEFT JOIN referral_earnings re
  ON re.user_id = u.id;

-- name: GetTotalRewardEarned :one
SELECT CAST(
    COALESCE(SUM(u.total_reward_earned), 0)
  + COALESCE(SUM(rt.points_amount), 0) AS INTEGER
) AS total_reward_earned
FROM users u
LEFT JOIN reward_transactions rt
  ON rt.user_id = u.id;
