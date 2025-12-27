-- =====================================================
-- DOWN MIGRATION: VAULT SAVINGS SYSTEM
-- =====================================================

-- -----------------------------------------------------
-- Drop triggers
-- -----------------------------------------------------
DROP TRIGGER IF EXISTS update_vault_savings_updated_at ON vault_savings;
DROP TRIGGER IF EXISTS update_vault_yield_configs_updated_at ON vault_yield_configs;

-- -----------------------------------------------------
-- Drop function
-- -----------------------------------------------------
DROP FUNCTION IF EXISTS update_updated_at_column();

-- -----------------------------------------------------
-- Drop indexes
-- -----------------------------------------------------

-- Vault Yield Configs indexes
DROP INDEX IF EXISTS idx_yield_config_effective;
DROP INDEX IF EXISTS idx_yield_config_active;
DROP INDEX IF EXISTS idx_yield_config_currency;

-- Vault Yields indexes
DROP INDEX IF EXISTS idx_vault_yields_period;
DROP INDEX IF EXISTS idx_vault_yields_status;
DROP INDEX IF EXISTS idx_vault_yields_user;
DROP INDEX IF EXISTS idx_vault_yields_vault;

-- Vault Transactions indexes
DROP INDEX IF EXISTS idx_vault_txn_reference;
DROP INDEX IF EXISTS idx_vault_txn_status;
DROP INDEX IF EXISTS idx_vault_txn_created;
DROP INDEX IF EXISTS idx_vault_txn_type;
DROP INDEX IF EXISTS idx_vault_txn_vault;
DROP INDEX IF EXISTS idx_vault_txn_user;

-- Vault Savings indexes
DROP INDEX IF EXISTS idx_vault_savings_next_autosave;
DROP INDEX IF EXISTS idx_vault_savings_currency;
DROP INDEX IF EXISTS idx_vault_savings_status;
DROP INDEX IF EXISTS idx_vault_savings_user;

-- -----------------------------------------------------
-- Drop tables (order matters due to foreign keys)
-- -----------------------------------------------------
DROP TABLE IF EXISTS vault_yields;
DROP TABLE IF EXISTS vault_transactions;
DROP TABLE IF EXISTS vault_yield_configs;
DROP TABLE IF EXISTS vault_savings;

-- -----------------------------------------------------
-- Drop extension (only if no longer needed elsewhere)
-- -----------------------------------------------------
DROP EXTENSION IF EXISTS "uuid-ossp";
