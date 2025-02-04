-- Drop table comments
COMMENT ON TABLE services_metadata IS NULL;
COMMENT ON TABLE fiat_withdrawal_metadata IS NULL;
COMMENT ON TABLE giftcard_transaction_metadata IS NULL;
COMMENT ON TABLE crypto_transaction_metadata IS NULL;
COMMENT ON TABLE swap_transfer_metadata IS NULL;
COMMENT ON TABLE transactions IS NULL;

-- Drop auto update functions
DROP TRIGGER IF EXISTS set_updated_at ON transactions;

-- Drop indexes
DROP INDEX IF EXISTS idx_services_metadata_source_wallet;
DROP INDEX IF EXISTS idx_fiat_withdrawal_source_wallet;
DROP INDEX IF EXISTS idx_giftcard_source_wallet;
DROP INDEX IF EXISTS idx_crypto_source_hash;
DROP INDEX IF EXISTS idx_swap_transfer_source_wallet;
DROP INDEX IF EXISTS idx_giftcard_transaction_id;
DROP INDEX IF EXISTS idx_crypto_transaction_id;
DROP INDEX IF EXISTS idx_swap_transfer_transaction_id;
DROP INDEX IF EXISTS idx_transactions_created_at;
DROP INDEX IF EXISTS idx_transactions_status;
DROP INDEX IF EXISTS idx_transactions_type;

-- Drop tables
DROP TABLE IF EXISTS services_metadata;
DROP TABLE IF EXISTS fiat_withdrawal_metadata;
DROP TABLE IF EXISTS giftcard_transaction_metadata;
DROP TABLE IF EXISTS crypto_transaction_metadata;
DROP TABLE IF EXISTS swap_transfer_metadata;
DROP TABLE IF EXISTS transactions;


