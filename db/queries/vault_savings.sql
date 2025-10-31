-- ============================================================================
-- VAULT SAVINGS QUERIES
-- ============================================================================

-- name: CreateVaultGoal :one
INSERT INTO vault_savings (
    user_id,
    vault_name,
    description,
    goal_amount,
    current_balance,
    currency,
    auto_save_enabled,
    auto_save_frequency,
    auto_save_amount,
    next_auto_save,
    recurring_rule,
    status,
    vault_type
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: GetVaultGoalByID :one
SELECT * FROM vault_savings
WHERE id = $1 AND status != 'cancelled';

-- name: GetVaultGoalsByUserID :many
SELECT * FROM vault_savings
WHERE user_id = $1 AND status != 'cancelled'
ORDER BY created_at DESC;

-- name: GetActiveVaultGoalsByUserID :many
SELECT * FROM vault_savings
WHERE user_id = $1 AND status = 'active'
ORDER BY created_at DESC;

-- name: GetVaultGoalsByUserIDAndCurrency :many
SELECT * FROM vault_savings
WHERE user_id = $1 AND currency = $2 AND status != 'cancelled'
ORDER BY created_at DESC;

-- name: UpdateVaultBalance :exec
UPDATE vault_savings
SET current_balance = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: UpdateVaultGoalDetails :exec
UPDATE vault_savings
SET vault_name = COALESCE(sqlc.narg('vault_name'), vault_name),
    description = COALESCE(sqlc.narg('description'), description),
    goal_amount = COALESCE(sqlc.narg('goal_amount'), goal_amount),
    updated_at = NOW()
WHERE id = $1;

-- name: UpdateVaultStatus :exec
UPDATE vault_savings
SET status = $2,
    updated_at = NOW(),
    completed_at = CASE 
        WHEN $2 = 'completed' THEN NOW()
        ELSE completed_at
    END
WHERE id = $1;

-- name: UpdateRecurringRule :exec
UPDATE vault_savings
SET recurring_rule = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: EnableAutoSave :exec
UPDATE vault_savings
SET auto_save_enabled = TRUE,
    auto_save_frequency = $2,
    auto_save_amount = $3,
    next_auto_save = $4,
    updated_at = NOW()
WHERE id = $1;

-- name: DisableAutoSave :exec
UPDATE vault_savings
SET auto_save_enabled = FALSE,
    next_auto_save = NULL,
    updated_at = NOW()
WHERE id = $1;

-- name: UpdateYieldTracking :exec
UPDATE vault_savings
SET total_yield_earned = $2,
    last_yield_calculation = $3,
    next_yield_calculation = $4,
    updated_at = NOW()
WHERE id = $1;

-- name: IncrementVaultBalance :exec
UPDATE vault_savings
SET current_balance = current_balance + $2,
    updated_at = NOW()
WHERE id = $1;

-- name: DecrementVaultBalance :exec
UPDATE vault_savings
SET current_balance = current_balance - $2,
    updated_at = NOW()
WHERE id = $1 AND current_balance >= $2;

-- name: GetVaultsDueForAutoSave :many
SELECT * FROM vault_savings
WHERE auto_save_enabled = TRUE
  AND status = 'active'
  AND next_auto_save <= NOW()
ORDER BY next_auto_save ASC;

-- name: GetVaultsDueForYieldCalculation :many
SELECT * FROM vault_savings
WHERE status = 'active'
  AND current_balance > 0
  AND (next_yield_calculation IS NULL OR next_yield_calculation <= NOW())
ORDER BY next_yield_calculation ASC NULLS FIRST
LIMIT $1;

-- name: GetVaultsWithRecurringRules :many
SELECT * FROM vault_savings
WHERE recurring_rule IS NOT NULL
  AND status = 'active'
  AND (recurring_rule->>'enabled')::boolean = true
  AND (recurring_rule->>'next_execution_at')::timestamptz <= NOW()
ORDER BY (recurring_rule->>'next_execution_at')::timestamptz ASC
LIMIT $1;

-- name: GetVaultGoalProgress :one
SELECT 
    id,
    vault_name,
    current_balance,
    goal_amount,
    CASE 
        WHEN goal_amount > 0 THEN (current_balance / goal_amount * 100)
        ELSE 0
    END as progress_percentage,
    CASE 
        WHEN current_balance >= goal_amount THEN true
        ELSE false
    END as goal_reached
FROM vault_savings
WHERE id = $1;

-- name: GetUserVaultsSummary :one
SELECT 
    COUNT(*) as total_vaults,
    COUNT(*) FILTER (WHERE status = 'active') as active_vaults,
    COUNT(*) FILTER (WHERE status = 'completed') as completed_vaults,
    COALESCE(SUM(current_balance) FILTER (WHERE currency = 'USDT'), 0) as total_usdt,
    COALESCE(SUM(current_balance) FILTER (WHERE currency = 'USDC'), 0) as total_usdc,
    COALESCE(SUM(current_balance) FILTER (WHERE currency = 'NGN'), 0) as total_ngn,
    COALESCE(SUM(current_balance) FILTER (WHERE currency = 'USD'), 0) as total_usd,
    COALESCE(SUM(total_yield_earned), 0) as total_yield_earned
FROM vault_savings
WHERE user_id = $1 AND status != 'cancelled';

-- name: DeleteVaultGoal :exec
UPDATE vault_savings
SET status = 'cancelled',
    updated_at = NOW()
WHERE id = $1;

-- ============================================================================
-- VAULT TRANSACTIONS QUERIES
-- ============================================================================

-- name: CreateVaultTransaction :one
INSERT INTO vault_transactions (
    user_id,
    vault_id,
    transaction_type,
    amount,
    currency,
    source_wallet,
    destination_wallet,
    balance_before,
    balance_after,
    reference,
    description,
    metadata,
    status,
    requires_2fa,
    requires_admin_approval
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
) RETURNING *;

-- name: GetVaultTransactionByID :one
SELECT * FROM vault_transactions
WHERE id = $1;

-- name: GetVaultTransactionByReference :one
SELECT * FROM vault_transactions
WHERE reference = $1;

-- name: GetVaultTransactionsByVaultID :many
SELECT * FROM vault_transactions
WHERE vault_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetVaultTransactionsByUserID :many
SELECT * FROM vault_transactions
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetVaultTransactionsByType :many
SELECT * FROM vault_transactions
WHERE vault_id = $1 AND transaction_type = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: UpdateVaultTransactionStatus :exec
UPDATE vault_transactions
SET status = $2,
    completed_at = CASE 
        WHEN $2 = 'completed' THEN NOW()
        ELSE completed_at
    END
WHERE id = $1;

-- name: MarkTransaction2FAVerified :exec
UPDATE vault_transactions
SET two_fa_verified_at = NOW()
WHERE id = $1 AND requires_2fa = TRUE;

-- name: ApproveTransactionByAdmin :exec
UPDATE vault_transactions
SET admin_approved_by = $2,
    admin_approved_at = NOW(),
    approval_notes = $3,
    status = 'completed',
    completed_at = NOW()
WHERE id = $1 AND requires_admin_approval = TRUE;

-- name: GetPendingVaultTransactions :many
SELECT * FROM vault_transactions
WHERE status = 'pending'
ORDER BY created_at ASC;

-- name: GetTransactionsRequiringAdminApproval :many
SELECT vt.*, vs.vault_name, vs.currency
FROM vault_transactions vt
JOIN vault_savings vs ON vt.vault_id = vs.id
WHERE vt.requires_admin_approval = TRUE
  AND vt.admin_approved_at IS NULL
  AND vt.status = 'pending'
ORDER BY vt.created_at ASC;

-- name: GetTransactionsRequiring2FA :many
SELECT * FROM vault_transactions
WHERE requires_2fa = TRUE
  AND two_fa_verified_at IS NULL
  AND status = 'pending'
  AND user_id = $1
ORDER BY created_at DESC;

-- name: GetVaultTransactionStats :one
SELECT 
    COUNT(*) as total_transactions,
    COUNT(*) FILTER (WHERE transaction_type = 'deposit') as total_deposits,
    COUNT(*) FILTER (WHERE transaction_type = 'withdrawal') as total_withdrawals,
    COUNT(*) FILTER (WHERE transaction_type = 'auto_save') as total_auto_saves,
    COUNT(*) FILTER (WHERE transaction_type = 'yield_credit') as total_yield_credits,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'deposit' AND status = 'completed'), 0) as total_deposited,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'withdrawal' AND status = 'completed'), 0) as total_withdrawn,
    COALESCE(SUM(amount) FILTER (WHERE transaction_type = 'yield_credit' AND status = 'completed'), 0) as total_yield
