-- Drop all triggers
DROP TRIGGER IF EXISTS trigger_sync_kyc_rejection ON kyc;
DROP TRIGGER IF EXISTS trigger_auto_verify_kyc ON kyc;
DROP TRIGGER IF EXISTS update_kyc_updated_at ON kyc;

-- Drop all functions
DROP FUNCTION IF EXISTS sync_kyc_rejection();
DROP FUNCTION IF EXISTS auto_verify_kyc();
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop indexes
DROP INDEX IF EXISTS idx_kyc_status;
DROP INDEX IF EXISTS idx_kyc_user_id;

-- Drop table
DROP TABLE IF EXISTS kyc;