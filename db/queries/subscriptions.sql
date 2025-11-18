-- name: CreateVirtualCard :one
INSERT INTO virtual_cards (
    user_id,
    flutterwave_card_id,
    card_pan_last4,
    card_brand,
    card_type,
    balance,
    currency,
    name_on_card,
    expiry_month,
    expiry_year,
    status,
    billing_address,
    metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: GetVirtualCard :one
SELECT * FROM virtual_cards
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetVirtualCardByFlutterwaveId :one
SELECT * FROM virtual_cards
WHERE flutterwave_card_id = $1 AND deleted_at IS NULL;

-- name: ListUserVirtualCards :many
SELECT * FROM virtual_cards
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateVirtualCardBalance :one
UPDATE virtual_cards
SET balance = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: FreezeVirtualCard :one
UPDATE virtual_cards
SET is_frozen = true, 
    freeze_reason = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UnfreezeVirtualCard :one
UPDATE virtual_cards
SET is_frozen = false, 
    freeze_reason = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: SoftDeleteVirtualCard :one
UPDATE virtual_cards
SET deleted_at = CURRENT_TIMESTAMP,
    status = 'closed',
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- =====================================================
-- Subscription Categories
-- subscription categories are the categories of subscriptions. For example, "Streaming", "Gaming", "Food", "Shopping", etc.
-- =====================================================

-- name: CreateSubscriptionCategory :one
INSERT INTO subscription_categories (name, description, icon_url, display_order)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListSubscriptionCategories :many
SELECT * FROM subscription_categories
ORDER BY display_order, name;

-- =====================================================
-- Subscription Merchants
-- subscription merchants are the merchants of subscriptions. For example, "Netflix", "Amazon", "Apple", "Google", etc.
-- =====================================================

-- name: CreateSubscriptionMerchant :one
INSERT INTO subscription_merchants (
    merchant_name,
    merchant_identifier,
    category_id,
    logo_url,
    description,
    website_url,
    default_renewal_days,
    common_amounts,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetSubscriptionMerchant :one
SELECT * FROM subscription_merchants
WHERE id = $1;

-- name: FindSubscriptionMerchantByIdentifier :one
SELECT * FROM subscription_merchants
WHERE merchant_identifier = $1 AND is_active = true
LIMIT 1;

-- name: SearchSubscriptionMerchants :many
SELECT sm.*, sc.name as category_name
FROM subscription_merchants sm
LEFT JOIN subscription_categories sc ON sm.category_id = sc.id
WHERE sm.merchant_name ILIKE '%' || $1 || '%'
   OR sm.merchant_identifier ILIKE '%' || $1 || '%'
ORDER BY sm.merchant_name;

-- name: ListSubscriptionMerchantsByCategory :many
SELECT sm.*, sc.name as category_name
FROM subscription_merchants sm
JOIN subscription_categories sc ON sm.category_id = sc.id
WHERE sm.category_id = $1 AND sm.is_active = true
ORDER BY sm.merchant_name;

-- =====================================================
-- Subscriptions - Core Operations
-- =====================================================

-- name: CreateSubscription :one
INSERT INTO subscriptions (
    user_id,
    card_id,
    merchant_id,
    merchant_name,
    amount,
    currency,
    first_transaction_date,
    last_transaction_date,
    next_estimated_renewal_date,
    renewal_interval_days,
    status,
    confidence_score,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: GetSubscription :one
SELECT s.*, 
       sm.merchant_name as merchant_official_name,
       sm.logo_url as merchant_logo,
       sc.name as category_name,
       vc.card_pan_last4
FROM subscriptions s
LEFT JOIN subscription_merchants sm ON s.merchant_id = sm.id
LEFT JOIN subscription_categories sc ON sm.category_id = sc.id
LEFT JOIN virtual_cards vc ON s.card_id = vc.id
WHERE s.id = $1;

-- name: ListUserSubscriptions :many
SELECT s.*,
       sm.merchant_name as merchant_official_name,
       sm.logo_url as merchant_logo,
       sc.name as category_name,
       vc.card_pan_last4
FROM subscriptions s
LEFT JOIN subscription_merchants sm ON s.merchant_id = sm.id
LEFT JOIN subscription_categories sc ON sm.category_id = sc.id
LEFT JOIN virtual_cards vc ON s.card_id = vc.id
WHERE s.user_id = $1
  AND s.status = ANY($2::TEXT[])
ORDER BY s.next_estimated_renewal_date ASC;

-- name: FindExistingSubscription :one
SELECT * FROM subscriptions
WHERE user_id = $1
  AND card_id = $2
  AND (merchant_id = $3 OR merchant_name = $4)
  AND status IN ('active', 'pending')
LIMIT 1;

-- name: UpdateSubscription :one
UPDATE subscriptions
SET last_transaction_date = COALESCE($2, last_transaction_date),
    next_estimated_renewal_date = COALESCE($3, next_estimated_renewal_date),
    renewal_interval_days = COALESCE($4, renewal_interval_days),
    amount = COALESCE($5, amount),
    total_spend = total_spend + COALESCE($6, 0),
    transaction_count = transaction_count + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: UpdateSubscriptionStatus :one
UPDATE subscriptions
SET status = $2,
    cancellation_date = CASE WHEN $2 = 'cancelled' THEN CURRENT_TIMESTAMP ELSE cancellation_date END,
    cancellation_reason = CASE WHEN $2 = 'cancelled' THEN $3 ELSE cancellation_reason END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: IncrementFailedPaymentCount :one
UPDATE subscriptions
SET failed_payment_count = failed_payment_count + 1,
    last_failed_payment_date = CURRENT_TIMESTAMP,
    status = CASE 
        WHEN failed_payment_count + 1 >= 3 THEN 'suspended'
        ELSE status
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: GetSubscriptionsNearingRenewal :many
SELECT s.*,
       sm.merchant_name as merchant_official_name,
       sm.logo_url as merchant_logo,
       vc.balance as card_balance,
       u.email as user_email
FROM subscriptions s
LEFT JOIN subscription_merchants sm ON s.merchant_id = sm.id
LEFT JOIN virtual_cards vc ON s.card_id = vc.id
LEFT JOIN users u ON s.user_id = u.id
WHERE s.status = 'active'
  AND s.next_estimated_renewal_date >= CURRENT_TIMESTAMP
  AND s.next_estimated_renewal_date <= CURRENT_TIMESTAMP + INTERVAL '1 day' * $1
ORDER BY s.next_estimated_renewal_date ASC;

-- name: GetSubscriptionsRequiringTopup :many
WITH topup_check AS (
    SELECT 
        s.id as subscription_id,
        s.user_id,
        s.card_id,
        s.amount as required_amount,
        vc.balance as card_balance,
        ats.buffer_fixed_amount,
        ats.buffer_percentage,
        (s.amount + COALESCE(ats.buffer_fixed_amount, 0) + 
         (s.amount * COALESCE(ats.buffer_percentage, 0) / 100)) as total_required
    FROM subscriptions s
    JOIN virtual_cards vc ON s.card_id = vc.id
    JOIN auto_topup_settings ats ON s.user_id = ats.user_id
    WHERE s.status = 'active'
      AND ats.enabled = true
      AND s.next_estimated_renewal_date >= CURRENT_TIMESTAMP + INTERVAL '1 hour' * $1
      AND s.next_estimated_renewal_date <= CURRENT_TIMESTAMP + INTERVAL '1 hour' * ($1 + 1)
)
SELECT 
    tc.*,
    w.balance as wallet_balance,
    (tc.total_required - tc.card_balance) as topup_amount_needed
FROM topup_check tc
JOIN swift_wallets w ON tc.user_id = w.user_id AND w.currency = 'USD'
WHERE tc.card_balance < tc.total_required
  AND w.balance >= (tc.total_required - tc.card_balance);


-- =====================================================
-- Subscription Transactions
-- =====================================================

-- name: CreateSubscriptionTransaction :one
INSERT INTO subscription_transactions (
    subscription_id,
    transaction_id,
    flutterwave_transaction_id,
    amount,
    currency,
    transaction_date,
    status,
    failure_reason,
    merchant_descriptor,
    is_renewal,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetSubscriptionTransactionHistory :many
SELECT st.*,
       t.type as transaction_type,
       t.status as ledger_status
FROM subscription_transactions st
LEFT JOIN transactions t ON st.transaction_id = t.id
WHERE st.subscription_id = $1
ORDER BY st.transaction_date DESC
LIMIT $2 OFFSET $3;

-- name: GetRecentFailedTransactions :many
SELECT st.*,
       s.merchant_name,
       s.user_id,
       u.email as user_email
FROM subscription_transactions st
JOIN subscriptions s ON st.subscription_id = s.id
JOIN users u ON s.user_id = u.id
WHERE st.status = 'failed'
  AND st.transaction_date >= CURRENT_TIMESTAMP - INTERVAL '1 day' * $1
ORDER BY st.transaction_date DESC;


-- =====================================================
-- Auto Top-up
-- =====================================================

-- name: GetUserAutoTopupSettings :one
SELECT * FROM auto_topup_settings
WHERE user_id = $1;

-- name: UpsertAutoTopupSettings :one
INSERT INTO auto_topup_settings (
    user_id,
    enabled,
    default_card_id,
    topup_strategy,
    fixed_amount,
    buffer_percentage,
    buffer_fixed_amount,
    min_wallet_balance_required,
    check_time_hours_before,
    max_topup_per_day
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (user_id) 
DO UPDATE SET
    enabled = EXCLUDED.enabled,
    default_card_id = EXCLUDED.default_card_id,
    topup_strategy = EXCLUDED.topup_strategy,
    fixed_amount = EXCLUDED.fixed_amount,
    buffer_percentage = EXCLUDED.buffer_percentage,
    buffer_fixed_amount = EXCLUDED.buffer_fixed_amount,
    min_wallet_balance_required = EXCLUDED.min_wallet_balance_required,
    check_time_hours_before = EXCLUDED.check_time_hours_before,
    max_topup_per_day = EXCLUDED.max_topup_per_day,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: CreateAutoTopupLog :one
INSERT INTO auto_topup_logs (
    user_id,
    card_id,
    subscription_id,
    topup_amount,
    wallet_balance_before,
    wallet_balance_after,
    card_balance_before,
    card_balance_after,
    status,
    failure_reason,
    transaction_id,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetUserTopupLogs :many
SELECT atl.*,
       vc.card_pan_last4,
       s.merchant_name
FROM auto_topup_logs atl
JOIN virtual_cards vc ON atl.card_id = vc.id
LEFT JOIN subscriptions s ON atl.subscription_id = s.id
WHERE atl.user_id = $1
ORDER BY atl.created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateDailyTopupCounter :one
UPDATE auto_topup_settings
SET daily_topup_count = CASE 
    WHEN last_topup_date = CURRENT_DATE THEN daily_topup_count + 1
    ELSE 1
END,
last_topup_date = CURRENT_DATE,
updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
RETURNING *;

-- =====================================================
-- Card Funding History
-- =====================================================

-- name: CreateCardFundingHistory :one
INSERT INTO card_funding_history (
    user_id,
    card_id,
    wallet_id,
    amount,
    currency,
    funding_type,
    transaction_id,
    ledger_entry_id,
    status,
    failure_reason,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetCardFundingHistory :many
SELECT cfh.*,
       vc.card_pan_last4,
       t.type as transaction_type
FROM card_funding_history cfh
JOIN virtual_cards vc ON cfh.card_id = vc.id
LEFT JOIN transactions t ON cfh.transaction_id = t.id
WHERE cfh.card_id = $1
ORDER BY cfh.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetUserFundingHistory :many
SELECT cfh.*,
       vc.card_pan_last4
FROM card_funding_history cfh
JOIN virtual_cards vc ON cfh.card_id = vc.id
WHERE cfh.user_id = $1
  AND cfh.created_at >= $2
  AND cfh.created_at <= $3
ORDER BY cfh.created_at DESC;


-- =====================================================
-- Notifications
-- =====================================================

-- name: CreateSubscriptionNotification :one
INSERT INTO subscription_notifications (
    user_id,
    subscription_id,
    notification_type,
    title,
    message,
    action_url,
    priority,
    delivery_channel,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: MarkNotificationAsSent :one
UPDATE subscription_notifications
SET sent_at = CURRENT_TIMESTAMP,
    delivery_status = 'sent'
WHERE id = $1
RETURNING *;

-- name: MarkVNotificationAsRead :one
UPDATE subscription_notifications
SET read_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: GetUserUnreadNotifications :many
SELECT sn.*,
       s.merchant_name,
       sm.logo_url as merchant_logo
FROM subscription_notifications sn
LEFT JOIN subscriptions s ON sn.subscription_id = s.id
LEFT JOIN subscription_merchants sm ON s.merchant_id = sm.id
WHERE sn.user_id = $1
  AND sn.read_at IS NULL
ORDER BY sn.created_at DESC
LIMIT $2;

-- name: GetUserNotifications :many
SELECT sn.*,
       s.merchant_name,
       sm.logo_url as merchant_logo
FROM subscription_notifications sn
LEFT JOIN subscriptions s ON sn.subscription_id = s.id
LEFT JOIN subscription_merchants sm ON s.merchant_id = sm.id
WHERE sn.user_id = $1
ORDER BY sn.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetPendingNotifications :many
SELECT * FROM subscription_notifications
WHERE delivery_status = 'pending'
  AND created_at <= CURRENT_TIMESTAMP
ORDER BY priority DESC, created_at ASC
LIMIT $1;


-- =====================================================
-- Analytics & Reporting
-- =====================================================

-- name: GetUserSubscriptionSummary :one
WITH subscription_stats AS (
    SELECT
        COUNT(*) FILTER (WHERE status = 'active') as active_count,
        COUNT(*) FILTER (WHERE status = 'cancelled') as cancelled_count,
        COUNT(*) FILTER (WHERE status = 'suspended') as suspended_count,
        SUM(amount) FILTER (WHERE status = 'active') as total_monthly_spend,
        AVG(amount) FILTER (WHERE status = 'active') as avg_subscription_cost
    FROM subscriptions s
    WHERE s.user_id = $1
),
transaction_stats AS (
    SELECT
        COUNT(*) FILTER (WHERE st.status = 'success') as successful_payments,
        COUNT(*) FILTER (WHERE st.status = 'failed') as failed_payments,
        SUM(st.amount) FILTER (WHERE st.status = 'success') as total_spent_all_time
    FROM subscription_transactions st
    JOIN subscriptions s ON st.subscription_id = s.id
    WHERE s.user_id = $1
),
upcoming_renewals AS (
    SELECT
        COUNT(*) as renewals_next_7_days,
        SUM(amount) as amount_next_7_days
    FROM subscriptions
    WHERE s.user_id = $1
      AND s.status = 'active'
      AND s.next_estimated_renewal_date >= CURRENT_TIMESTAMP
      AND s.next_estimated_renewal_date <= CURRENT_TIMESTAMP + INTERVAL '7 days'
)
SELECT 
    ss.*,
    ts.*,
    ur.renewals_next_7_days,
    ur.amount_next_7_days
FROM subscription_stats ss
CROSS JOIN transaction_stats ts
CROSS JOIN upcoming_renewals ur;

-- name: GetSubscriptionSpendByCategory :many
SELECT 
    sc.name as category_name,
    sc.icon_url as category_icon,
    COUNT(DISTINCT s.id) as subscription_count,
    SUM(s.amount) as total_monthly_spend,
    AVG(s.amount) as avg_subscription_cost,
    SUM(s.total_spend) as lifetime_spend
FROM subscriptions s
JOIN subscription_merchants sm ON s.merchant_id = sm.id
JOIN subscription_categories sc ON sm.category_id = sc.id
WHERE s.user_id = $1
  AND s.status = 'active'
GROUP BY sc.id, sc.name, sc.icon_url
ORDER BY total_monthly_spend DESC;

-- name: GetSubscriptionSpendTrends :many
WITH monthly_spend AS (
    SELECT 
        DATE_TRUNC('month', st.transaction_date) as month,
        SUM(st.amount) FILTER (WHERE st.status = 'success') as total_spent,
        COUNT(*) FILTER (WHERE st.status = 'success') as successful_payments,
        COUNT(*) FILTER (WHERE st.status = 'failed') as failed_payments,
        COUNT(DISTINCT st.subscription_id) as active_subscriptions
    FROM subscription_transactions st
    JOIN subscriptions s ON st.subscription_id = s.id
    WHERE s.user_id = $1
      AND st.transaction_date >= $2
      AND st.transaction_date <= $3
    GROUP BY DATE_TRUNC('month', st.transaction_date)
)
SELECT 
    month,
    total_spent,
    successful_payments,
    failed_payments,
    active_subscriptions,
    total_spent - LAG(total_spent) OVER (ORDER BY month) as spend_change,
    ROUND((total_spent - LAG(total_spent) OVER (ORDER BY month)) / 
          NULLIF(LAG(total_spent) OVER (ORDER BY month), 0) * 100, 2) as spend_change_percentage
FROM monthly_spend
ORDER BY month DESC;

-- name: GetTopMerchantsBySpend :many
SELECT 
    sm.merchant_name,
    sm.logo_url,
    sc.name as category_name,
    COUNT(DISTINCT s.id) as subscription_count,
    SUM(st.amount) FILTER (WHERE st.status = 'success') as total_spent,
    MAX(st.transaction_date) as last_payment_date,
    COUNT(*) FILTER (WHERE st.status = 'failed') as failed_payments
FROM subscription_transactions st
JOIN subscriptions s ON st.subscription_id = s.id
JOIN subscription_merchants sm ON s.merchant_id = sm.id
LEFT JOIN subscription_categories sc ON sm.category_id = sc.id
WHERE s.user_id = $1
  AND st.transaction_date >= $2
GROUP BY sm.id, sm.merchant_name, sm.logo_url, sc.name
ORDER BY total_spent DESC
LIMIT $3;

-- =====================================================
-- Admin Analytics
-- =====================================================

-- name: GetPlatformSubscriptionStats :one
SELECT 
    COUNT(DISTINCT user_id) as total_users_with_subscriptions,
    COUNT(*) FILTER (WHERE status = 'active') as total_active_subscriptions,
    COUNT(*) FILTER (WHERE status = 'cancelled') as total_cancelled_subscriptions,
    SUM(amount) FILTER (WHERE status = 'active') as total_monthly_volume,
    AVG(amount) FILTER (WHERE status = 'active') as avg_subscription_amount,
    SUM(total_spend) as total_lifetime_volume,
    SUM(failed_payment_count) as total_failed_payments
FROM subscriptions;

-- name: GetTopMerchantsGlobal :many
SELECT 
    sm.merchant_name,
    sm.logo_url,
    sc.name as category_name,
    COUNT(DISTINCT s.user_id) as unique_users,
    COUNT(DISTINCT s.id) as total_subscriptions,
    SUM(s.amount) as monthly_volume,
    SUM(s.total_spend) as lifetime_volume,
    AVG(s.amount) as avg_amount,
    ROUND(COUNT(*) FILTER (WHERE s.status = 'active')::NUMERIC / 
          NULLIF(COUNT(*), 0) * 100, 2) as active_rate_percentage
FROM subscriptions s
JOIN subscription_merchants sm ON s.merchant_id = sm.id
LEFT JOIN subscription_categories sc ON sm.category_id = sc.id
GROUP BY sm.id, sm.merchant_name, sm.logo_url, sc.name
ORDER BY monthly_volume DESC
LIMIT $1;

-- name: GetSubscriptionRenewalSuccessRate :many
SELECT 
    DATE_TRUNC('day', st.transaction_date) as date,
    COUNT(*) FILTER (WHERE st.status = 'success') as successful_renewals,
    COUNT(*) FILTER (WHERE st.status = 'failed') as failed_renewals,
    COUNT(*) as total_renewals,
    ROUND(COUNT(*) FILTER (WHERE st.status = 'success')::NUMERIC / 
          NULLIF(COUNT(*), 0) * 100, 2) as success_rate
FROM subscription_transactions st
WHERE st.transaction_date >= $1
  AND st.transaction_date <= $2
  AND st.is_renewal = true
GROUP BY DATE_TRUNC('day', st.transaction_date)
ORDER BY date DESC;

-- name: GetCategoryPerformance :many
SELECT 
    sc.name as category_name,
    COUNT(DISTINCT s.user_id) as unique_users,
    COUNT(DISTINCT s.id) as total_subscriptions,
    SUM(s.amount) FILTER (WHERE s.status = 'active') as monthly_volume,
    AVG(s.amount) as avg_subscription_amount,
    SUM(s.failed_payment_count) as total_failures,
    ROUND(AVG(s.confidence_score) * 100, 2) as avg_confidence_score
FROM subscriptions s
JOIN subscription_merchants sm ON s.merchant_id = sm.id
JOIN subscription_categories sc ON sm.category_id = sc.id
GROUP BY sc.id, sc.name
ORDER BY monthly_volume DESC;

-- =====================================================
-- Merchant Detection & Pattern Matching
-- =====================================================

-- name: DetectPotentialSubscription :many
-- WITH transaction_patterns AS (
--     SELECT 
--         t.user_id,
--         t.merchant_descriptor,
--         COUNT(*) as transaction_count,
--         AVG(t.amount) as avg_amount,
--         STDDEV(t.amount) as amount_stddev,
--         MIN(t.created_at) as first_transaction,
--         MAX(t.created_at) as last_transaction,
--         AVG(EXTRACT(EPOCH FROM (t.created_at - LAG(t.created_at) OVER (PARTITION BY t.user_id, t.merchant_descriptor ORDER BY t.created_at))) / 86400) as avg_days_between
--     FROM transactions t
--     WHERE t.type = 'card_payment'
--       AND t.status = 'success'
--       AND t.created_at >= CURRENT_TIMESTAMP - INTERVAL '6 months'
--     GROUP BY t.user_id, t.merchant_descriptor
--     HAVING COUNT(*) >= 2
-- )
-- SELECT 
--     tp.*,
--     CASE 
--         WHEN tp.transaction_count >= 3 AND tp.avg_days_between BETWEEN 25 AND 35 THEN 0.95
--         WHEN tp.transaction_count >= 2 AND tp.avg_days_between BETWEEN 25 AND 35 THEN 0.80
--         WHEN tp.transaction_count >= 3 AND tp.avg_days_between BETWEEN 6 AND 8 THEN 0.90
--         WHEN tp.transaction_count >= 2 AND tp.amount_stddev < (tp.avg_amount * 0.1) THEN 0.85
--         ELSE 0.60
--     END as confidence_score,
--     CASE
--         WHEN tp.avg_days_between BETWEEN 6 AND 8 THEN 'weekly'
--         WHEN tp.avg_days_between BETWEEN 13 AND 16 THEN 'biweekly'
--         WHEN tp.avg_days_between BETWEEN 25 AND 35 THEN 'monthly'
--         WHEN tp.avg_days_between BETWEEN 85 AND 95 THEN 'quarterly'
--         WHEN tp.avg_days_between BETWEEN 350 AND 370 THEN 'yearly'
--         ELSE 'irregular'
--     END as detected_frequency
-- FROM transaction_patterns tp
-- WHERE NOT EXISTS (
--     SELECT 1 FROM subscriptions s
--     WHERE s.user_id = tp.user_id
--       AND s.merchant_name = tp.merchant_descriptor
--       AND s.status IN ('active', 'pending')
-- )
-- ORDER BY tp.transaction_count DESC, confidence_score DESC;

-- name: RefreshSubscriptionAnalytics :exec
REFRESH MATERIALIZED VIEW CONCURRENTLY subscription_spending_analytics;