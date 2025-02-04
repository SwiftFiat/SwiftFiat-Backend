-- Down migration for the core banking system
-- WARNING: This will delete all data! Use with caution in production.

-- Start transaction
BEGIN;

-- Drop triggers
DROP TRIGGER IF EXISTS update_accounts_updated_at ON swift_wallets;

-- Drop indexes
DROP INDEX IF EXISTS idx_accounts_customer;

-- Drop tables in correct order (respecting foreign key constraints)
DROP TABLE IF EXISTS swift_wallets;

-- Optional: Disable UUID extension if no other tables are using it
-- DROP EXTENSION IF EXISTS "uuid-ossp";

-- Add verification that tables were dropped successfully
DO $$
BEGIN
    ASSERT NOT EXISTS (
        SELECT FROM pg_tables 
        WHERE tablename IN ('swift_wallets')
    ), 'Some tables were not dropped successfully';
END $$;

COMMIT;