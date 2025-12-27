-- ============================================================
-- DOWN MIGRATION: BANK ACCOUNTS, SMART CONVERSION, RAPID RAMP
-- ============================================================

-- ------------------------------------------------------------
-- Drop triggers
-- ------------------------------------------------------------
DROP TRIGGER IF EXISTS set_updated_at_qr_transactions ON qr_transactions;
DROP TRIGGER IF EXISTS set_updated_at_qr_codes ON qr_codes;
DROP TRIGGER IF EXISTS set_updated_at_conversion_rules ON conversion_rules;
DROP TRIGGER IF EXISTS set_updated_at_bank_accounts ON bank_accounts;

-- ------------------------------------------------------------
-- Drop shared trigger function
-- ------------------------------------------------------------
DROP FUNCTION IF EXISTS update_updated_at_column();

-- ------------------------------------------------------------
-- Drop indexes (qr_transactions)
-- ------------------------------------------------------------
DROP INDEX IF EXISTS idx_qr_transactions_cryptomus_address;
DROP INDEX IF EXISTS idx_qr_transactions_bank_account;
DROP INDEX IF EXISTS idx_qr_transactions_payout_ref;
DROP INDEX IF EXISTS idx_qr_transactions_created_at;
DROP INDEX IF EXISTS idx_qr_transactions_transaction_hash;
DROP INDEX IF EXISTS idx_qr_transactions_cryptomus_order;
DROP INDEX IF EXISTS idx_qr_transactions_cryptomus_id;
DROP INDEX IF EXISTS idx_qr_transactions_status;
DROP INDEX IF EXISTS idx_qr_transactions_transaction;
DROP INDEX IF EXISTS idx_qr_transactions_user;
DROP INDEX IF EXISTS idx_qr_transactions_qr_code;

-- ------------------------------------------------------------
-- Drop indexes (qr_codes)
-- ------------------------------------------------------------
DROP INDEX IF EXISTS idx_qr_codes_crypto_network;
DROP INDEX IF EXISTS idx_qr_codes_cryptomus_address;
DROP INDEX IF EXISTS idx_qr_codes_bank_account;
DROP INDEX IF EXISTS idx_qr_codes_wallet;
DROP INDEX IF EXISTS idx_qr_codes_status;
DROP INDEX IF EXISTS idx_qr_codes_token;
DROP INDEX IF EXISTS idx_qr_codes_user;

-- ------------------------------------------------------------
-- Drop indexes (conversion_history)
-- ------------------------------------------------------------
DROP INDEX IF EXISTS idx_conversion_history_user_status;
DROP INDEX IF EXISTS idx_conversion_history_currency_pair;
DROP INDEX IF EXISTS idx_conversion_history_executed_at;
DROP INDEX IF EXISTS idx_conversion_history_status;
DROP INDEX IF EXISTS idx_conversion_history_transaction;
DROP INDEX IF EXISTS idx_conversion_history_rule;
DROP INDEX IF EXISTS idx_conversion_history_user;

-- ------------------------------------------------------------
-- Drop indexes (conversion_rules)
-- ------------------------------------------------------------
DROP INDEX IF EXISTS idx_conversion_rules_target_wallet;
DROP INDEX IF EXISTS idx_conversion_rules_source_wallet;
DROP INDEX IF EXISTS idx_conversion_rules_currency_pair;
DROP INDEX IF EXISTS idx_conversion_rules_next_execution;
DROP INDEX IF EXISTS idx_conversion_rules_active;
DROP INDEX IF EXISTS idx_conversion_rules_status;
DROP INDEX IF EXISTS idx_conversion_rules_user;

-- ------------------------------------------------------------
-- Drop indexes (bank_accounts)
-- ------------------------------------------------------------
DROP INDEX IF EXISTS idx_bank_accounts_one_default_per_user;
DROP INDEX IF EXISTS idx_bank_accounts_verification;
DROP INDEX IF EXISTS idx_bank_accounts_active;
DROP INDEX IF EXISTS idx_bank_accounts_default;
DROP INDEX IF EXISTS idx_bank_accounts_user;

-- ------------------------------------------------------------
-- Drop tables (order matters due to foreign keys)
-- ------------------------------------------------------------
DROP TABLE IF EXISTS qr_transactions;
DROP TABLE IF EXISTS qr_codes;
DROP TABLE IF EXISTS conversion_history;
DROP TABLE IF EXISTS conversion_rules;
DROP TABLE IF EXISTS bank_accounts;
