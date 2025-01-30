-- name: CreateTransactionFee :one
INSERT INTO transaction_fees (
    transaction_type,
    fee_percentage,
    max_fee,
    flat_fee,
    effective_time,
    source
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: GetLatestTransactionFee :one
SELECT * FROM transaction_fees
WHERE transaction_type = $1
ORDER BY effective_time DESC
LIMIT 1;

-- name: GetTransactionFeeAtTime :one
SELECT * FROM transaction_fees
WHERE transaction_type = $1
AND effective_time <= $2
ORDER BY effective_time DESC
LIMIT 1;

-- name: ListTransactionFees :many
SELECT * FROM transaction_fees
WHERE transaction_type = $1
AND effective_time BETWEEN $2 AND $3
ORDER BY effective_time DESC;

-- name: ListLatestTransactionFees :many
SELECT DISTINCT ON (transaction_type)
    transaction_type,
    fee_percentage,
    max_fee,
    flat_fee,
    effective_time,
    source
FROM transaction_fees
WHERE effective_time > '1900-01-01'
ORDER BY transaction_type, effective_time DESC;

-- name: DeleteOldTransactionFees :exec
DELETE FROM transaction_fees 
WHERE effective_time < $1;
