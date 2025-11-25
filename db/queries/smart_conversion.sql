-- name: CreateConversionRule :one
INSERT INTO conversion_rules (
    user_id,
    source_currency,
    target_currency,
    source_wallet_id,
    target_wallet_id,
    trigger_type,
    trigger_rate,
    trigger_condition,
    conversion_type,
    fixed_amount,
    percentage,
    schedule_frequency,
    schedule_day_of_week,
    schedule_day_of_month,
    schedule_time,
    next_execution_at,
    timezone,
    description,
    label
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19
) RETURNING *;

-- name: GetConversionRule :one
SELECT * FROM conversion_rules
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetConversionRulesByUser :many
SELECT * FROM conversion_rules
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetActiveConversionRules :many
SELECT * FROM conversion_rules
WHERE user_id = $1 
    AND status = 'active'
    AND is_active = TRUE
    AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetRulesByCurrencyPair :many
SELECT * FROM conversion_rules
WHERE user_id = $1
    AND source_currency = $2
    AND target_currency = $3
    AND deleted_at IS NULL;

-- name: GetActiveRuleByCurrencyPair :one
SELECT * FROM conversion_rules
WHERE user_id = $1
    AND source_currency = $2
    AND target_currency = $3
    AND is_active = TRUE
    AND status = 'active'
    AND deleted_at IS NULL
LIMIT 1;

-- name: GetScheduledRulesDue :many
SELECT * FROM conversion_rules
WHERE status = 'active'
    AND is_active = TRUE
    AND deleted_at IS NULL
    AND trigger_type = 'scheduled'
    AND next_execution_at <= NOW()
ORDER BY next_execution_at ASC;

-- name: UpdateConversionRule :one
UPDATE conversion_rules
SET trigger_rate = COALESCE(sqlc.narg('trigger_rate'), trigger_rate),
    percentage = COALESCE(sqlc.narg('percentage'), percentage),
    fixed_amount = COALESCE(sqlc.narg('fixed_amount'), fixed_amount),
    label = COALESCE(sqlc.narg('label'), label),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateRuleStatus :one
UPDATE conversion_rules
SET status = $2,
    is_active = $3,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateRuleExecution :one
UPDATE conversion_rules
SET execution_count = execution_count + 1,
    last_triggered_at = NOW(),
    last_trigger_rate = $2,
    next_execution_at = COALESCE(sqlc.narg('next_execution_at'), next_execution_at),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateRuleFailure :one
UPDATE conversion_rules
SET failure_count = failure_count + 1,
    last_failure_reason = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteConversionRule :one
UPDATE conversion_rules
SET deleted_at = NOW(),
    is_active = FALSE,
    updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CreateConversionHistory :one
INSERT INTO conversion_history (
    conversion_rule_id,
    user_id,
    transaction_id,
    source_currency,
    target_currency,
    source_wallet_id,
    target_wallet_id,
    trigger_rate,
    executed_rate,
    rate_provider,
    source_amount,
    target_amount,
    fees,
    net_amount,
    source_balance_before,
    source_balance_after,
    target_balance_before,
    target_balance_after,
    execution_type,
    trigger_type,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
) RETURNING *;

-- name: GetConversionHistory :one
SELECT * FROM conversion_history WHERE id = $1;

-- name: GetConversionHistoryByUser :many
SELECT * FROM conversion_history
WHERE user_id = $1
ORDER BY executed_at DESC
LIMIT $2 OFFSET $3;

-- name: GetConversionHistoryByRule :many
SELECT * FROM conversion_history
WHERE conversion_rule_id = $1
ORDER BY executed_at DESC;

-- name: GetConversionHistoryStats :one
SELECT 
    COUNT(*) as total_conversions,
    COUNT(CASE WHEN status = 'success' THEN 1 END) as successful_conversions,
    COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_conversions,
    COALESCE(SUM(CASE WHEN status = 'success' THEN source_amount ELSE 0 END), 0) as total_converted,
    COALESCE(SUM(CASE WHEN status = 'success' THEN fees ELSE 0 END), 0) as total_fees
FROM conversion_history
WHERE user_id = $1 
AND executed_at >= $2;

-- name: UpdateConversionHistoryStatus :one
UPDATE conversion_history
SET status = $2,
    failure_reason = COALESCE(sqlc.narg('failure_reason'), failure_reason)
WHERE id = $1
RETURNING *;

-- ============================================================
-- BANK ACCOUNT QUERIES
-- ============================================================

