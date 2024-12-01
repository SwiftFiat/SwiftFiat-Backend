-- name: CreateWallet :one
INSERT INTO swift_wallets (
    customer_id,
    type,
    currency,
    balance
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetWallet :one
SELECT * FROM swift_wallets
WHERE id = $1 LIMIT 1;

-- name: GetWalletByCustomerID :many
SELECT * FROM swift_wallets
WHERE customer_id = $1;

-- name: GetWalletByCurrency :one
SELECT * FROM swift_wallets
WHERE customer_id = $1 AND currency = $2 LIMIT 1;

-- name: GetWalletByCurrencyForUpdate :one
SELECT * FROM swift_wallets
WHERE customer_id = $1 AND currency = $2 LIMIT 1
FOR UPDATE;

-- name: GetWalletForUpdate :one
SELECT * FROM swift_wallets
WHERE id = $1 LIMIT 1
FOR UPDATE;

-- name: ListWallets :many
SELECT * FROM swift_wallets
WHERE customer_id = $1
ORDER BY created_at;

-- name: UpdateWalletBalance :one
UPDATE swift_wallets 
SET balance = balance + sqlc.arg(amount)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateWalletTransaction :one
INSERT INTO transactions (
    type,
    amount, 
    currency,
    currency_flow,
    from_account_id,
    to_account_id,
    description
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: CreateWalletCryptoTransaction :one
INSERT INTO transactions (
    type,
    amount, 
    coin,
    source_hash,
    to_account_id,
    currency,
    currency_flow,
    description
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: GetWalletTransaction :one
SELECT t.*, w.id as wallet_id
FROM transactions t
JOIN swift_wallets w ON (t.from_account_id = w.id OR t.to_account_id = w.id)
WHERE t.id = $1 
  AND w.customer_id = $2
LIMIT 1;

-- name: ListWalletTransactions :many
SELECT t.*
FROM transactions t
JOIN swift_wallets w ON (t.from_account_id = w.id OR t.to_account_id = w.id)
WHERE 
    (t.from_account_id = $1 OR t.to_account_id = $1)
    AND w.customer_id = $2
ORDER BY t.created_at DESC
LIMIT $3
OFFSET $4;

-- name: UpdateWalletTransactionStatus :one
UPDATE transactions
SET status = $2
WHERE id = $1
RETURNING *;

-- name: CreateWalletLedgerEntry :one
INSERT INTO ledger_entries (
    transaction_id,
    account_id,
    type,
    amount,
    balance
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetWalletLedger :many
SELECT * FROM ledger_entries
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2
OFFSET $3;

-- name: ListWalletTransactionsByUserID :one
WITH transaction_data AS (
    SELECT t.*
    FROM transactions t
    JOIN swift_wallets w ON (t.from_account_id = w.id OR t.to_account_id = w.id)
    WHERE 
        w.customer_id = $1
        AND (
            -- If cursor is provided, apply the cursor-based filtering
            -- If no cursor, use the default query to fetch the first results
            (sqlc.narg(transaction_created)::timestamptz IS NULL OR (t.created_at::timestamptz, t.id) < (
                sqlc.narg(transaction_created)::timestamptz,
                sqlc.narg(transaction_id)::uuid
            ))
        )
    ORDER BY t.created_at DESC, t.id DESC
    LIMIT COALESCE(sqlc.arg(page_limit), 25)
),
last_transaction AS (
    SELECT created_at, id
    FROM transaction_data
    ORDER BY created_at ASC, id ASC
    LIMIT 1
),
has_more_check AS (
    SELECT EXISTS (
        SELECT 1 
        FROM transactions t2
        JOIN swift_wallets w2 ON (t2.from_account_id = w2.id OR t2.to_account_id = w2.id)
        WHERE 
            w2.customer_id = $1
            AND (t2.created_at::timestamptz, t2.id::uuid) < (SELECT created_at::timestamptz, id::uuid FROM last_transaction)
    ) AS has_more
)
SELECT 
    jsonb_build_object(
        'transactions', (SELECT jsonb_agg(td.*) FROM transaction_data td),
        'metadata', jsonb_build_object(
            'has_more', (SELECT has_more FROM has_more_check),
            'next_cursor', (SELECT concat(created_at::timestamptz, '_', id) FROM last_transaction)
        )
    ) AS result;
