-- name: CreateTransaction :one
INSERT INTO transactions (
    type,
    description,
    transaction_flow,
    status
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

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
    service_transaction_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
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

-- name: CreateFiatWithdrawalMetadata :one
INSERT INTO fiat_withdrawal_metadata (
    source_wallet,
    transaction_id,
    rate,
    received_amount,
    sent_amount,
    fees,
    account_name,
    bank_code,
    account_number,
    service_provider,
    service_transaction_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

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
    service_transaction_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: GetTransactionByID :one
SELECT 
    t.*,
    COALESCE(st.source_wallet, ct.destination_wallet, gt.source_wallet, fw.source_wallet, sm.source_wallet) as source_wallet,
    COALESCE(st.destination_wallet, ct.destination_wallet) as destination_wallet,
    COALESCE(st.currency, ct.coin) as currency,
    COALESCE(st.rate, ct.rate, gt.rate, fw.rate, sm.rate) as rate,
    COALESCE(st.fees, ct.fees, gt.fees, fw.fees, sm.fees) as fees,
    COALESCE(st.received_amount, ct.received_amount, gt.received_amount, fw.received_amount, sm.received_amount) as received_amount,
    COALESCE(st.sent_amount, ct.sent_amount, gt.sent_amount, fw.sent_amount, sm.sent_amount) as sent_amount
FROM transactions t
LEFT JOIN swap_transfer_metadata st ON t.id = st.transaction_id
LEFT JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
LEFT JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
LEFT JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
WHERE t.id = $1 LIMIT 1;

-- name: GetTransactionByIDForUpdate :one
SELECT * FROM transactions
WHERE id = $1 LIMIT 1
FOR UPDATE;

-- name: GetTransactionsByWallet :many
SELECT 
    t.*,
    CASE 
        WHEN st.source_wallet = sqlc.arg(wallet_id) THEN 'source'
        ELSE 'destination'
    END as wallet_role,
    COALESCE(st.currency, ct.coin) as currency,
    COALESCE(st.rate, ct.rate, gt.rate, fw.rate, sm.rate) as rate,
    COALESCE(st.fees, ct.fees, gt.fees, fw.fees, sm.fees) as fees,
    COALESCE(st.received_amount, ct.received_amount, gt.received_amount, fw.received_amount, sm.received_amount) as received_amount,
    COALESCE(st.sent_amount, ct.sent_amount, gt.sent_amount, fw.sent_amount, sm.sent_amount) as sent_amount
FROM transactions t
LEFT JOIN swap_transfer_metadata st ON t.id = st.transaction_id
LEFT JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
LEFT JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
LEFT JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
WHERE st.source_wallet = sqlc.arg(wallet_id) 
   OR st.destination_wallet = sqlc.arg(wallet_id)
   OR ct.destination_wallet = sqlc.arg(wallet_id)
   OR gt.source_wallet = sqlc.arg(wallet_id)
   OR fw.source_wallet = sqlc.arg(wallet_id)
   OR sm.source_wallet = sqlc.arg(wallet_id)
ORDER BY t.created_at DESC
LIMIT sqlc.arg(_limit) OFFSET sqlc.arg(_offset);

-- name: GetTransactionsByDateRange :many
SELECT 
    t.*,
    COALESCE(st.currency, ct.coin) as currency,
    COALESCE(st.rate, ct.rate, gt.rate, fw.rate, sm.rate) as rate,
    COALESCE(st.received_amount, ct.received_amount, gt.received_amount, fw.received_amount, sm.received_amount) as received_amount,
    COALESCE(st.sent_amount, ct.sent_amount, gt.sent_amount, fw.sent_amount, sm.sent_amount) as sent_amount
FROM transactions t
LEFT JOIN swap_transfer_metadata st ON t.id = st.transaction_id
LEFT JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
LEFT JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
LEFT JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
WHERE t.created_at BETWEEN sqlc.arg(start_date) AND sqlc.arg(end_date)
AND (sqlc.arg(transaction_type)::text IS NULL OR t.type = sqlc.arg(transaction_type))
ORDER BY t.created_at DESC
LIMIT sqlc.arg(_limit) OFFSET sqlc.arg(_offset);

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
    END as metadata
FROM transactions t
LEFT JOIN swap_transfer_metadata st ON t.id = st.transaction_id
LEFT JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
LEFT JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
LEFT JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
WHERE t.id = $1 LIMIT 1;

-- name: GetTransactionsByUserID :many
WITH user_wallets AS (
    -- If user_id is provided, get all their wallets
    SELECT id as wallet_id 
    FROM swift_wallets
    WHERE CASE 
        WHEN sqlc.narg(user_id)::bigint IS NOT NULL THEN customer_id = sqlc.narg(user_id)::bigint
        ELSE id = ANY(sqlc.arg(wallet_ids)::uuid[])
    END
),
wallet_transactions AS (
    -- Get transactions from swap_transfer_metadata where wallet is source or destination
    SELECT t.*, 'swap_transfer' as metadata_type, to_jsonb(st.*) as metadata
    FROM transactions t
    JOIN swap_transfer_metadata st ON t.id = st.transaction_id 
    JOIN user_wallets uw ON st.source_wallet = uw.wallet_id OR st.destination_wallet = uw.wallet_id

    UNION ALL

    -- Get transactions from crypto_transaction_metadata where wallet is destination
    SELECT t.*, 'crypto' as metadata_type, to_jsonb(ct.*) as metadata
    FROM transactions t
    JOIN crypto_transaction_metadata ct ON t.id = ct.transaction_id
    JOIN user_wallets uw ON ct.destination_wallet = uw.wallet_id

    UNION ALL

    -- Get transactions from giftcard_transaction_metadata where wallet is source
    SELECT t.*, 'giftcard' as metadata_type, to_jsonb(gt.*) as metadata
    FROM transactions t
    JOIN giftcard_transaction_metadata gt ON t.id = gt.transaction_id
    JOIN user_wallets uw ON gt.source_wallet = uw.wallet_id

    UNION ALL

    -- Get transactions from fiat_withdrawal_metadata where wallet is source
    SELECT t.*, 'withdrawal' as metadata_type, to_jsonb(fw.*) as metadata
    FROM transactions t
    JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
    JOIN user_wallets uw ON fw.source_wallet = uw.wallet_id

    UNION ALL

    -- Get transactions from services_metadata where wallet is source
    SELECT t.*, 'service' as metadata_type, to_jsonb(sm.*) as metadata
    FROM transactions t
    JOIN services_metadata sm ON t.id = sm.transaction_id
    JOIN user_wallets uw ON sm.source_wallet = uw.wallet_id
)
SELECT 
    t.id,
    t.type,
    t.description,
    t.transaction_flow,
    t.status,
    t.created_at,
    t.updated_at,
    jsonb_build_object(
        'type', t.metadata_type,
        'data', t.metadata
    ) as metadata
FROM wallet_transactions t
WHERE CASE 
    WHEN sqlc.narg(created_at)::timestamptz IS NOT NULL THEN t.created_at < sqlc.narg(created_at)::timestamptz
    ELSE true
END
AND CASE
    WHEN sqlc.narg(transaction_id)::uuid IS NOT NULL THEN t.id < sqlc.narg(transaction_id)::uuid
    ELSE true
END
ORDER BY t.created_at DESC, t.id DESC
LIMIT sqlc.arg(_limit);

-- name: GetTransactionsForWallet :one
WITH pagination AS (
    SELECT sqlc.arg(_limit)::int as page_limit,
           sqlc.arg(_offset)::int as page_offset
),
matching_transactions AS (
    SELECT cm.transaction_id FROM public.crypto_transaction_metadata cm
    WHERE cm.destination_wallet = sqlc.arg(usd_wallet_id) OR cm.destination_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT fm.transaction_id FROM public.fiat_withdrawal_metadata fm
    WHERE fm.source_wallet = sqlc.arg(usd_wallet_id) OR fm.source_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT gm.transaction_id FROM public.giftcard_transaction_metadata gm
    WHERE gm.source_wallet = sqlc.arg(usd_wallet_id) OR gm.source_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT sm.transaction_id FROM public.services_metadata sm
    WHERE sm.source_wallet = sqlc.arg(usd_wallet_id) OR sm.source_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT stm.transaction_id FROM public.swap_transfer_metadata stm
    WHERE stm.source_wallet = sqlc.arg(usd_wallet_id) OR stm.source_wallet = sqlc.arg(ngn_wallet_id)
    OR stm.destination_wallet = sqlc.arg(usd_wallet_id) OR stm.destination_wallet = sqlc.arg(ngn_wallet_id)
),
total_count AS (
    SELECT COUNT(*) as total FROM matching_transactions
),
transaction_data AS (
    SELECT 
        t.*,
        CASE 
            WHEN t.type = 'deposit' THEN (
                SELECT jsonb_build_object(
                    'destination_wallet', cm.destination_wallet,
                    'coin', cm.coin,
                    'rate', cm.rate,
                    'fees', cm.fees,
                    'received_amount', cm.received_amount,
                    'sent_amount', cm.sent_amount,
                    'service_provider', cm.service_provider,
                    'service_transaction_id', cm.service_transaction_id
                )::jsonb
                FROM public.crypto_transaction_metadata cm
                WHERE cm.transaction_id = t.id
            )
            WHEN t.type = 'withdrawal' THEN (
                SELECT jsonb_build_object(
                    'source_wallet', fm.source_wallet,
                    'rate', fm.rate,
                    'received_amount', fm.received_amount,
                    'sent_amount', fm.sent_amount,
                    'fees', fm.fees,
                    'account_name', fm.account_name,
                    'bank_code', fm.bank_code,
                    'account_number', fm.account_number,
                    'service_provider', fm.service_provider,
                    'service_transaction_id', fm.service_transaction_id
                )::jsonb
                FROM public.fiat_withdrawal_metadata fm
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
            WHEN t.type IN ('airtime', 'data', 'tv', 'electricity') THEN (
                SELECT jsonb_build_object(
                    'source_wallet', sm.source_wallet,
                    'rate', sm.rate,
                    'received_amount', sm.received_amount,
                    'sent_amount', sm.sent_amount,
                    'fees', sm.fees,
                    'service_type', sm.service_type,
                    'service_provider', sm.service_provider,
                    'service_id', sm.service_id,
                    'service_status', sm.service_status,
                    'service_transaction_id', sm.service_transaction_id
                )::jsonb
                FROM public.services_metadata sm
                WHERE sm.transaction_id = t.id
            )
            WHEN t.type IN ('transfer', 'swap') THEN (
                SELECT jsonb_build_object(
                    'currency', stm.currency,
                    'transfer_type', stm.transfer_type,
                    'description', stm.description,
                    'source_wallet', stm.source_wallet,
                    'destination_wallet', stm.destination_wallet,
                    'user_tag', stm.user_tag,
                    'rate', stm.rate,
                    'fees', stm.fees,
                    'received_amount', stm.received_amount,
                    'sent_amount', stm.sent_amount
                )::jsonb
                FROM public.swap_transfer_metadata stm
                WHERE stm.transaction_id = t.id
            )
        END as metadata
    FROM matching_transactions mt
    JOIN public.transactions t ON t.id = mt.transaction_id
    ORDER BY t.created_at DESC
    LIMIT (SELECT page_limit FROM pagination)
    OFFSET (SELECT page_offset FROM pagination)
)
SELECT 
    jsonb_build_object(
        'transactions', jsonb_agg(to_jsonb(transaction_data.*)),
        'page_limit', (SELECT page_limit FROM pagination),
        'page_offset', (SELECT page_offset FROM pagination),
        'total_count', (SELECT total FROM total_count),
        'has_more', (SELECT (page_offset + page_limit) < total FROM pagination, total_count)
    ) as result
FROM transaction_data;

-- name: GetTransactionsForWalletCursor :one
WITH pagination AS (
    SELECT sqlc.arg(_limit)::int as page_limit
),
matching_transactions AS (
    SELECT cm.transaction_id FROM public.crypto_transaction_metadata cm
    WHERE cm.destination_wallet = sqlc.arg(usd_wallet_id) OR cm.destination_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT fm.transaction_id FROM public.fiat_withdrawal_metadata fm
    WHERE fm.source_wallet = sqlc.arg(usd_wallet_id) OR fm.source_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT gm.transaction_id FROM public.giftcard_transaction_metadata gm
    WHERE gm.source_wallet = sqlc.arg(usd_wallet_id) OR gm.source_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT sm.transaction_id FROM public.services_metadata sm
    WHERE sm.source_wallet = sqlc.arg(usd_wallet_id) OR sm.source_wallet = sqlc.arg(ngn_wallet_id)
    UNION ALL
    SELECT stm.transaction_id FROM public.swap_transfer_metadata stm
    WHERE stm.source_wallet = sqlc.arg(usd_wallet_id) OR stm.source_wallet = sqlc.arg(ngn_wallet_id)
    OR stm.destination_wallet = sqlc.arg(usd_wallet_id) OR stm.destination_wallet = sqlc.arg(ngn_wallet_id)
),
transaction_data AS (
    SELECT 
        t.*,
        CASE 
            WHEN t.type = 'deposit' THEN (
                SELECT jsonb_build_object(
                    'destination_wallet', cm.destination_wallet,
                    'coin', cm.coin,
                    'rate', cm.rate,
                    'fees', cm.fees,
                    'received_amount', cm.received_amount,
                    'sent_amount', cm.sent_amount,
                    'service_provider', cm.service_provider,
                    'service_transaction_id', cm.service_transaction_id
                )::jsonb
                FROM public.crypto_transaction_metadata cm
                WHERE cm.transaction_id = t.id
            )
            WHEN t.type = 'withdrawal' THEN (
                SELECT jsonb_build_object(
                    'source_wallet', fm.source_wallet,
                    'rate', fm.rate,
                    'received_amount', fm.received_amount,
                    'sent_amount', fm.sent_amount,
                    'fees', fm.fees,
                    'account_name', fm.account_name,
                    'bank_code', fm.bank_code,
                    'account_number', fm.account_number,
                    'service_provider', fm.service_provider,
                    'service_transaction_id', fm.service_transaction_id
                )::jsonb
                FROM public.fiat_withdrawal_metadata fm
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
            WHEN t.type IN ('airtime', 'data', 'tv', 'electricity') THEN (
                SELECT jsonb_build_object(
                    'source_wallet', sm.source_wallet,
                    'rate', sm.rate,
                    'received_amount', sm.received_amount,
                    'sent_amount', sm.sent_amount,
                    'fees', sm.fees,
                    'service_type', sm.service_type,
                    'service_provider', sm.service_provider,
                    'service_id', sm.service_id,
                    'service_status', sm.service_status,
                    'service_transaction_id', sm.service_transaction_id
                )::jsonb
                FROM public.services_metadata sm
                WHERE sm.transaction_id = t.id
            )
            WHEN t.type IN ('transfer', 'swap') THEN (
                SELECT jsonb_build_object(
                    'currency', stm.currency,
                    'transfer_type', stm.transfer_type,
                    'description', stm.description,
                    'source_wallet', stm.source_wallet,
                    'destination_wallet', stm.destination_wallet,
                    'user_tag', stm.user_tag,
                    'rate', stm.rate,
                    'fees', stm.fees,
                    'received_amount', stm.received_amount,
                    'sent_amount', stm.sent_amount
                )::jsonb
                FROM public.swap_transfer_metadata stm
                WHERE stm.transaction_id = t.id
            )
        END as metadata
    FROM matching_transactions mt
    JOIN public.transactions t ON t.id = mt.transaction_id
    WHERE CASE 
        WHEN sqlc.narg(created_at)::timestamptz IS NOT NULL THEN t.created_at < sqlc.narg(created_at)::timestamptz
        ELSE true
    END
    AND CASE
        WHEN sqlc.narg(transaction_id)::uuid IS NOT NULL THEN t.id < sqlc.narg(transaction_id)::uuid
        ELSE true
    END
    ORDER BY t.created_at DESC, t.id DESC
    LIMIT (SELECT page_limit FROM pagination) + 1
),
result_set AS (
    SELECT * FROM transaction_data
    LIMIT (SELECT page_limit FROM pagination)
)
SELECT 
    jsonb_build_object(
        'transactions', jsonb_agg(to_jsonb(result_set.*)),
        'has_more', (SELECT COUNT(*) FROM transaction_data) > (SELECT page_limit FROM pagination),
        'next_cursor', CASE 
            WHEN (SELECT COUNT(*) FROM transaction_data) > (SELECT page_limit FROM pagination) THEN
                jsonb_build_object(
                    'created_at', (SELECT created_at FROM result_set ORDER BY created_at ASC, id ASC LIMIT 1),
                    'transaction_id', (SELECT id FROM result_set ORDER BY created_at ASC, id ASC LIMIT 1)
                )
            ELSE NULL
        END
    ) as result
FROM result_set;

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
                WHEN t.type = 'deposit' THEN (
                    SELECT jsonb_build_object(
                        'destination_wallet', cm.destination_wallet,
                        'coin', cm.coin,
                        'rate', cm.rate,
                        'fees', cm.fees,
                        'received_amount', cm.received_amount,
                        'sent_amount', cm.sent_amount,
                        'service_provider', cm.service_provider,
                        'service_transaction_id', cm.service_transaction_id
                    )::jsonb
                    FROM public.crypto_transaction_metadata cm
                    WHERE cm.transaction_id = t.id
                )
                WHEN t.type = 'withdrawal' THEN (
                    SELECT jsonb_build_object(
                        'source_wallet', fm.source_wallet,
                        'rate', fm.rate,
                        'received_amount', fm.received_amount,
                        'sent_amount', fm.sent_amount,
                        'fees', fm.fees,
                        'account_name', fm.account_name,
                        'bank_code', fm.bank_code,
                        'account_number', fm.account_number,
                        'service_provider', fm.service_provider,
                        'service_transaction_id', fm.service_transaction_id
                    )::jsonb
                    FROM public.fiat_withdrawal_metadata fm
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
                WHEN t.type IN ('airtime', 'data', 'tv', 'electricity') THEN (
                    SELECT jsonb_build_object(
                        'source_wallet', sm.source_wallet,
                        'rate', sm.rate,
                        'received_amount', sm.received_amount,
                        'sent_amount', sm.sent_amount,
                        'fees', sm.fees,
                        'service_type', sm.service_type,
                        'service_provider', sm.service_provider,
                        'service_id', sm.service_id,
                        'service_status', sm.service_status,
                        'service_transaction_id', sm.service_transaction_id
                    )::jsonb
                    FROM public.services_metadata sm
                    WHERE sm.transaction_id = t.id
                )
                WHEN t.type IN ('transfer', 'swap') THEN (
                    SELECT jsonb_build_object(
                        'currency', stm.currency,
                        'transfer_type', stm.transfer_type,
                        'description', stm.description,
                        'source_wallet', stm.source_wallet,
                        'destination_wallet', stm.destination_wallet,
                        'user_tag', stm.user_tag,
                        'rate', stm.rate,
                        'fees', stm.fees,
                        'received_amount', stm.received_amount,
                        'sent_amount', stm.sent_amount
                    )::jsonb
                    FROM public.swap_transfer_metadata stm
                    WHERE stm.transaction_id = t.id
                )
            END
        )
    ) as result
FROM public.transactions t
WHERE t.id = sqlc.arg(transaction_id)
LIMIT 1;