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
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
LEFT JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
LEFT JOIN vault_transactions vt ON t.id = vt.transaction_id
LEFT JOIN conversion_history ch ON t.id = ch.transaction_id
LEFT JOIN qr_transactions qr ON t.id = qr.transaction_id
LEFT JOIN reward_transactions rt ON t.id = rt.transaction_id
LEFT JOIN card_transactions ct ON t.id = ct.transaction_id
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
LEFT JOIN fiat_withdrawal_metadata fw ON t.id = fw.transaction_id
LEFT JOIN services_metadata sm ON t.id = sm.transaction_id
LEFT JOIN vault_transactions vt ON t.id = vt.transaction_id
LEFT JOIN conversion_history ch ON t.id = ch.transaction_id
LEFT JOIN qr_transactions qr ON t.id = qr.transaction_id
LEFT JOIN reward_transactions rt ON t.id = rt.transaction_id
LEFT JOIN card_transactions ct ON t.id = ct.transaction_id
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
    WHERE cm.destination_wallet = sqlc.arg(usd_wallet_id) 
       OR cm.destination_wallet = sqlc.arg(ngn_wallet_id)
       OR cm.destination_wallet = sqlc.arg(usdc_wallet_id)
       OR cm.destination_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT fm.transaction_id FROM public.fiat_withdrawal_metadata fm
    WHERE fm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR fm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR fm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR fm.source_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT gm.transaction_id FROM public.giftcard_transaction_metadata gm
    WHERE gm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR gm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR gm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR gm.source_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT sm.transaction_id FROM public.services_metadata sm
    WHERE sm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR sm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR sm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR sm.source_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT stm.transaction_id FROM public.swap_transfer_metadata stm
    WHERE stm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR stm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR stm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR stm.source_wallet = sqlc.arg(usdt_wallet_id)
       OR stm.destination_wallet = sqlc.arg(usd_wallet_id) 
       OR stm.destination_wallet = sqlc.arg(ngn_wallet_id)
       OR stm.destination_wallet = sqlc.arg(usdc_wallet_id)
       OR stm.destination_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    -- Add vault transactions: match transactions with transaction_flow = 'Vault' 
    -- that have corresponding vault_transactions with matching wallet IDs
    SELECT t.id as transaction_id FROM public.transactions t
    INNER JOIN public.vault_transactions vt ON vt.transaction_id = t.id
    WHERE t.transaction_flow IN ('wallet -> savings', 'savings -> wallet')
        AND (
            (vt.source_wallet IS NOT NULL AND (
                vt.source_wallet = sqlc.arg(usd_wallet_id) 
                OR vt.source_wallet = sqlc.arg(ngn_wallet_id)
                OR vt.source_wallet = sqlc.arg(usdc_wallet_id)
                OR vt.source_wallet = sqlc.arg(usdt_wallet_id)
            ))
            OR (vt.destination_wallet IS NOT NULL AND (
                vt.destination_wallet = sqlc.arg(usd_wallet_id) 
                OR vt.destination_wallet = sqlc.arg(ngn_wallet_id)
                OR vt.destination_wallet = sqlc.arg(usdc_wallet_id)
                OR vt.destination_wallet = sqlc.arg(usdt_wallet_id)
            ))
        )
    ),