FROM vault_transactions
WHERE vault_id = $1;

-- name: GetRecentVaultActivity :many
SELECT 
    vt.id,
    vt.transaction_type,
    vt.amount,
    vt.currency,
    vt.status,
    vt.created_at,
    vs.vault_name
FROM vault_transactions vt
JOIN vault_savings vs ON vt.vault_id = vs.id
WHERE vt.user_id = $1
ORDER BY vt.created_at DESC
LIMIT $2;

-- name: GetVaultTransactionsByDateRange :many
SELECT * FROM vault_transactions
WHERE vault_id = $1
  AND created_at BETWEEN $2 AND $3
ORDER BY created_at DESC;

-- ============================================================================
-- VAULT YIELDS QUERIES
-- ============================================================================

-- name: CreateVaultYield :one
INSERT INTO vault_yields (
    user_id,
    vault_id,
    yield_amount,
    yield_rate,
    calculation_period_start,
    calculation_period_end,
    vault_balance_snapshot,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: GetVaultYieldByID :one
SELECT * FROM vault_yields
WHERE id = $1;

-- name: GetVaultYieldsByVaultID :many
SELECT * FROM vault_yields
WHERE vault_id = $1
ORDER BY calculation_period_end DESC
LIMIT $2 OFFSET $3;

-- name: GetVaultYieldsByUserID :many
SELECT vy.*, vs.vault_name
FROM vault_yields vy
JOIN vault_savings vs ON vy.vault_id = vs.id
WHERE vy.user_id = $1
ORDER BY vy.created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateYieldStatus :exec
UPDATE vault_yields
SET status = $2,
    credited_at = CASE 
        WHEN $2 = 'credited' THEN NOW()
        ELSE credited_at
    END
WHERE id = $1;

-- name: GetTotalYieldEarned :one
SELECT COALESCE(SUM(yield_amount), 0) as total_yield
FROM vault_yields
WHERE vault_id = $1 AND status = 'credited';

-- name: GetYieldsByPeriod :many
SELECT * FROM vault_yields
WHERE vault_id = $1
  AND calculation_period_start >= $2
  AND calculation_period_end <= $3
ORDER BY calculation_period_start ASC;

-- name: GetPendingYields :many
SELECT * FROM vault_yields
WHERE status = 'calculated'
ORDER BY created_at ASC
LIMIT $1;

-- name: GetVaultYieldSummary :one
SELECT 
    COUNT(*) as total_yield_periods,
    COALESCE(SUM(yield_amount), 0) as total_yield_earned,
    COALESCE(AVG(yield_rate), 0) as average_yield_rate,
    MAX(calculation_period_end) as last_yield_date
FROM vault_yields
WHERE vault_id = $1 AND status = 'credited';

-- ============================================================================
-- VAULT YIELD CONFIGS QUERIES
-- ============================================================================

-- name: CreateYieldConfig :one
INSERT INTO vault_yield_configs (
    currency,
    apy_rate,
    min_balance_for_yield,
    compound_frequency,
    is_active,
    effective_from,
    effective_until,
    notes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: GetYieldConfigByID :one
SELECT * FROM vault_yield_configs
WHERE id = $1;

-- name: GetActiveYieldConfigByCurrency :one
SELECT * FROM vault_yield_configs
WHERE currency = $1
  AND is_active = TRUE
  AND effective_from <= NOW()
  AND (effective_until IS NULL OR effective_until > NOW())
ORDER BY effective_from DESC
LIMIT 1;

-- name: GetAllActiveYieldConfigs :many
SELECT * FROM vault_yield_configs
WHERE is_active = TRUE
  AND effective_from <= NOW()
  AND (effective_until IS NULL OR effective_until > NOW())
ORDER BY currency, effective_from DESC;

-- name: GetYieldConfigsByCurrency :many
SELECT * FROM vault_yield_configs
WHERE currency = $1
ORDER BY effective_from DESC;

-- name: UpdateYieldConfig :exec
UPDATE vault_yield_configs
SET apy_rate = COALESCE(sqlc.narg('apy_rate'), apy_rate),
    min_balance_for_yield = COALESCE(sqlc.narg('min_balance_for_yield'), min_balance_for_yield),
    compound_frequency = COALESCE(sqlc.narg('compound_frequency'), compound_frequency),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    effective_until = COALESCE(sqlc.narg('effective_until'), effective_until),
    notes = COALESCE(sqlc.narg('notes'), notes),
    updated_at = NOW()
WHERE id = $1;

-- name: DeactivateYieldConfig :exec
UPDATE vault_yield_configs
SET is_active = FALSE,
    effective_until = NOW(),
    updated_at = NOW()
WHERE id = $1;

-- name: GetYieldConfigHistory :many
SELECT * FROM vault_yield_configs
WHERE currency = $1
ORDER BY effective_from DESC
LIMIT $2 OFFSET $3;

-- ============================================================================
-- COMPLEX ANALYTICAL QUERIES
-- ============================================================================

-- name: GetVaultsDashboardMetrics :one
SELECT 
    COUNT(DISTINCT vs.id) as total_active_vaults,
    COUNT(DISTINCT vs.user_id) as unique_users,
    COALESCE(SUM(vs.current_balance) FILTER (WHERE vs.currency = 'USDT'), 0) as total_usdt_locked,
    COALESCE(SUM(vs.current_balance) FILTER (WHERE vs.currency = 'USDC'), 0) as total_usdc_locked,
    COALESCE(SUM(vs.current_balance) FILTER (WHERE vs.currency = 'NGN'), 0) as total_ngn_locked,
    COALESCE(SUM(vs.current_balance) FILTER (WHERE vs.currency = 'USD'), 0) as total_usd_locked,
    COALESCE(SUM(vt.amount) FILTER (WHERE vt.transaction_type = 'deposit' AND vt.created_at >= NOW() - INTERVAL '30 days'), 0) as deposits_last_30_days,
    COALESCE(SUM(vt.amount) FILTER (WHERE vt.transaction_type = 'withdrawal' AND vt.created_at >= NOW() - INTERVAL '30 days'), 0) as withdrawals_last_30_days,
    COUNT(DISTINCT vs.user_id) FILTER (WHERE vs.created_at >= NOW() - INTERVAL '30 days') as new_users_last_30_days,
    COALESCE(AVG(vs.current_balance), 0) as average_vault_balance
FROM vault_savings vs
LEFT JOIN vault_transactions vt ON vs.id = vt.vault_id
WHERE vs.status = 'active';

-- name: GetUserRetentionMetrics :many
SELECT 
    DATE_TRUNC('day', vs.created_at) as cohort_date,
    COUNT(DISTINCT vs.user_id) as users_created,
    COUNT(DISTINCT CASE 
        WHEN vt.created_at >= vs.created_at + INTERVAL '30 days' 
        THEN vs.user_id 
    END) as users_active_after_30_days,
    CASE 
        WHEN COUNT(DISTINCT vs.user_id) > 0 
        THEN (COUNT(DISTINCT CASE 
            WHEN vt.created_at >= vs.created_at + INTERVAL '30 days' 
            THEN vs.user_id 
        END)::float / COUNT(DISTINCT vs.user_id)::float * 100)
        ELSE 0
    END as retention_rate_30_days
FROM vault_savings vs
LEFT JOIN vault_transactions vt ON vs.user_id = vt.user_id
WHERE vs.created_at >= $1 AND vs.created_at <= $2
GROUP BY DATE_TRUNC('day', vs.created_at)
ORDER BY cohort_date DESC;

-- name: GetVaultCompletionStats :many
SELECT 
    currency,
    COUNT(*) as total_completed,
    COALESCE(AVG(EXTRACT(EPOCH FROM (completed_at - created_at)) / 86400), 0) as avg_days_to_complete,
    COALESCE(AVG(goal_amount), 0) as avg_goal_amount
FROM vault_savings
WHERE status = 'completed'
  AND completed_at IS NOT NULL
  AND created_at >= $1
GROUP BY currency
ORDER BY total_completed DESC;

-- name: GetTopSavers :many
SELECT 
    vs.user_id,
    COUNT(vs.id) as total_vaults,
    COALESCE(SUM(vs.current_balance), 0) as total_saved,
    COALESCE(SUM(vs.total_yield_earned), 0) as total_yield_earned,
    COUNT(*) FILTER (WHERE vs.status = 'completed') as completed_goals
FROM vault_savings vs
WHERE vs.status IN ('active', 'completed')
GROUP BY vs.user_id
ORDER BY total_saved DESC
LIMIT $1;

-- name: GetRecurringDepositMetrics :one
SELECT 
    COUNT(*) FILTER (WHERE auto_save_enabled = TRUE OR recurring_rule IS NOT NULL) as vaults_with_auto_save,
    COUNT(*) FILTER (WHERE (recurring_rule->>'interval')::text = 'daily') as daily_auto_saves,
    COUNT(*) FILTER (WHERE (recurring_rule->>'interval')::text = 'weekly') as weekly_auto_saves,
    COUNT(*) FILTER (WHERE (recurring_rule->>'interval')::text = 'monthly') as monthly_auto_saves,
    COALESCE(SUM((recurring_rule->>'amount')::decimal), 0) as total_scheduled_amount
FROM vault_savings
WHERE status = 'active'
  AND (auto_save_enabled = TRUE OR (recurring_rule->>'enabled')::boolean = TRUE);

-- name: GetVaultHealthCheck :many
SELECT 
    vs.id,
    vs.vault_name,
    vs.user_id,
    vs.current_balance,
    vs.goal_amount,
    vs.status,
    CASE 
        WHEN vs.current_balance >= vs.goal_amount THEN 'goal_reached'
        WHEN vs.current_balance = 0 AND vs.created_at < NOW() - INTERVAL '30 days' THEN 'inactive'
        WHEN vs.auto_save_enabled = TRUE AND vs.next_auto_save < NOW() - INTERVAL '7 days' THEN 'auto_save_failed'
        ELSE 'healthy'
    END as health_status
FROM vault_savings vs
WHERE vs.status = 'active'
ORDER BY 
    CASE 
        WHEN vs.current_balance >= vs.goal_amount THEN 1
        WHEN vs.current_balance = 0 AND vs.created_at < NOW() - INTERVAL '30 days' THEN 2
        WHEN vs.auto_save_enabled = TRUE AND vs.next_auto_save < NOW() - INTERVAL '7 days' THEN 3
        ELSE 4
    END;

-- ============================================================================
-- TRANSACTION MANAGEMENT QUERIES (FOR USE WITH SQLC TRANSACTIONS)
-- ============================================================================

-- name: ProcessVaultDeposit :exec
-- This should be called within a transaction
UPDATE vault_savings
SET current_balance = current_balance + $2,
    updated_at = NOW()
WHERE id = $1;

-- name: ProcessVaultWithdrawal :exec
-- This should be called within a transaction
-- Will fail if insufficient balance
UPDATE vault_savings
SET current_balance = current_balance - $2,
    updated_at = NOW()
WHERE id = $1 
  AND current_balance >= $2;

-- name: CheckVaultBalance :one
SELECT current_balance, currency
FROM vault_savings
WHERE id = $1
FOR UPDATE; -- Lock row for transaction

-- name: LockVaultForUpdate :one
SELECT * FROM vault_savings
WHERE id = $1
FOR UPDATE;

-- ============================================================================
-- BATCH OPERATIONS
-- ============================================================================

-- name: BatchUpdateVaultBalances :exec
UPDATE vault_savings
SET current_balance = v.new_balance,
    updated_at = NOW()
FROM (SELECT unnest($1::uuid[]) as vault_id, unnest($2::decimal[]) as new_balance) as v
WHERE vault_savings.id = v.vault_id;

-- -- name: BatchCreateTransactions :copyfrom
-- INSERT INTO vault_transactions (
--     user_id,
--     vault_id,
--     transaction_type,
--     amount,
--     currency,
--     balance_before,
--     balance_after,
--     reference,
--     description,
--     status
-- ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- ============================================================================
-- SEARCH AND FILTER QUERIES
-- ============================================================================

-- name: SearchVaultsByName :many
SELECT * FROM vault_savings
WHERE user_id = $1 
  AND vault_name ILIKE '%' || $2 || '%'
  AND status != 'cancelled'
ORDER BY created_at DESC;

-- name: FilterVaultsByStatus :many
SELECT * FROM vault_savings
WHERE user_id = $1 
  AND status = ANY($2::text[])
ORDER BY created_at DESC;

-- name: GetVaultsWithLowBalance :many
SELECT * FROM vault_savings
WHERE status = 'active'
  AND current_balance > 0
  AND current_balance < goal_amount * 0.1  -- Less than 10% of goal
ORDER BY (current_balance / NULLIF(goal_amount, 0)) ASC
LIMIT $1;

-- name: GetVaultsWithDueRecurringDeposits :many
-- Get all vaults with recurring deposits that are due for execution
SELECT *
FROM vault_savings
WHERE recurring_rule IS NOT NULL
  AND recurring_rule->>'enabled' = 'true'
  AND (recurring_rule->>'next_execution_at')::timestamptz <= $1::timestamptz
  AND status = 'active'
ORDER BY (recurring_rule->>'next_execution_at')::timestamptz ASC;

-- name: GetVaultsWithActiveRecurringRules :many
-- Get all vaults that have active recurring rules (for stats)
SELECT *
FROM vault_savings
WHERE recurring_rule IS NOT NULL
  AND recurring_rule->>'enabled' = 'true'
  AND status = 'active';

-- name: UpdateVaultRecurringRule :exec
-- Update the recurring rule for a vault
UPDATE vault_savings
SET recurring_rule = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;