-- name: CreateBankAccount :one
INSERT INTO bank_accounts (
    user_id,
    account_name,
    account_number,
    bank_code,
    bank_name,
    account_type,
    currency,
    label,
    description
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetBankAccount :one
SELECT * FROM bank_accounts
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetBankAccountsByUser :many
SELECT * FROM bank_accounts
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY is_default DESC, created_at DESC;

-- name: GetDefaultBankAccount :one
SELECT * FROM bank_accounts
WHERE user_id = $1 
    AND is_default = TRUE 
    AND is_active = TRUE
    AND deleted_at IS NULL
LIMIT 1;

-- name: ClearDefaultBankAccounts :exec
UPDATE bank_accounts
SET is_default = FALSE, updated_at = NOW()
WHERE user_id = $1 AND deleted_at IS NULL;

-- name: SetDefaultBankAccount :one
UPDATE bank_accounts
SET is_default = TRUE, updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: VerifyBankAccount :one
UPDATE bank_accounts
SET is_verified = TRUE,
    verified_at = NOW(),
    verification_method = $2,
    verification_reference = $3,
    status = 'active',
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: UpdateBankAccountStatus :one
UPDATE bank_accounts
SET status = $2,
    is_active = $3,
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteBankAccount :one
UPDATE bank_accounts
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- ============================================================
-- QR CODE QUERIES
-- ============================================================

-- name: CreateQRCode :one
INSERT INTO qr_codes (
    user_id,
    qr_type,
    currency_preference,
    conversion_mode,
    network,
    crypto_currency,
    cryptomus_address_id,
    linked_wallet_id,
    linked_bank_account_id,
    qr_code_data,
    qr_code_image_url,
    label,
    description,
    fixed_amount,
    min_amount,
    max_amount,
    usage_limit,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18
) RETURNING *;

-- name: GetQRCode :one
SELECT * FROM qr_codes
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetQRCodesByCryptomusAddress :many
SELECT * FROM qr_codes
WHERE cryptomus_address_id = $1 AND deleted_at IS NULL;

-- name: GetQRCodeByToken :one
SELECT * FROM qr_codes
WHERE token = $1 AND deleted_at IS NULL;

-- name: GetQRCodesByUser :many
SELECT * FROM qr_codes
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: GetActiveQRCodes :many
SELECT * FROM qr_codes
WHERE user_id = $1 
    AND status = 'active'
    AND deleted_at IS NULL
    AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY created_at DESC;

-- name: UpdateQRCodeUsage :one
UPDATE qr_codes
SET usage_count = usage_count + 1,
    last_used_at = NOW(),
    status = CASE 
        WHEN usage_limit IS NOT NULL AND usage_count + 1 >= usage_limit THEN 'used'
        ELSE status
    END,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRCodeStatus :one
UPDATE qr_codes
SET status = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteQRCode :one
UPDATE qr_codes
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: CreateQRTransaction :one
INSERT INTO qr_transactions (
    qr_code_id,
    user_id,
    cryptomus_transaction_id,
    cryptomus_order_id,
    cryptomus_uuid,
    cryptomus_address_id,
    webhook_data,
    crypto_currency,
    crypto_network,
    crypto_amount,
    crypto_amount_usd,
    transaction_hash,
    required_confirmations,
    bank_account_id,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15
) RETURNING *;

-- name: GetQRTransaction :one
SELECT * FROM qr_transactions WHERE id = $1;

-- name: GetQRTransactionByCryptomusID :one
SELECT * FROM qr_transactions 
WHERE cryptomus_transaction_id = $1
LIMIT 1;

-- name: GetQRTransactionsByUser :many
SELECT * FROM qr_transactions
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetQRTransactionsByQRCode :many
SELECT * FROM qr_transactions
WHERE qr_code_id = $1
ORDER BY created_at DESC;

-- name: GetPendingConfirmations :many
SELECT * FROM qr_transactions
WHERE status IN ('received', 'confirming')
    AND confirmation_blocks < required_confirmations
    AND created_at > NOW() - INTERVAL '24 hours'
ORDER BY created_at ASC;

-- name: GetTransactionsReadyForConversion :many
SELECT * FROM qr_transactions
WHERE status = 'confirmed'
    AND conversion_started_at IS NULL
ORDER BY payment_confirmed_at ASC
LIMIT $1;

-- name: GetTransactionsReadyForPayout :many
SELECT * FROM qr_transactions
WHERE status = 'converting'
    AND conversion_completed_at IS NOT NULL
    AND payout_initiated_at IS NULL
ORDER BY conversion_completed_at ASC
LIMIT $1;

-- name: UpdateQRTransactionConfirmation :one
UPDATE qr_transactions
SET confirmation_blocks = $2,
    status = CASE 
        WHEN $2 >= required_confirmations THEN 'confirmed'
        ELSE 'confirming'
    END,
    payment_confirmed_at = CASE
        WHEN $2 >= required_confirmations AND payment_confirmed_at IS NULL THEN NOW()
        ELSE payment_confirmed_at
    END,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRTransactionToConverting :one
UPDATE qr_transactions
SET status = 'converting',
    conversion_started_at = NOW(),
    conversion_rate = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRTransactionConversionComplete :one
UPDATE qr_transactions
SET status = 'sending_to_bank',
    conversion_completed_at = NOW(),
    fiat_currency = $2,
    fiat_amount = $3,
    conversion_fees = $4,
    platform_fees = $5,
    network_fees = $6,
    total_fees = $7,
    net_amount = $8,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRTransactionPayoutInitiated :one
UPDATE qr_transactions
SET payout_initiated_at = NOW(),
    payout_reference = $2,
    payout_provider = $3,
    payout_provider_response = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRTransactionPayoutCompleted :one
UPDATE qr_transactions
SET status = 'completed',
    payout_completed_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRTransactionStatus :one
UPDATE qr_transactions
SET status = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateQRTransactionFailure :one
UPDATE qr_transactions
SET status = 'failed',
    failure_reason = $2,
    failure_stage = $3,
    retry_count = retry_count + 1,
    last_retry_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetFailedQRTransactions :many
SELECT * FROM qr_transactions
WHERE status = 'failed'
    AND retry_count < max_retries
    AND (last_retry_at IS NULL OR last_retry_at < NOW() - INTERVAL '5 minutes')
ORDER BY created_at ASC
LIMIT $1;

-- name: GetQRTransactionStats :one
SELECT 
    COUNT(*) as total_transactions,
    COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed_transactions,
    COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed_transactions,
    COALESCE(SUM(CASE WHEN status = 'completed' THEN crypto_amount ELSE 0 END), 0) as total_crypto_received,
    COALESCE(SUM(CASE WHEN status = 'completed' THEN net_amount ELSE 0 END), 0) as total_net_payout
FROM qr_transactions
WHERE user_id = $1
    AND created_at >= $2;