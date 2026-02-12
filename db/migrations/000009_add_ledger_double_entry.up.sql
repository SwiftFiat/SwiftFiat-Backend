CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    transaction_id UUID NOT NULL
        REFERENCES transactions(id)
        ON DELETE CASCADE,

    wallet_id UUID
        REFERENCES swift_wallets(id)
        ON DELETE SET NULL,

    entry_type VARCHAR(10) NOT NULL
        CHECK (entry_type IN ('debit', 'credit')),

    amount DECIMAL(19,4) NOT NULL
        CHECK (amount > 0),

    source_type VARCHAR(20) NOT NULL
        CHECK (source_type IN ('on-platform', 'off-platform')),

    destination_type VARCHAR(20) NOT NULL
        CHECK (destination_type IN ('on-platform', 'off-platform')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ledger_wallet_created
ON ledger_entries (wallet_id, created_at);
