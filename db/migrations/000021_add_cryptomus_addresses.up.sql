--! To enable easy retrieval and management of addresses
--! deletion of user accounts should not affect the management
--! of an address so that this can be reassigned
-- Start transaction
BEGIN;

-- Crypto Addresses table
CREATE TABLE IF NOT EXISTS "cryptomus_addresses" (
                                                     "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "customer_id" BIGSERIAL REFERENCES users(id) ON DELETE SET NULL,
    "wallet_uuid" VARCHAR NOT NULL,
    "uuid" VARCHAR NOT NULL,
    "address" VARCHAR NOT NULL UNIQUE,
    "network" VARCHAR NOT NULL,
    "currency" VARCHAR NOT NULL,
    "payment_url" VARCHAR,
    "callback_url" VARCHAR,
    "status" VARCHAR(20) NOT NULL DEFAULT 'active',
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );

-- Create index for faster lookup
CREATE INDEX IF NOT EXISTS "idx_cryptomus_addresses_wallet_uuid" ON cryptomus_addresses(wallet_uuid);
CREATE INDEX IF NOT EXISTS "idx_cryptomus_addresses_uuid" ON cryptomus_addresses(uuid);
CREATE INDEX IF NOT EXISTS "idx_cryptomus_addresses_address" ON cryptomus_addresses(address);
CREATE INDEX IF NOT EXISTS "idx_cryptomus_addresses_network" ON cryptomus_addresses(network);
CREATE INDEX IF NOT EXISTS "idx_cryptomus_addresses_currency" ON cryptomus_addresses(currency);
CREATE INDEX IF NOT EXISTS "idx_cryptomus_addresses_network_currency_customer_id" ON cryptomus_addresses(network, currency, customer_id);

-- End Transaction
COMMIT;