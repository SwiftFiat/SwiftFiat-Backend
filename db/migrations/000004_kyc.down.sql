-- Drop triggers first
DROP TRIGGER trigger_update_kyc_tier ON kyc;
DROP TRIGGER update_kyc_updated_at ON kyc;

-- Drop functions
DROP FUNCTION update_kyc_tier();
DROP FUNCTION update_updated_at_column();

-- Drop the index
DROP INDEX IF EXISTS idx_kyc_user_id;

-- Drop the table
DROP TABLE IF EXISTS kyc;

-- Add a comment to indicate this is a down migration
COMMENT ON SCHEMA public IS 'KYC table and related objects have been removed.';