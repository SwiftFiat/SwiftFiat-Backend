--! To enable storage and retrieval of fees for transactions
-- Start transaction
BEGIN;

-- Transaction Fees table
CREATE TABLE IF NOT EXISTS "transaction_fees" (
    "id" BIGSERIAL PRIMARY KEY,
    "transaction_type" VARCHAR(50) NOT NULL,
    "fee_percentage" DECIMAL(20,8),
    "max_fee" DECIMAL(20,8),
    "flat_fee" DECIMAL(20,8),
    "effective_time" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "source" VARCHAR(50) NOT NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX "idx_fees_transaction_type" ON transaction_fees(transaction_type);
CREATE INDEX "idx_fees_effective_time" ON transaction_fees(effective_time);
CREATE INDEX "idx_fees_lookup" ON transaction_fees(transaction_type, effective_time);

-- End Transaction
COMMIT;