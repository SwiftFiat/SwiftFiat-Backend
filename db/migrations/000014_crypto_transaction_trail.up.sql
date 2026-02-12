--! To enable easy checks for transaction hashes on crypto wallet addresses
-- Start transaction
BEGIN;

-- Crypto Transaction Trail table
CREATE TABLE IF NOT EXISTS "crypto_transaction_trail" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "address_id" VARCHAR(200) NOT NULL,
    "order_id" VARCHAR(100) NOT NULL,
    "transaction_hash" VARCHAR(100),
    "amount" DECIMAL(30,10) DEFAULT 0,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for faster lookup
CREATE INDEX IF NOT EXISTS "idx_crypto_order_id" ON crypto_transaction_trail(order_id);

-- Create triggers for updated_at
CREATE OR REPLACE TRIGGER update_crypt_trail_updated_at
    BEFORE UPDATE ON crypto_transaction_trail
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- End Transaction
COMMIT;