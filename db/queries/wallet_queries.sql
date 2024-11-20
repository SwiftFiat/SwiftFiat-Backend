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
SET balance = $2
WHERE id = $1
RETURNING *;

-- name: CreateWalletTransaction :one
INSERT INTO transactions (
    type,
    amount, 
    currency,
    from_account_id,
    to_account_id,
    description
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: GetWalletTransaction :one
SELECT * FROM transactions
WHERE id = $1 LIMIT 1;

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

-- name: ListWalletTransactionsByUserID :many
SELECT t.*
FROM transactions t
JOIN swift_wallets w ON (t.from_account_id = w.id OR t.to_account_id = w.id)
WHERE 
    w.customer_id = $1
ORDER BY t.created_at DESC
LIMIT $2
OFFSET $3;
