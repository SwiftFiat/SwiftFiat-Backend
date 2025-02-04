
-- Start transaction
BEGIN;

-- Drop trigger
DROP TRIGGER IF EXISTS update_crypt_trail_updated_at ON crypto_transaction_trail;

-- Drop index
DROP INDEX IF EXISTS idx_crypto_transaction_hash;

-- Drop Table
DROP TABLE IF EXISTS crypto_transaction_trail;

-- End transaction
COMMIT;