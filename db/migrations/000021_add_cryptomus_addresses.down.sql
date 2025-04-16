-- Start transaction
BEGIN;

-- Drop indexes first
DROP INDEX IF EXISTS idx_cryptomus_addresses_wallet_uuid;
DROP INDEX IF EXISTS idx_cryptomus_addresses_uuid;
DROP INDEX IF EXISTS idx_cryptomus_addresses_address;
DROP INDEX IF EXISTS idx_cryptomus_addresses_network;
DROP INDEX IF EXISTS idx_cryptomus_addresses_currency;
DROP INDEX IF EXISTS idx_cryptomus_addresses_network_currency;

-- Drop the table
DROP TABLE IF EXISTS cryptomus_addresses;

-- End transaction
COMMIT;