total_count AS (
    SELECT COUNT(*) as total FROM matching_transactions
),
transaction_data AS (
    SELECT
        t.id, t.type, t.description, t.transaction_flow, t.status, t.created_at, t.updated_at, t.deleted_from_account_id, t.deleted_to_account_id,
        CASE
            WHEN t.transaction_flow IN ('wallet -> savings', 'savings -> wallet') THEN (
                -- Handle vault transactions
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
                WHERE ABS(EXTRACT(EPOCH FROM (t.created_at - vt.created_at))) < 5
                  AND (
                      (vt.source_wallet IS NOT NULL AND (
                          vt.source_wallet = sqlc.arg(usd_wallet_id) 
                          OR vt.source_wallet = sqlc.arg(ngn_wallet_id)
                          OR vt.source_wallet = sqlc.arg(usdc_wallet_id)
                          OR vt.source_wallet = sqlc.arg(usdt_wallet_id)
                      ))
                      OR (vt.destination_wallet IS NOT NULL AND (
                          vt.destination_wallet = sqlc.arg(usd_wallet_id) 
                          OR vt.destination_wallet = sqlc.arg(ngn_wallet_id)
                          OR vt.destination_wallet = sqlc.arg(usdc_wallet_id)
                          OR vt.destination_wallet = sqlc.arg(usdt_wallet_id)
                      ))
                  )
                ORDER BY ABS(EXTRACT(EPOCH FROM (t.created_at - vt.created_at)))
                LIMIT 1
            )
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
    WHERE cm.destination_wallet = sqlc.arg(usd_wallet_id) 
       OR cm.destination_wallet = sqlc.arg(ngn_wallet_id)
       OR cm.destination_wallet = sqlc.arg(usdc_wallet_id)
       OR cm.destination_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT fm.transaction_id FROM public.fiat_withdrawal_metadata fm
    WHERE fm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR fm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR fm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR fm.source_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT gm.transaction_id FROM public.giftcard_transaction_metadata gm
    WHERE gm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR gm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR gm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR gm.source_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT sm.transaction_id FROM public.services_metadata sm
    WHERE sm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR sm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR sm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR sm.source_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    SELECT stm.transaction_id FROM public.swap_transfer_metadata stm
    WHERE stm.source_wallet = sqlc.arg(usd_wallet_id) 
       OR stm.source_wallet = sqlc.arg(ngn_wallet_id)
       OR stm.source_wallet = sqlc.arg(usdc_wallet_id)
       OR stm.source_wallet = sqlc.arg(usdt_wallet_id)
       OR stm.destination_wallet = sqlc.arg(usd_wallet_id) 
       OR stm.destination_wallet = sqlc.arg(ngn_wallet_id)
       OR stm.destination_wallet = sqlc.arg(usdc_wallet_id)
       OR stm.destination_wallet = sqlc.arg(usdt_wallet_id)
    UNION ALL
    -- Add vault transactions
    SELECT t.id as transaction_id FROM public.transactions t
    INNER JOIN public.vault_transactions vt ON vt.transaction_id = t.id
    WHERE t.transaction_flow IN ('wallet -> savings', 'savings -> wallet')
        AND (
            (vt.source_wallet IS NOT NULL AND (
                vt.source_wallet = sqlc.arg(usd_wallet_id) 
                OR vt.source_wallet = sqlc.arg(ngn_wallet_id)
                OR vt.source_wallet = sqlc.arg(usdc_wallet_id)
                OR vt.source_wallet = sqlc.arg(usdt_wallet_id)
            ))
            OR (vt.destination_wallet IS NOT NULL AND (
                vt.destination_wallet = sqlc.arg(usd_wallet_id) 
                OR vt.destination_wallet = sqlc.arg(ngn_wallet_id)
                OR vt.destination_wallet = sqlc.arg(usdc_wallet_id)
                OR vt.destination_wallet = sqlc.arg(usdt_wallet_id)
            ))
        )
    ),
transaction_data AS (
    SELECT
        t.id, t.type, t.description, t.transaction_flow, t.status, t.created_at, t.updated_at, t.deleted_from_account_id, t.deleted_to_account_id,
        CASE
        WHEN t.transaction_flow IN ('wallet -> savings', 'savings -> wallet') THEN (
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
                WHEN t.type IN ('transfer') THEN (
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
                        'user_id', ch.user_id,
                        'source_currency', ch.source_currency,
                        'target_currency', ch.target_currency,
                        'source_amount', ch.source_amount,
                        'target_amount', ch.target_amount,
                        'source_wallet_id', ch.source_wallet_id,
                        'target_wallet_id', ch.target_wallet_id,
                        'fees', ch.fees,
                        'rate_provider', ch.rate_provider,
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
                        'reward_id', rt.id,
                        'transaction_type', rt.transaction_type,
                        'points_amount', rt.points_amount,
                        'naira_value', rt.naira_value,
                        'transaction_id', rt.transaction_id,
                        'source_transaction_type', rt.source_transaction_type,
                        'transaction_amount', rt.transaction_amount,
                        'balance_after', rt.balance_after,
                        'status', rt.status,
                        'description', rt.description,
                        'created_at', rt.created_at,
                        'updated_at', rt.updated_at
                    )::jsonb
                    FROM public.reward_transactions rt
                    WHERE rt.transaction_id = t.id
                )
                WHEN t.type IN ('crypto') THEN (
                    SELECT jsonb_build_object(
                        'destination_wallet', cm.destination_wallet,
                        'coin', cm.coin,
                        'rate', cm.rate,
                        'source_hash', cm.source_hash,
                        'fees', cm.fees,
                        'received_amount', cm.received_amount,
                        'sent_amount', cm.sent_amount,
                        'service_provider', cm.service_provider,
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
LEFT JOIN fiat_withdrawal_metadata fwm ON t.id = fwm.transaction_id
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
WHERE t.type IN ('swap', 'transfer', 'crypto', 'giftcard', 'withdrawal', 'service', 'reward', 'vault', 'qr_code', 'card', 'airtime', 'data', 'tv_subscription', 'utility_payment', 'electricity');


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