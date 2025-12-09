-- name: CreateCardPlan :one
INSERT INTO card_plans (
    name, description, creation_fee, monthly_maintenance_fee,
    monthly_spending_limit, transaction_limit, daily_spending_limit,
    max_cards_per_user, supports_international, supports_online, supports_atm, card_limit
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: GetCardPlan :one
SELECT * FROM card_plans
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetCardPlanByName :one
SELECT * FROM card_plans
WHERE name = $1 AND deleted_at IS NULL;

-- name: ListActiveCardPlans :many
SELECT * FROM card_plans
WHERE is_active = TRUE AND deleted_at IS NULL
ORDER BY creation_fee ASC;

-- name: UpdateCardPlan :one
UPDATE card_plans
SET 
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    creation_fee = COALESCE(sqlc.narg('creation_fee'), creation_fee),
    monthly_maintenance_fee = COALESCE(sqlc.narg('monthly_maintenance_fee'), monthly_maintenance_fee),
    monthly_spending_limit = COALESCE(sqlc.narg('monthly_spending_limit'), monthly_spending_limit),
    transaction_limit = COALESCE(sqlc.narg('transaction_limit'), transaction_limit),
    daily_spending_limit = COALESCE(sqlc.narg('daily_spending_limit'), daily_spending_limit),
    is_active = COALESCE(sqlc.narg('is_active'), is_active),
    card_limit = COALESCE(sqlc.narg('card_limit'), card_limit),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteCardPlan :exec
UPDATE card_plans
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- ============================================================================
-- VIRTUAL CARDS
-- ============================================================================

-- name: CreateVirtualCard :one
INSERT INTO virtual_cards (
    user_id, card_plan_id, bridgecard_card_id, card_name, card_color,
    currency, status, next_billing_date, spending_month
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetVirtualCard :one
SELECT * FROM virtual_cards
WHERE id = $1 AND terminated_at IS NULL;

-- name: GetVirtualCardByBridgeCardID :one
SELECT * FROM virtual_cards
WHERE bridgecard_card_id = $1 AND terminated_at IS NULL;

-- name: GetUserCards :many
SELECT vc.*, cp.name as plan_name, cp.monthly_spending_limit, cp.transaction_limit
FROM virtual_cards vc
JOIN card_plans cp ON vc.card_plan_id = cp.id
WHERE vc.user_id = $1 AND vc.terminated_at IS NULL
ORDER BY vc.created_at DESC;

-- name: GetUserActiveCardsCount :one
SELECT COUNT(*) FROM virtual_cards
WHERE user_id = $1 AND status = 'active' AND terminated_at IS NULL;

-- name: UpdateCardStatus :one
UPDATE virtual_cards
SET 
    status = $2,
    status_reason = $3,
    updated_at = NOW()
WHERE id = $1 AND terminated_at IS NULL
RETURNING *;

-- name: UpdateCardName :one
UPDATE virtual_cards
SET 
    card_name = $2,
    card_color = COALESCE(sqlc.narg('card_color'), card_color),
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND terminated_at IS NULL
RETURNING *;

-- name: UpdateCardAutoTopup :one
UPDATE virtual_cards
SET 
    auto_topup_enabled = $2,
    auto_topup_threshold_cents = $3,
    auto_topup_amount_cents = $4,
    auto_topup_source_wallet_id = $5,
    updated_at = NOW()
WHERE id = $1 AND terminated_at IS NULL
RETURNING *;

-- name: UpdateCardSpending :one
UPDATE virtual_cards
SET 
    current_month_spend_cents = $2,
    current_day_spend_cents = $3,
    spending_month = $4,
    spending_day = $5,
    total_transactions_count = total_transactions_count + 1,
    last_transaction_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND terminated_at IS NULL
RETURNING *;

-- name: ResetMonthlySpending :exec
UPDATE virtual_cards
SET 
    current_month_spend_cents = 0,
    spending_month = $1
WHERE spending_month < $1;

-- name: ResetDailySpending :exec
UPDATE virtual_cards
SET 
    current_day_spend_cents = 0,
    spending_day = $1
WHERE spending_day < $1;

-- name: UpdateCardBilling :one
UPDATE virtual_cards
SET 
    next_billing_date = $2,
    last_billing_date = $3,
    updated_at = NOW()
WHERE id = $1 AND terminated_at IS NULL
RETURNING *;

-- name: TerminateCard :one
UPDATE virtual_cards
SET 
    status = 'terminated',
    terminated_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND terminated_at IS NULL
RETURNING *;

-- name: GetCardsForBilling :many
SELECT * FROM virtual_cards
WHERE status = 'active' 
  AND next_billing_date <= NOW()
  AND terminated_at IS NULL
ORDER BY next_billing_date ASC
LIMIT $1;

-- name: GetCardsForAutoTopup :many
SELECT vc.*, cp.monthly_spending_limit
FROM virtual_cards vc
JOIN card_plans cp ON vc.card_plan_id = cp.id
WHERE vc.status = 'active'
  AND vc.auto_topup_enabled = TRUE
  AND vc.terminated_at IS NULL;

-- ============================================================================
-- CARD FUNDING
-- ============================================================================

-- name: CreateCardFunding :one
INSERT INTO card_funding_history (
    card_id, user_id, source_wallet_id, amount, currency,
    source_currency, exchange_rate, funding_type, initiated_by, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: GetCardFunding :one
SELECT * FROM card_funding_history WHERE id = $1;

-- name: UpdateCardFundingStatus :one
UPDATE card_funding_history
SET 
    status = $2,
    failure_reason = $3,
    bridgecard_transaction_id = COALESCE(sqlc.narg('bridgecard_transaction_id'), bridgecard_transaction_id),
    completed_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: GetUserCardFundingHistory :many
SELECT * FROM card_funding_history
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetCardFundingHistory :many
SELECT * FROM card_funding_history
WHERE card_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- ============================================================================
-- CARD TRANSACTIONS
-- ============================================================================

-- name: CreateCardTransaction :one
INSERT INTO card_transactions (
    card_id, user_id, bridgecard_transaction_id, transaction_type,
    merchant_name, merchant_category, merchant_category_code,
    merchant_country, merchant_city, amount_cents, fee_cents,
    currency, billing_amount_cents, billing_currency,
    status, balance_after_cents, transaction_date, webhook_received_at, raw_webhook_data
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
) RETURNING *;

-- name: GetCardTransaction :one
SELECT * FROM card_transactions WHERE id = $1;

-- name: GetCardTransactionByBridgeCardID :one
SELECT * FROM card_transactions WHERE bridgecard_transaction_id = $1;

-- name: UpdateCardTransactionStatus :one
UPDATE card_transactions
SET 
    status = $2,
    decline_reason = $3
WHERE id = $1
RETURNING *;

-- name: LinkTransactionToSubscription :one
UPDATE card_transactions
SET 
    is_recurring_merchant = TRUE,
    subscription_id = $2
WHERE id = $1
RETURNING *;

-- name: GetCardTransactions :many
SELECT * FROM card_transactions
WHERE card_id = $1
ORDER BY transaction_date DESC
LIMIT $2 OFFSET $3;

-- name: GetUserCardTransactions :many
SELECT ct.*, vc.card_name
FROM card_transactions ct
JOIN virtual_cards vc ON ct.card_id = vc.id
WHERE ct.user_id = $1
ORDER BY ct.transaction_date DESC
LIMIT $2 OFFSET $3;

-- name: GetTransactionsByMerchant :many
SELECT * FROM card_transactions
WHERE card_id = $1 
  AND LOWER(merchant_name) = LOWER($2)
  AND status = 'approved'
ORDER BY transaction_date DESC;

-- name: GetRecurringTransactions :many
SELECT * FROM card_transactions
WHERE card_id = $1 AND is_recurring_merchant = TRUE
ORDER BY transaction_date DESC
LIMIT $2 OFFSET $3;

-- name: GetVirtualCardTransactionsByDateRange :many
SELECT * FROM card_transactions
WHERE card_id = $1 
  AND transaction_date >= $2
  AND transaction_date <= $3
ORDER BY transaction_date DESC;

-- ============================================================================
-- SUBSCRIPTION MERCHANTS
-- ============================================================================

-- name: CreateSubscriptionMerchant :one
INSERT INTO subscription_merchants (
    merchant_name, display_name, aliases, category, subcategory,
    logo_url, website, description, typical_intervals,
    typical_amounts_cents, mcc_codes, match_confidence, auto_detect
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: GetSubscriptionMerchant :one
SELECT * FROM subscription_merchants WHERE id = $1;

-- name: GetSubscriptionMerchantByName :one
SELECT * FROM subscription_merchants 
WHERE LOWER(merchant_name) = LOWER($1) AND is_active = TRUE;

-- name: FindSubscriptionMerchantByPattern :one
SELECT * FROM subscription_merchants
WHERE is_active = TRUE 
  AND auto_detect = TRUE
  AND (
    LOWER(merchant_name) = LOWER($1)
    OR LOWER($1) = ANY(SELECT LOWER(unnest(aliases)))
  )
LIMIT 1;

-- name: ListSubscriptionMerchants :many
SELECT * FROM subscription_merchants
WHERE is_active = TRUE
ORDER BY display_name ASC;

-- name: ListSubscriptionMerchantsByCategory :many
SELECT * FROM subscription_merchants
WHERE category = $1 AND is_active = TRUE
ORDER BY display_name ASC;

-- name: UpdateSubscriptionMerchant :one
UPDATE subscription_merchants
SET 
    display_name = COALESCE(sqlc.narg('display_name'), display_name),
    aliases = COALESCE(sqlc.narg('aliases'), aliases),
    category = COALESCE(sqlc.narg('category'), category),
    subcategory = COALESCE(sqlc.narg('subcategory'), subcategory),
    logo_url = COALESCE(sqlc.narg('logo_url'), logo_url),
    typical_intervals = COALESCE(sqlc.narg('typical_intervals'), typical_intervals),
    typical_amounts_cents = COALESCE(sqlc.narg('typical_amounts_cents'), typical_amounts_cents),
    auto_detect = COALESCE(sqlc.narg('auto_detect'), auto_detect),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- ============================================================================
-- USER SUBSCRIPTIONS
-- ============================================================================

-- name: CreateUserSubscription :one
INSERT INTO user_subscriptions (
    user_id, card_id, merchant_id, merchant_name, display_name,
    category, amount_cents, currency, billing_interval_days,
    first_charge_date, last_charge_date, next_estimated_charge_date,
    status, confidence_score, reminder_enabled, reminder_days_before
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
) RETURNING *;

-- name: GetUserSubscription :one
SELECT us.*, sm.logo_url, sm.website
FROM user_subscriptions us
LEFT JOIN subscription_merchants sm ON us.merchant_id = sm.id
WHERE us.id = $1;

-- name: GetUserSubscriptions :many
SELECT us.*, sm.logo_url, sm.website
FROM user_subscriptions us
LEFT JOIN subscription_merchants sm ON us.merchant_id = sm.id
WHERE us.user_id = $1 AND us.status = 'active'
ORDER BY us.next_estimated_charge_date ASC;

-- name: GetUserSubscriptionsByCard :many
SELECT us.*, sm.logo_url, sm.website
FROM user_subscriptions us
LEFT JOIN subscription_merchants sm ON us.merchant_id = sm.id
WHERE us.card_id = $1 AND us.status = 'active'
ORDER BY us.next_estimated_charge_date ASC;

-- name: FindExistingSubscription :one
SELECT * FROM user_subscriptions
WHERE user_id = $1 
  AND card_id = $2
  AND LOWER(merchant_name) = LOWER($3)
  AND status = 'active'
LIMIT 1;

-- name: UpdateSubscriptionCharge :one
UPDATE user_subscriptions
SET 
    last_charge_date = $2,
    next_estimated_charge_date = $3,
    total_charges = total_charges + 1,
    amount_cents = COALESCE($4, amount_cents),
    confidence_score = LEAST(confidence_score + 0.1, 1.0),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateSubscriptionFailure :one
UPDATE user_subscriptions
SET 
    failed_charges = failed_charges + 1,
    last_failed_date = $2,
    last_failure_reason = $3,
    status = CASE 
        WHEN failed_charges + 1 >= 3 THEN 'failed'
        ELSE status
    END,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateSubscriptionStatus :one
UPDATE user_subscriptions
SET 
    status = $2,
    cancelled_at = CASE WHEN $2 = 'cancelled' THEN NOW() ELSE cancelled_at END,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateSubscriptionPreferences :one
UPDATE user_subscriptions
SET 
    reminder_enabled = COALESCE(sqlc.narg('reminder_enabled'), reminder_enabled),
    reminder_days_before = COALESCE(sqlc.narg('reminder_days_before'), reminder_days_before),
    custom_name = COALESCE(sqlc.narg('custom_name'), custom_name),
    user_confirmed = COALESCE(sqlc.narg('user_confirmed'), user_confirmed),
    updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: GetSubscriptionsDueForReminder :many
SELECT us.*, sm.logo_url
FROM user_subscriptions us
LEFT JOIN subscription_merchants sm ON us.merchant_id = sm.id
WHERE us.status = 'active'
  AND us.reminder_enabled = TRUE
  AND us.next_estimated_charge_date <= NOW() + ($1 || ' days')::INTERVAL
  AND us.next_estimated_charge_date > NOW()
  AND NOT EXISTS (
    SELECT 1 FROM subscription_reminders sr
    WHERE sr.subscription_id = us.id
      AND sr.reminder_type = 'upcoming_renewal'
      AND sr.scheduled_for::DATE = (us.next_estimated_charge_date - ($1 || ' days')::INTERVAL)::DATE
      AND sr.status IN ('pending', 'sent')
  )
LIMIT $2;

-- name: GetUserSubscriptionSummary :one
SELECT 
    COUNT(*) FILTER (WHERE status = 'active') as active_count,
    COUNT(*) FILTER (WHERE status = 'failed') as failed_count,
    COALESCE(SUM(amount_cents) FILTER (WHERE status = 'active'), 0) as total_monthly_spend_cents,
    MIN(next_estimated_charge_date) FILTER (WHERE status = 'active') as next_charge_date
FROM user_subscriptions
WHERE user_id = $1;

-- name: GetSubscriptionsByCategory :many
SELECT 
    category,
    COUNT(*) as subscription_count,
    SUM(amount_cents) as total_spend_cents
FROM user_subscriptions
WHERE user_id = $1 AND status = 'active'
GROUP BY category
ORDER BY total_spend_cents DESC;

-- ============================================================================
-- SUBSCRIPTION REMINDERS
-- ============================================================================

-- name: CreateSubscriptionReminder :one
INSERT INTO subscription_reminders (
    subscription_id, user_id, reminder_type, scheduled_for,
    title, message, action_url, channels, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetPendingReminders :many
SELECT sr.*, us.merchant_name, us.amount_cents
FROM subscription_reminders sr
JOIN user_subscriptions us ON sr.subscription_id = us.id
WHERE sr.status = 'pending' AND sr.scheduled_for <= NOW()
ORDER BY sr.scheduled_for ASC
LIMIT $1;

-- name: UpdateReminderStatus :one
UPDATE subscription_reminders
SET 
    status = $2,
    sent_at = CASE WHEN $2 = 'sent' THEN NOW() ELSE sent_at END
WHERE id = $1
RETURNING *;

-- name: GetUserReminders :many
SELECT * FROM subscription_reminders
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- ============================================================================
-- CARD BILLING HISTORY
-- ============================================================================

-- name: CreateCardBilling :one
INSERT INTO card_billing_history (
    card_id, user_id, card_plan_id, billing_type, amount,
    currency, billing_period_start, billing_period_end, source_wallet_id, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: UpdateCardBillingStatus :one
UPDATE card_billing_history
SET 
    status = $2,
    failure_reason = $3,
    processed_at = CASE WHEN $2 IN ('completed', 'failed') THEN NOW() ELSE processed_at END
WHERE id = $1
RETURNING *;

-- name: GetCardBillingHistory :many
SELECT * FROM card_billing_history
WHERE card_id = $1
ORDER BY billing_period_start DESC
LIMIT $2 OFFSET $3;

-- name: GetUserBillingHistory :many
SELECT cbh.*, vc.card_name, cp.name as plan_name
FROM card_billing_history cbh
JOIN virtual_cards vc ON cbh.card_id = vc.id
JOIN card_plans cp ON cbh.card_plan_id = cp.id
WHERE cbh.user_id = $1
ORDER BY cbh.billing_period_start DESC
LIMIT $2 OFFSET $3;

-- ============================================================================
-- ANALYTICS
-- ============================================================================

-- name: GetUserCardStats :one
SELECT 
    COUNT(DISTINCT vc.id) as total_cards,
    COUNT(DISTINCT vc.id) FILTER (WHERE vc.status = 'active') as active_cards,
    COUNT(DISTINCT ct.id) as total_transactions,
    COALESCE(SUM(ct.amount_cents) FILTER (WHERE ct.status = 'approved'), 0) as total_spend_cents,
    COUNT(DISTINCT us.id) FILTER (WHERE us.status = 'active') as active_subscriptions
FROM virtual_cards vc
LEFT JOIN card_transactions ct ON vc.id = ct.card_id
LEFT JOIN user_subscriptions us ON vc.id = us.card_id
WHERE vc.user_id = $1 AND vc.terminated_at IS NULL;

-- name: GetCardSpendingByMonth :many
SELECT 
    DATE_TRUNC('month', transaction_date) as month,
    COUNT(*) as transaction_count,
    SUM(amount_cents) as total_spend_cents
FROM card_transactions
WHERE card_id = $1 
  AND status = 'approved'
  AND transaction_date >= $2
GROUP BY DATE_TRUNC('month', transaction_date)
ORDER BY month DESC;

-- name: GetTopMerchantsBySpend :many
SELECT 
    merchant_name,
    COUNT(*) as transaction_count,
    SUM(amount_cents) as total_spend_cents,
    MAX(transaction_date) as last_transaction_date
FROM card_transactions
WHERE user_id = $1 
  AND status = 'approved'
  AND merchant_name IS NOT NULL
GROUP BY merchant_name
ORDER BY total_spend_cents DESC
LIMIT $2;