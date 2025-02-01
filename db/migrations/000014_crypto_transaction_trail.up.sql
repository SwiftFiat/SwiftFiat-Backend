--! To enable easy checks for transaction hashes on crypto wallet addresses
-- Start transaction
BEGIN;

-- Crypto Transaction Trail table
CREATE TABLE IF NOT EXISTS "crypto_transaction_trail" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "address_id" VARCHAR(200) NOT NULL,
    "transaction_hash" VARCHAR(128) NOT NULL,
    "amount" DECIMAL(30,10) DEFAULT 0,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for faster lookup
CREATE INDEX IF NOT EXISTS "idx_crypto_transaction_hash" ON crypto_transaction_trail(transaction_hash);

-- Create triggers for updated_at
CREATE OR REPLACE TRIGGER update_crypt_trail_updated_at
    BEFORE UPDATE ON crypto_transaction_trail
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- End Transaction
COMMIT;