-- Enable UUID generation (required for gen_random_uuid)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- =========================
-- Cryptomus Webhooks Table
-- =========================
CREATE TABLE cryptomus_webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    signature VARCHAR(255) NOT NULL UNIQUE, -- idempotency key
    order_id VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    source_ip INET NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'received',
    processing_error TEXT,
    processed_transaction_id UUID,
    retry_count INT DEFAULT 0,
    received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Indexes (Postgres requires separate CREATE INDEX statements)
CREATE INDEX idx_cryptomus_webhooks_signature
    ON cryptomus_webhooks(signature);

CREATE INDEX idx_cryptomus_webhooks_order_id
    ON cryptomus_webhooks(order_id);

CREATE INDEX idx_cryptomus_webhooks_status
    ON cryptomus_webhooks(status);

CREATE INDEX idx_cryptomus_webhooks_received_at
    ON cryptomus_webhooks(received_at);

CREATE INDEX idx_cryptomus_webhooks_processed_transaction_id
    ON cryptomus_webhooks(processed_transaction_id);


-- =========================
-- Webhook Replays Table
-- =========================
CREATE TABLE webhook_replays (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id UUID NOT NULL REFERENCES cryptomus_webhooks(id) ON DELETE CASCADE,
    replayed_by VARCHAR(255),
    reason VARCHAR(500),
    result VARCHAR(50) NOT NULL DEFAULT 'pending',
    error_message TEXT,
    replayed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_webhook_replays_webhook_id
    ON webhook_replays(webhook_id);

CREATE INDEX idx_webhook_replays_replayed_at
    ON webhook_replays(replayed_at);