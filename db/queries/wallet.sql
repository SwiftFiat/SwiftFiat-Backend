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

-- name: GetWalletByTag :one
SELECT sw.id, sw.currency, sw.status ,u.id, u.first_name, u.last_name
FROM users u
JOIN swift_wallets sw ON u.id = sw.customer_id
WHERE u.user_tag = $1 AND sw.currency = $2;

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