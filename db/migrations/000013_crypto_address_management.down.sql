
-- Start transaction
BEGIN;

-- Drop trigger
DROP TRIGGER IF EXISTS update_address_updated_at ON crypto_addresses;

-- Drop index
DROP INDEX IF EXISTS idx_crypto_addresses_address_id;

-- Drop Table
DROP TABLE IF EXISTS crypto_addresses;

-- End transaction
COMMIT;