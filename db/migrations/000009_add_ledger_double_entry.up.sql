-- Ledger entries table (for double-entry accounting)
CREATE TABLE IF NOT EXISTS "ledger_entries" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "transaction_id" UUID NOT NULL REFERENCES transactions(id),
    "wallet_id" UUID, -- NULL if destination/source is off-platform
    "type" VARCHAR(10) NOT NULL, -- 'debit' or 'credit'
    "amount" DECIMAL(19,4) NOT NULL,
    "balance" DECIMAL(19,4) NOT NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "source_type" VARCHAR(20) NOT NULL, -- 'on-platform' or 'off-platform'
    "destination_type" VARCHAR(20) NOT NULL -- 'on-platform' or 'off-platform'
    -- CONSTRAINT "positive_amount" CHECK (amount > 0)
);