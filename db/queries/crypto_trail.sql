-- name: CreateCryptoTransactionTrail :one
INSERT INTO crypto_transaction_trail (address_id, transaction_hash, amount)
VALUES ($1, $2, $3)
RETURNING *;

-- name: FetchCryptoTransactionTrailByAddressID :many
SELECT *
FROM crypto_transaction_trail
WHERE address_id = $1;

-- name: FetchCryptoTransactionTrailByTransactionHash :one
SELECT *
FROM crypto_transaction_trail
WHERE transaction_hash = $1;

-- name: CheckCryptoTransactionTrailByTransactionHash :one
SELECT EXISTS (
    SELECT 1
    FROM crypto_transaction_trail
    WHERE transaction_hash = $1
) AS exists;

-- name: UpdateCryptoTransactionTrailAmountByTransactionHash :one
UPDATE crypto_transaction_trail
SET amount = amount + $2,
    updated_at = NOW()
WHERE transaction_hash = $1
RETURNING *;

-- name: DeleteCryptoTransactionTrailByTransactionHash :one
DELETE FROM crypto_transaction_trail
WHERE transaction_hash = $1
RETURNING *;
