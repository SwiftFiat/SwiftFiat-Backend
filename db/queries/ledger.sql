-- name: InsertLedgerEntry :one
INSERT INTO ledger_entries (
    transaction_id,
    wallet_id,
    entry_type,
    amount,
    source_type,
    destination_type
)
VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, transaction_id, wallet_id, entry_type, amount, created_at;


-- name: GetWalletLedger :many
SELECT * FROM ledger_entries
WHERE wallet_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetWalletBalanceFromLedger :one
SELECT
    COALESCE(
        SUM(
            CASE
                WHEN type = 'credit' THEN amount
                WHEN type = 'debit'  THEN -amount
            END
        ),
        0
    ) AS balance
FROM ledger_entries
WHERE wallet_id = $1;
