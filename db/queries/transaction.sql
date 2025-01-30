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