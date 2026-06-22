-- name: CreateCryptomusWebhook :one
INSERT INTO cryptomus_webhooks (
    signature,
    order_id,
    payload,
    source_ip,
    status
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetCryptomusWebhookBySignature :one
SELECT * FROM cryptomus_webhooks
WHERE signature = $1 LIMIT 1;

-- name: GetCryptomusWebhookByID :one
SELECT * FROM cryptomus_webhooks
WHERE id = $1 LIMIT 1;

-- name: GetCryptomusWebhookByOrderID :one
SELECT * FROM cryptomus_webhooks
WHERE order_id = $1 
ORDER BY received_at DESC 
LIMIT 1;

-- name: UpdateCryptomusWebhookStatus :one
UPDATE cryptomus_webhooks
SET 
    status = $2,
    processed_at = CASE WHEN $2 = 'processed' THEN CURRENT_TIMESTAMP ELSE processed_at END,
    processed_transaction_id = COALESCE($3, processed_transaction_id),
    processing_error = COALESCE($4, processing_error),
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: IncrementCryptomusWebhookRetryCount :one
UPDATE cryptomus_webhooks
SET 
    retry_count = retry_count + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: CreateWebhookReplay :one
INSERT INTO webhook_replays (
    webhook_id,
    replayed_by,
    reason,
    result
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: UpdateWebhookReplayResult :one
UPDATE webhook_replays
SET 
    result = $2,
    error_message = $3
WHERE id = $1
RETURNING *;

-- name: ListCryptomusWebhooks :many
SELECT * FROM cryptomus_webhooks
WHERE ($1::text = '' OR status = $1)
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- name: CountCryptomusWebhooks :one
SELECT COUNT(*) FROM cryptomus_webhooks
WHERE ($1::text = '' OR status = $1);
