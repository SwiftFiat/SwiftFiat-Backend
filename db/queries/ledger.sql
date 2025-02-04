-- name: CreateWalletLedgerEntry :one
INSERT INTO ledger_entries (
    transaction_id,
    wallet_id,
    type,
    amount,
    balance,
    source_type,
    destination_type
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetWalletLedger :many
SELECT * FROM ledger_entries
WHERE wallet_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;