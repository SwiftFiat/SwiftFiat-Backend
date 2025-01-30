
-- Start transaction
BEGIN;

-- Drop trigger
DROP TRIGGER IF EXISTS update_address_updated_at ON beneficiaries;

-- Drop index
DROP INDEX IF EXISTS idx_beneficiaries_user_id;

-- Drop Table
DROP TABLE IF EXISTS beneficiaries;

-- End transaction
COMMIT;