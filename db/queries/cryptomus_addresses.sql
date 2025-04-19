-- name: UpsertCryptomusAddress :one
INSERT INTO cryptomus_addresses (
    customer_id,
    wallet_uuid,
    uuid,
    address,
    network,
    currency,
    payment_url,
    callback_url,
    status,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    ON CONFLICT (address) DO UPDATE SET
    customer_id = EXCLUDED.customer_id,
                                 wallet_uuid = EXCLUDED.wallet_uuid,
                                 uuid = EXCLUDED.uuid,
                                 address = EXCLUDED.address,
                                 network = EXCLUDED.network,
                                 currency = EXCLUDED.currency,
                                 payment_url = EXCLUDED.payment_url,
                                 callback_url = EXCLUDED.callback_url,
                                 status = EXCLUDED.status,
                                 updated_at = CURRENT_TIMESTAMP
                                 RETURNING *;

-- name: GetCryptomusAddressByAddress :one
SELECT * FROM cryptomus_addresses
WHERE address = $1 LIMIT 1;

-- name: GetCryptomusAddressByUUID :one
SELECT * FROM cryptomus_addresses
WHERE uuid = $1 LIMIT 1;

-- name: ListCryptomusAddressesByCustomer :many
SELECT * FROM cryptomus_addresses
WHERE customer_id = $1
ORDER BY created_at DESC;

-- name: ListCryptomusAddressesByNetwork :many
SELECT * FROM cryptomus_addresses
WHERE network = $1
ORDER BY created_at DESC;

-- name: ListCryptomusAddressesByCurrency :many
SELECT * FROM cryptomus_addresses
WHERE currency = $1
ORDER BY created_at DESC;

-- name: GetCryptomusAddressByNetworkAndCurrencyAndCustomerID :one
SELECT * FROM cryptomus_addresses
WHERE LOWER(network) = LOWER(sqlc.narg(network)) AND LOWER(currency) = LOWER(sqlc.narg(currency)) AND customer_id = sqlc.narg(customer_id)
    LIMIT 1;

-- name: ListCryptomusAddressesByNetworkAndCurrency :many
SELECT * FROM cryptomus_addresses
WHERE network = $1 AND currency = $2
ORDER BY created_at DESC;

-- name: UpdateCryptomusAddressStatus :one
UPDATE cryptomus_addresses
SET
    status = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE address = $1
    RETURNING *;

-- name: DeleteCryptomusAddress :exec
DELETE FROM cryptomus_addresses
WHERE address = $1;