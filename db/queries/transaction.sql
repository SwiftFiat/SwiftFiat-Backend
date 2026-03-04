-- name: CreateTransaction :one
INSERT INTO transactions (
    user_id,
    type,
    description,
    transaction_flow,
    amount,
    currency,
    amount_usd,
    idempotency_key,
    t_from,
    t_to,
    direction,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: GetTransactionByIdempotencyKey :one
SELECT * FROM transactions
WHERE idempotency_key = $1;

-- name: CreateSwapTransferMetadata :one 
INSERT INTO swap_transfer_metadata (
    currency,
    transaction_id,
    transfer_type,
    description,
    source_wallet,
    destination_wallet,
    user_tag,
    rate,
    fees,
    received_amount,
    sent_amount
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: CreateCryptoMetadata :one
INSERT INTO crypto_transaction_metadata (
    destination_wallet,
    transaction_id,
    coin,
    source_hash,
    rate,
    fees,
    received_amount,
    sent_amount,
    service_provider,
    order_id,
    service_transaction_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: CreateGiftcardMetadata :one
INSERT INTO giftcard_transaction_metadata (
    source_wallet,
    transaction_id,
    rate,
    received_amount,
    sent_amount,
    fees,
    service_provider,
    service_transaction_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: UpdateGiftCardServiceTransactionID :one
UPDATE giftcard_transaction_metadata
SET service_transaction_id = $1
WHERE transaction_id = $2
RETURNING *;

-- name: UpdateBillServiceTransactionID :one
UPDATE services_metadata
SET service_transaction_id = $1
WHERE transaction_id = $2
RETURNING *;

-- name: CreateBankTransferMetadata :one
INSERT INTO bank_transfer_metadata (
    amount,
    service_charge,
    transaction_id,
    account_name,
    account_number,
    service_provider,
    service_transaction_id,
    type,
    status,
    amount_paid,
    points_earned
)
VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11
)
RETURNING *;

-- name: UpdateBankTransferStatus :one
UPDATE bank_transfer_metadata
SET status = $2, service_transaction_id = $3
WHERE transaction_id = $1
RETURNING *;


-- name: CreateServiceMetadata :one
INSERT INTO services_metadata (
    source_wallet,
    transaction_id,
    rate,
    received_amount,
    sent_amount,
    fees,
    service_type,
    service_provider,
    service_id,
    service_transaction_id,
    service_status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: UpdateServiceMetadataStatus :one
UPDATE services_metadata
SET service_status = $1
WHERE transaction_id = $2
RETURNING *;

-- name: GetTransactionByID :one
SELECT
    t.*,
    COALESCE(st.source_wallet, ct.destination_wallet, gt.source_wallet, sm.source_wallet) as source_wallet,
    COALESCE(st.destination_wallet, ct.destination_wallet) as destination_wallet,
    COALESCE(st.currency, ct.coin) as currency,
    COALESCE(st.rate, ct.rate, gt.rate, sm.rate) as rate,
    COALESCE(st.fees, ct.fees, gt.fees, sm.fees) as fees,
    COALESCE(st.received_amount, ct.received_amount, gt.received_amount, fw.amount, sm.received_amount) as received_amount,
    COALESCE(st.sent_amount, ct.sent_amount, gt.sent_amount, fw.amount, sm.sent_amount) as sent_amount
FROM transactions t
LEFT JOIN swap_transfer_metadata st ON t.id = st.transaction_id
LEFT JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
LEFT JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
LEFT JOIN bank_transfer_metadata fw ON t.id = bt.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
LEFT JOIN vault_transactions vt ON t.id = vt.transaction_id
LEFT JOIN conversion_history ch ON t.id = ch.transaction_id
LEFT JOIN qr_transactions qr ON t.id = qr.transaction_id
LEFT JOIN reward_transactions rt ON t.id = rt.transaction_id
LEFT JOIN card_transactions card_tx ON t.id = card_tx.transaction_id
WHERE t.id = $1 LIMIT 1;

-- name: GetTransactionByIDForUpdate :one
SELECT * FROM transactions
WHERE id = $1 LIMIT 1 
FOR UPDATE;

-- name: UpdateTransactionStatus :one
UPDATE transactions
SET status = $2
WHERE id = $1
RETURNING *;

-- name: GetPendingTransactions :many
SELECT * FROM transactions
WHERE status = 'pending'
ORDER BY created_at DESC
LIMIT sqlc.arg(_limit) OFFSET sqlc.arg(_offset);

-- name: GetTransactionMetadata :one
SELECT
    CASE t.type
        WHEN 'swap' THEN jsonb_build_object(
            'type', 'swap_transfer',
            'data', to_jsonb(st.*)
        )
        WHEN 'transfer' THEN jsonb_build_object(
            'type', 'swap_transfer',
            'data', to_jsonb(st.*)
        )
        WHEN 'crypto' THEN jsonb_build_object(
            'type', 'crypto',
            'data', to_jsonb(ct.*)
        )
        WHEN 'giftcard' THEN jsonb_build_object(
            'type', 'giftcard',
            'data', to_jsonb(gt.*)
        )
        WHEN 'withdrawal' THEN jsonb_build_object(
            'type', 'withdrawal',
            'data', to_jsonb(fw.*)
        )
        WHEN 'service' THEN jsonb_build_object(
            'type', 'service',
            'data', to_jsonb(sm.*)
        )
        WHEN 'vault' THEN jsonb_build_object(
            'type', 'vault',
            'data', to_jsonb(vt.*)
        )
        WHEN 'conversion' THEN jsonb_build_object(
            'type', 'conversion',
            'data', to_jsonb(ch.*)
        )
        WHEN 'qr_code' THEN jsonb_build_object(
            'type', 'qr_code',
            'data', to_jsonb(qr.*)
        )
        WHEN 'reward' THEN jsonb_build_object(
            'type', 'reward',
            'data', to_jsonb(rt.*)
        )
        WHEN 'card' THEN jsonb_build_object(
            'type', 'card',
            'data', to_jsonb(ct.*)
        )
    END as metadata
FROM transactions t
LEFT JOIN swap_transfer_metadata st ON t.id = st.transaction_id
LEFT JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
LEFT JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
LEFT JOIN bank_transfer_metadata fw ON t.id = bt.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
LEFT JOIN vault_transactions vt ON t.id = vt.transaction_id
LEFT JOIN conversion_history ch ON t.id = ch.transaction_id
LEFT JOIN qr_transactions qr ON t.id = qr.transaction_id
LEFT JOIN reward_transactions rt ON t.id = rt.transaction_id
LEFT JOIN card_transactions ct ON t.id = ct.transaction_id
WHERE t.id = $1 LIMIT 1;

-- name: GetTransactionWithMetadata :one
SELECT
    jsonb_build_object(
        'transaction', jsonb_build_object(
            'id', t.id,
            'type', t.type,
            'description', t.description,
            'transaction_flow', t.transaction_flow,
            'status', t.status,
            'created_at', t.created_at,
            'updated_at', t.updated_at,
            'metadata', CASE
            WHEN t.type IN ('vault') THEN (
                    SELECT jsonb_build_object(
                        'vault_id', vt.vault_id,
                        'transaction_type', vt.transaction_type,
                        'source_wallet', vt.source_wallet,
                        'destination_wallet', vt.destination_wallet,
                        'amount', vt.amount,
                        'currency', vt.currency,
                        'balance_before', vt.balance_before,
                        'balance_after', vt.balance_after,
                        'reference', vt.reference,
                        'description', vt.description,
                        'metadata', vt.metadata,
                        'status', vt.status,
                        'requires_2fa', vt.requires_2fa
                    )::jsonb
                    FROM public.vault_transactions vt
                    WHERE vt.transaction_id = t.id
                    LIMIT 1
                )
                WHEN t.type = 'electricity' THEN (
                    SELECT jsonb_build_object(
                        'amount', epm.amount,
                        'points_used', epm.points_used,
                        'amount_paid', epm.amount_paid,
                        'token', epm.token,
                        'customer_name', epm.customer_name,
                        'customer_address', epm.customer_address,
                        'units', epm.units,
                        'meter_number', epm.meter_number,
                        'debt', epm.debt,
                        'points_earned', epm.points_earned,
                        'phone_number', epm.phone_number,
                        'tax', epm.tax,
                        'date', epm.date
                    )::jsonb
                    FROM public.electricity_purchase_metadata epm
                    WHERE epm.transaction_id = t.id
                )
                WHEN t.type = 'rewards' THEN (
                    SELECT jsonb_build_object(
                        'transaction_type', fm.transaction_type,
                        'source_transaction_type', fm.source_transaction_type,
                        'transaction_amount', fm.transaction_amount,
                        'points_amount', fm.points_amount,
                        'naira_value', fm.naira_value,
                        'status', fm.status,
                        'created_at', fm.created_at
                    )::jsonb
                    FROM public.reward_transactions fm
                    WHERE fm.transaction_id = t.id
                )
                WHEN t.type = 'giftcard' THEN (
                    SELECT jsonb_build_object(
                        'source_wallet', gm.source_wallet,
                        'rate', gm.rate,
                        'received_amount', gm.received_amount,
                        'sent_amount', gm.sent_amount,
                        'fees', gm.fees,
                        'service_provider', gm.service_provider,
                        'service_transaction_id', gm.service_transaction_id
                    )::jsonb
                    FROM public.giftcard_transaction_metadata gm 
                    WHERE gm.transaction_id = t.id
                )
                WHEN t.type IN ('airtime', 'data', 'tv_subscription') THEN (
                    SELECT jsonb_build_object(
                        'amount', sm.amount,
                        'points_used', sm.points_used,
                        'type', sm.type,
                        'amount_paid', sm.amount_paid,
                        'service_charge', sm.service_charge,
                        'points_earned', sm.points_earned,
                        'phone_number', sm.phone_number,
                        'plan', sm.plan,
                        'reference', sm.reference,
                        'date', sm.date,
                        'status', sm.status
                    )::jsonb
                    FROM public.data_airtime_purchase_metadata sm
                    WHERE sm.transaction_id = t.id
                )
                WHEN t.type IN ('transfer', 'rapid_ramp') THEN (
                    SELECT jsonb_build_object(
                        'currency', stm.currency,
                        'type', stm.type,
                        'recipient', stm.recipient,
                        'sender', stm.sender,
                        'service_charge', stm.service_charge,
                        'amount', stm.amount,
                        'amount_paid', stm.amount_paid,
                        'bonus_earned', stm.bonus_earned,
                        'reference', stm.reference,
                        'status', stm.status,
                        'date', stm.date,
                        'description', stm.description
                    )::jsonb 
                    FROM public.wallet_transfer_metadata stm
                    WHERE stm.transaction_id = t.id
                )
                WHEN t.type IN ('card') THEN (
                    SELECT jsonb_build_object(
                        'card_id', cm.card_id,
                        'user_id', cm.user_id,
                        'transaction_id', cm.transaction_id,
                        'transaction_type', cm.transaction_type,
                        'merchant_category_code', cm.merchant_category_code,
                        'amount', cm.amount,
                        'fee', cm.fee,
                        'currency', cm.currency,
                        'status', cm.status,
                        'balance_after', cm.balance_after,
                        'mode', cm.mode,
                        'transaction_timestamp', cm.transaction_timestamp
                    )::jsonb
                    FROM public.card_transactions cm
                    WHERE cm.transaction_id = t.id
                )
                WHEN t.type IN ('swap') THEN (
                    SELECT jsonb_build_object(
                        'source_currency', ch.source_currency,
                        'target_currency', ch.target_currency,
                        'source_amount', ch.source_amount,
                        'target_amount', ch.target_amount,
                        'fees', ch.fees,
                        'net_amount', ch.net_amount,
                        'source_balance_before', ch.source_balance_before,
                        'target_balance_before', ch.target_balance_before,
                        'source_balance_after', ch.source_balance_after,
                        'target_balance_after', ch.target_balance_after,
                        'execution_type', ch.execution_type,
                        'status', ch.status,
                        'executed_at', ch.executed_at
                    )::jsonb
                    FROM public.conversion_history ch
                    WHERE ch.transaction_id = t.id
                )
                WHEN t.type IN ('qr_code') THEN (
                    SELECT jsonb_build_object(
                        'user_id', qr.user_id,
                        'qr_code_id', qr.qr_code_id,
                        'qr_order_id', qr.order_id,
                        'provider_transaction_id', qr.cryptomus_transaction_id,
                        'provider_order_id', qr.cryptomus_order_id,
                        'address_id', qr.cryptomus_address_id,
                        'crypto_currency', qr.crypto_currency,
                        'crypto_network', qr.crypto_network,
                        'crypto_amount', qr.crypto_amount,
                        'crypto_amount_usd', qr.crypto_amount_usd,
                        'transaction_hash', qr.transaction_hash,
                        'conversion_rate', qr.conversion_rate,
                        'fiat_currency', qr.fiat_currency,
                        'fiat_amount', qr.fiat_amount,
                        'conversion_fees', qr.conversion_fees, 
                        'net_amount', qr.net_amount,
                        'bank_account_id', qr.bank_account_id,
                        'status', qr.status,
                        'created_at', qr.created_at,
                        'failure_reason', qr.failure_reason,
                        'failure_stage', qr.failure_stage,
                        'retry_count', qr.retry_count,
                        'last_retry_at', qr.last_retry_at,
                        'payment_received_at', qr.payment_received_at,
                        'conversion_started_at', qr.conversion_started_at,
                        'payout_initiated_at', qr.payout_initiated_at,
                        'payout_completed_at', qr.payout_completed_at,
                        'updated_at', qr.updated_at
                    )::jsonb
                    FROM public.qr_transactions qr
                    WHERE qr.transaction_id = t.id
                )
                WHEN t.type IN ('reward') THEN (
                    SELECT jsonb_build_object(
                        'transaction_type', rt.transaction_type,
                        'points_amount', rt.points_amount,
                        'naira_value', rt.naira_value,
                        'source_transaction_type', rt.source_transaction_type,
                        'transaction_amount', rt.transaction_amount,
                        'status', rt.status,
                        'description', rt.description,
                        'created_at', rt.created_at
                    )::jsonb
                    FROM public.reward_transactions rt
                    WHERE rt.transaction_id = t.id
                )
                WHEN t.type IN ('referral') THEN (
                    SELECT jsonb_build_object(
                        'amount', rft.amount,
                        'transaction_type', rft.transaction_type,
                        'reference', rft.reference,
                        'status', rft.status,
                        'date', rft.created_at
                    )::jsonb
                    FROM public.referral_transactions rft
                    WHERE rft.transaction_id = t.id
                )
                WHEN t.type IN ('crypto') THEN (
                    SELECT jsonb_build_object(
                        'destination_wallet', cm.destination_wallet,
                        'coin', cm.coin,
                        'rate', cm.rate,
                        'fees', cm.fees,
                        'order_id', cm.order_id,
                        'received_amount', cm.received_amount,
                        'sent_amount', cm.sent_amount,
                        'service_transaction_id', cm.service_transaction_id
                    )::jsonb
                    FROM public.crypto_transaction_metadata cm
                    WHERE cm.transaction_id = t.id
                )
                ELSE NULL
            END
        )
    ) as result
FROM public.transactions t
WHERE t.id = sqlc.arg(transaction_id)
LIMIT 1;

-- name: ListAllTransactionsWithUsers :many
SELECT
    t.id AS transaction_id,
    t.type AS transaction_type,
    t.description AS transaction_description,
    t.transaction_flow,
    t.status AS transaction_status,
    t.amount AS amount,
    t.idempotency_key AS reference,
    t.t_from AS from,
    t.t_to AS to,
    t.direction AS direction,
    t.risk_score AS risk_score,
    t.fraud_status AS fraud_status,
    t.flagged_at AS flagged_at,
    t.currency AS currency,
    t.amount_usd AS amount_usd,
    t.created_at AS transaction_created_at,
    t.updated_at AS transaction_updated_at,

    u.id AS user_id,
    u.first_name AS user_first_name,
    u.last_name AS user_last_name,
    u.email AS user_email,
    u.phone_number AS user_phone_number
FROM transactions t
JOIN users u ON u.id = t.user_id

-- Metadata joins (kept if needed for filtering / enrichment)
LEFT JOIN swap_transfer_metadata stm ON t.id = stm.transaction_id
LEFT JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
LEFT JOIN giftcard_transaction_metadata gtm ON t.id = gtm.transaction_id
LEFT JOIN bank_transfer_metadata fwm ON t.id = fwm.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
LEFT JOIN reward_transactions rt ON t.id = rt.transaction_id
LEFT JOIN vault_transactions vt ON t.id = vt.transaction_id
LEFT JOIN qr_transactions qr ON t.id = qr.transaction_id
LEFT JOIN card_transactions ct ON t.id = ct.transaction_id

ORDER BY t.created_at DESC;

-- name: ListTransactionsByType :many
SELECT
    t.id AS transaction_id,
    t.type AS transaction_type,
    t.description AS transaction_description,
    t.transaction_flow,
    t.amount,
    t.currency,
    t.amount_usd,
    t.status AS transaction_status,
    t.created_at AS transaction_created_at,

    u.id AS user_id,
    u.first_name AS user_first_name,
    u.last_name AS user_last_name,
    u.email AS user_email,
    u.phone_number AS user_phone_number
FROM transactions t
JOIN users u ON u.id = t.user_id
WHERE t.type = $1
ORDER BY t.created_at DESC;

 

-- name: GetTotalTransactions :one
SELECT
    COUNT(*) AS total_transactions
FROM transactions t
WHERE t.type IN ('swap', 'transfer', 'crypto', 'giftcard', 'vault', 'airtime', 'data', 'tv_subscription', 'electricity', 'qr_code', 'card', 'rewards', 'referral', 'rapid_ramp');


-- name: GetCryptoTransactionCounts :one
SELECT
    COUNT(*) FILTER (WHERE t.status = 'success') AS successful_transactions,
    COUNT(*) FILTER (WHERE t.status = 'failed') AS failed_transactions,
    COUNT(*) FILTER (WHERE t.status = 'pending') AS pending_transactions
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE t.type = 'crypto';

-- name: GetTotalCryptoTransactionAmount :one
SELECT
    COALESCE(SUM(ctm.sent_amount), 0) AS total_sent_amount,
    COALESCE(SUM(ctm.received_amount), 0) AS total_received_amount
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE t.type = 'crypto';

-- name: ListAllCryptoTransactions :many
SELECT
    ctm.id,
    ctm.destination_wallet,
    ctm.transaction_id,
    ctm.coin,
    ctm.source_hash,
    ctm.rate,
    ctm.fees,
    ctm.received_amount,
    ctm.sent_amount,
    ctm.service_provider,
    ctm.service_transaction_id,

    u.first_name,
    u.last_name
FROM crypto_transaction_metadata ctm
JOIN transactions t
    ON t.id = ctm.transaction_id
JOIN users u
    ON u.id = t.user_id
ORDER BY t.created_at DESC;

-- ORDER BY t.created_at DESC;

-- name: ListGiftcardTransactions :many
SELECT
    gtm.id AS metadata_id,
    gtm.source_wallet,
    gtm.transaction_id,
    gtm.rate,
    gtm.received_amount,
    gtm.sent_amount,
    gtm.fees,
    gtm.service_provider,
    gtm.service_transaction_id,
    t.type,
    t.description,
    t.transaction_flow,
    t.status,
    t.created_at,
    t.updated_at
FROM giftcard_transaction_metadata gtm
JOIN transactions t ON gtm.transaction_id = t.id
ORDER BY t.created_at DESC;

-- name: GetTotalTransactionVolume :one
SELECT CAST(COALESCE(SUM(amount_usd), 0) AS INTEGER) AS total_volume
FROM transactions  
WHERE status = 'successful';

-- name: GetTotalTransactionVolumeForUser :one
SELECT CAST(COALESCE(SUM(amount_usd), 0) AS INTEGER) AS total_volume
FROM transactions  
WHERE user_id = $1 AND status = 'successful';

-- name: GetTotalOutflowTransactions :one
SELECT CAST(COALESCE(SUM(amount_usd), 0) AS INTEGER) AS total_outflow
FROM transactions
WHERE transaction_flow = 'outflow';

-- name: GetTotalInflowTransactions :one
SELECT CAST(COALESCE(SUM(amount_usd), 0) AS INTEGER) AS total_inflow
FROM transactions
WHERE transaction_flow = 'inflow';

-- name: GetTotalInplatformTransactions :one
SELECT CAST(COALESCE(SUM(amount_usd), 0) AS INTEGER) AS total_inplatform
FROM transactions
WHERE transaction_flow = 'inplatform';

-- name: ListUserTransactions :many
SELECT
    t.id AS transaction_id,
    t.type AS transaction_type,
    t.description AS transaction_description,
    t.transaction_flow,
    t.status AS transaction_status, 
    t.amount AS amount,
    t.direction AS direction,
    t.t_from AS from,
    t.t_to AS to,
    t.currency AS currency,
    t.created_at AS transaction_created_at,
    t.updated_at AS transaction_updated_at,
    u.id AS user_id,
    u.first_name AS user_first_name,
    u.last_name AS user_last_name,
    u.email AS user_email,
    u.phone_number AS user_phone_number
FROM transactions t
JOIN users u ON u.id = t.user_id
WHERE t.user_id = $1
ORDER BY t.created_at DESC;

-- name: GetDailyTransactionSummary :many
SELECT
    date,
    SUM(crypto_usd)::float8 AS crypto_total_usd,
    SUM(giftcard_usd)::float8 AS giftcard_total_usd,
    SUM(bill_ngn)::float8 AS bill_payment_total_ngn,
    SUM(virtual_cards) AS virtual_cards_created,
    SUM(vaults) AS vaults_created
FROM (
    -- Crypto transactions (amount in USD)
    SELECT
        DATE(t.created_at) AS date,
        COALESCE(SUM(t.amount_usd), 0)::float8 AS crypto_usd,
        0::float8 AS giftcard_usd,
        0::float8 AS bill_ngn,
        0 AS virtual_cards,
        0 AS vaults
    FROM transactions t
    JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
    WHERE t.type = 'crypto' AND t.status = 'successful'
    GROUP BY DATE(t.created_at)

    UNION ALL

    -- Giftcard transactions (amount in USD)
    SELECT
        DATE(t.created_at) AS date,
        0::float8 AS crypto_usd,
        COALESCE(SUM(t.amount_usd), 0)::float8 AS giftcard_usd,
        0::float8 AS bill_ngn,
        0 AS virtual_cards,
        0 AS vaults
    FROM transactions t
    JOIN giftcard_transaction_metadata gtm ON t.id = gtm.transaction_id
    WHERE t.type = 'giftcard' AND t.status = 'successful'
    GROUP BY DATE(t.created_at)

    UNION ALL

    -- Bill payments (services like airtime, data, tv_subscription, utility_payment, electricity in NGN)
    SELECT
        DATE(t.created_at) AS date,
        0::float8 AS crypto_usd,
        0::float8 AS giftcard_usd,
        COALESCE(SUM(CASE WHEN t.currency = 'NGN' THEN t.amount ELSE 0 END), 0)::float8 AS bill_ngn,
        0 AS virtual_cards,
        0 AS vaults
    FROM transactions t
    JOIN services_metadata sm ON t.id = sm.transaction_id
    WHERE t.type = 'airtime' AND t.status = 'successful'
    GROUP BY DATE(t.created_at)

    UNION ALL

        -- Bill payments (services like, data, tv_subscription, utility_payment, electricity in NGN)
        SELECT
            DATE(t.created_at) AS date,
            0::float8 AS crypto_usd,
            0::float8 AS giftcard_usd,
            COALESCE(SUM(CASE WHEN t.currency = 'NGN' THEN t.amount ELSE 0 END), 0)::float8 AS bill_ngn,
            0 AS virtual_cards,
            0 AS vaults
        FROM transactions t
        JOIN services_metadata sm ON t.id = sm.transaction_id
        WHERE t.type = 'data' AND t.status = 'successful'
        GROUP BY DATE(t.created_at)

        UNION ALL

        -- Bill payments (services like, tv_subscription, utility_payment, electricity in NGN)
        SELECT
            DATE(t.created_at) AS date,
            0::float8 AS crypto_usd,
            0::float8 AS giftcard_usd,
            COALESCE(SUM(CASE WHEN t.currency = 'NGN' THEN t.amount ELSE 0 END), 0)::float8 AS bill_ngn,
            0 AS virtual_cards, 
            0 AS vaults
        FROM transactions t
        JOIN services_metadata sm ON t.id = sm.transaction_id
        WHERE t.type = 'tv' AND t.status = 'successful'
        GROUP BY DATE(t.created_at)
    
        UNION ALL

        SELECT
            DATE(t.created_at) AS date,
            0::float8 AS crypto_usd,
            0::float8 AS giftcard_usd,
            COALESCE(SUM(CASE WHEN t.currency = 'NGN' THEN t.amount ELSE 0 END), 0)::float8 AS bill_ngn,
            0 AS virtual_cards, 
            0 AS vaults
        FROM transactions t
        JOIN services_metadata sm ON t.id = sm.transaction_id
        WHERE t.type = 'electricity' AND t.status = 'successful'
        GROUP BY DATE(t.created_at)
    
        UNION ALL

    -- Virtual cards created
    SELECT
        DATE(created_at) AS date,
        0::float8 AS crypto_usd,
        0::float8 AS giftcard_usd,
        0::float8 AS bill_ngn,
        COUNT(*) AS virtual_cards,
        0 AS vaults
    FROM virtual_cards
    WHERE terminated_at IS NULL
    GROUP BY DATE(created_at)

    UNION ALL

    -- Vaults created
    SELECT
        DATE(created_at) AS date,
        0::float8 AS crypto_usd,
        0::float8 AS giftcard_usd,
        0::float8 AS bill_ngn,
        0 AS virtual_cards,
        COUNT(*) AS vaults
    FROM vault_savings
    GROUP BY DATE(created_at)
) AS daily_data
GROUP BY date
ORDER BY date DESC;

-- Additional queries for two-stage crypto transaction processing

-- name: GetCryptoMetadataByTransactionID :one
SELECT * FROM crypto_transaction_metadata
WHERE transaction_id = $1 LIMIT 1;

-- name: GetCryptoMetadataByOrderID :one
SELECT * FROM crypto_transaction_metadata
WHERE order_id = $1 LIMIT 1;

-- name: GetPendingCryptoTransactionByOrderID :one
SELECT t.* 
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE ctm.order_id = $1 
AND t.status = 'pending'
AND t.type = 'crypto'
LIMIT 1;

-- name: GetPendingCryptoTransactionByTransactionID :one
SELECT t.*, ctm.*
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE t.id = $1
AND t.status = 'pending'
AND t.type = 'crypto'
LIMIT 1;

-- name: UpdateCryptoTransactionStatus :one
UPDATE transactions
SET 
    status = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateCryptoMetadataRate :one
UPDATE crypto_transaction_metadata
SET 
    rate = $2,
    received_amount = $3
WHERE transaction_id = $1
RETURNING *;

-- name: GetWalletByIDForUpdate :one
SELECT * FROM swift_wallets
WHERE id = $1
LIMIT 1
FOR UPDATE;

-- name: ListPendingCryptoTransactions :many
SELECT 
    t.id,
    t.user_id,
    t.type,
    t.description,
    t.status,
    t.amount,
    t.currency,
    t.created_at,
    t.updated_at,
    ctm.coin,
    ctm.source_hash,
    ctm.sent_amount,
    ctm.service_transaction_id
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE t.status = 'pending'
AND t.type = 'crypto'
ORDER BY t.created_at DESC
LIMIT sqlc.arg(_limit) OFFSET sqlc.arg(_offset);

-- name: CountPendingCryptoTransactions :one
SELECT COUNT(*) as pending_count
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE t.status = 'pending'
AND t.type = 'crypto';

-- name: GetPendingTransactionsByUser :many
SELECT 
    t.*,
    ctm.coin,
    ctm.source_hash,
    ctm.rate,
    ctm.received_amount,
    ctm.sent_amount
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE t.user_id = $1
AND t.status = 'pending'
AND t.type = 'crypto'
ORDER BY t.created_at DESC;

-- name: GetTransactionTimeline :many
-- Get transaction status changes over time for a specific transaction
SELECT 
    t.id,
    t.status,
    t.created_at,
    t.updated_at,
    ctm.source_hash,
    ctm.coin,
    ctm.sent_amount
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
WHERE ctm.source_hash = $1
ORDER BY t.created_at ASC;

-- name: UpdateTransactionToFailed :one
-- Update a transaction to failed status with optional error message
UPDATE transactions
SET 
    status = 'failed',
    description = CASE 
        WHEN sqlc.arg(error_message)::text IS NOT NULL 
        THEN CONCAT(description, ' - Error: ', sqlc.arg(error_message)::text)
        ELSE description
    END,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: GetCryptoTransactionsByStatus :many
SELECT 
    t.id,
    t.user_id,
    t.type,
    t.status,
    t.amount,
    t.currency,
    t.created_at,
    t.updated_at,
    ctm.coin,
    ctm.source_hash,
    ctm.rate,
    ctm.received_amount,
    ctm.sent_amount,
    u.first_name,
    u.last_name,
    u.email
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
JOIN users u ON t.user_id = u.id
WHERE t.status = $1
AND t.type = 'crypto'
ORDER BY t.created_at DESC
LIMIT sqlc.arg(_limit) OFFSET sqlc.arg(_offset);

-- Get pending crypto transactions older than specified duration (for cleanup/monitoring)
-- name: GetStalePendingTransactions :many
SELECT 
    t.id,
    t.user_id,
    t.status,
    t.created_at,
    t.updated_at,
    ctm.source_hash,
    ctm.coin,
    ctm.sent_amount,
    u.email
FROM transactions t
JOIN crypto_transaction_metadata ctm ON t.id = ctm.transaction_id
JOIN users u ON t.user_id = u.id
WHERE t.status = 'pending'
AND t.type = 'crypto'
AND t.created_at < NOW() - sqlc.arg(age_duration)::interval
ORDER BY t.created_at ASC;

-- name: GetCryptoTransactionStats :one
-- Get statistics about crypto transactions by status
SELECT 
    COUNT(*) FILTER (WHERE status = 'pending') as pending_count,
    COUNT(*) FILTER (WHERE status = 'successful') as successful_count,
    COUNT(*) FILTER (WHERE status = 'failed') as failed_count,
    COALESCE(SUM(amount_usd) FILTER (WHERE status = 'successful'), 0) as total_successful_usd,
    COALESCE(SUM(amount_usd) FILTER (WHERE status = 'pending'), 0) as total_pending_usd,
    COALESCE(AVG(EXTRACT(EPOCH FROM (updated_at - created_at))) FILTER (WHERE status = 'successful'), 0) as avg_completion_time_seconds
FROM transactions
WHERE type = 'crypto'
AND created_at >= sqlc.arg(start_date)
AND created_at <= sqlc.arg(end_date);


-- name: CreateAirtimeDataMetadata :one
INSERT INTO data_airtime_purchase_metadata (
    amount,
    points_used,
    type,
    amount_paid,
    points_earned,
    phone_number,
    plan,
    reference,
    status,
    request_id,
    transaction_id,
    service_charge, --TODO:
    date
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;

-- name: UpdateAirtimePurchaseStatus :one
UPDATE data_airtime_purchase_metadata
SET status = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAirtimePurchaseStatusByReference :one
UPDATE data_airtime_purchase_metadata
SET status = $2
WHERE reference = $1
RETURNING *;

-- name: UpdateAirtimeStatusIfPending :one
UPDATE data_airtime_purchase_metadata
SET status = $2
WHERE reference = $1
  AND status = 'pending'
RETURNING *;


-- name: CreateElectricityPurchaseMetadata :one
INSERT INTO electricity_purchase_metadata (
    transaction_id,
    amount,
    points_used,
    amount_paid,
    token,
    customer_name,
    customer_address,
    units,
    meter_number,
    tax,
    debt,
    points_earned,
    phone_number,
    reference,
    request_id,
    service_charge,
    status
) VALUES (
    $1,  -- transaction_id
    $2,  -- amount
    $3,  -- points_used
    $4,  -- amount_paid
    $5,  -- token
    $6,  -- customer_name
    $7,  -- customer_address
    $8,  -- units
    $9,  -- meter_number
    $10, -- tax
    $11, -- debt
    $12, -- points_earned
    $13, -- phone_number
    $14, -- reference
    $15, -- request_id
    $16, -- service_charge
    $17  -- status
)
RETURNING *;

-- name: UpdateElectricityPurchasePartial :one
UPDATE electricity_purchase_metadata
SET
    token = COALESCE(token, $2),
    customer_name = COALESCE(customer_name, $3),
    customer_address = COALESCE(customer_address, $4),
    units = COALESCE(units, $5),
    meter_number = COALESCE(meter_number, $6),
    tax = COALESCE(tax, $7),
    debt = COALESCE(debt, $8),
    service_charge = COALESCE(service_charge, $9),
    status = $10
WHERE reference = $1
RETURNING *;

-- name: UpdateElectricityPurchaseStatus :one
UPDATE electricity_purchase_metadata
SET status = $2
WHERE id = $1
RETURNING *;

-- name: CreateWalletTransferMetadata :one
INSERT INTO wallet_transfer_metadata (
    currency,
    transaction_id,
    sender,
    type,
    recipient,
    service_charge,
    amount,
    amount_paid,
    bonus_earned,
    reference,
    status,
    description
)
VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12
)
RETURNING *;

-- name: UpdateWalletTransferMetadataStatus :exec
UPDATE wallet_transfer_metadata
SET status = $2
WHERE id = $1;

-- name: GetPendingDataAirtimePurchaseMetadataOlderThan20Seconds :many
SELECT *
FROM data_airtime_purchase_metadata
WHERE status = 'pending'
  AND date < NOW() - INTERVAL '20 seconds'
ORDER BY date ASC;

-- name: GetPendingElectricityPurchaseMetadataOlderThan20Seconds :many
SELECT * FROM electricity_purchase_metadata
WHERE status = 'pending'
  AND date < NOW() - INTERVAL '20 seconds'
ORDER BY date ASC;

-- name: UpdateTransactionAmountUSD :exec
UPDATE transactions SET amount_usd = $2 WHERE id = $1;

-- name: UpdateCryptoMetadataFinalAmounts :exec
UPDATE crypto_transaction_metadata
SET rate = $2, received_amount = $3
WHERE transaction_id = $1;

-- name: GetPendingBankTransferMetadataOlderThan20Seconds :many
SELECT * FROM bank_transfer_metadata
WHERE status = 'pending'
  AND date < NOW() - INTERVAL '20 seconds'
ORDER BY date ASC;

-- name: ListRapidRampTransactions :many
SELECT *
FROM transactions
WHERE type = 'rapid_ramp'
ORDER BY created_at DESC;