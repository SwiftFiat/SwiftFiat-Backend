--! To enable easy retrieval and management of addresses
--! deletion of user accounts should not affect the management
--! of an address so that this can be reassigned
-- Start transaction
BEGIN;

-- Crypto Addresses table
CREATE TABLE "crypto_addresses" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "customer_id" BIGSERIAL REFERENCES users(id) ON DELETE SET NULL,
    "address_id" VARCHAR(200) NOT NULL,
    "coin" VARCHAR(10) NOT NULL,
    "balance" DECIMAL(30,10) DEFAULT 0,
    "status" VARCHAR(20) NOT NULL DEFAULT 'active',
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for faster lookup
CREATE INDEX "idx_crypto_addresses_address_id" ON crypto_addresses(address_id);

-- Create triggers for updated_at
CREATE TRIGGER "update_address_updated_at"
    BEFORE UPDATE ON crypto_addresses
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- End Transaction
COMMIT;