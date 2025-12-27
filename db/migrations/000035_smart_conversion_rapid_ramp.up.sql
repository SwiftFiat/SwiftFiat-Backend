-- ============================================================
-- BANK ACCOUNTS TABLE (Shared Resource)
-- Users can add multiple bank accounts and select default
-- ============================================================

CREATE TABLE IF NOT EXISTS "bank_accounts" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Bank account details
    "account_name" VARCHAR(100) NOT NULL,
    "account_number" VARCHAR(20) NOT NULL,
    "bank_code" VARCHAR(20) NOT NULL,
    "bank_name" VARCHAR(100) NOT NULL,
    "account_type" VARCHAR(20),
    "currency" VARCHAR(3) NOT NULL DEFAULT 'NGN',

    -- Verification status
    "is_verified" BOOLEAN NOT NULL DEFAULT FALSE,
    "verified_at" TIMESTAMPTZ,
    "verification_method" VARCHAR(50),
    "verification_reference" VARCHAR(100),

    -- Status and default
    "is_default" BOOLEAN NOT NULL DEFAULT FALSE,
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending',

     -- Metadata
    "label" VARCHAR(100),
    "description" TEXT,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "deleted_at" TIMESTAMPTZ,

    CONSTRAINT "valid_bank_account_status" CHECK (status IN ('active', 'pending', 'failed', 'disabled'))
);

CREATE INDEX IF NOT EXISTS "idx_bank_accounts_user" ON bank_accounts(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_bank_accounts_default" ON bank_accounts(user_id, is_default) WHERE is_default = TRUE AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_bank_accounts_active" ON bank_accounts(is_active, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_bank_accounts_verification" ON bank_accounts(is_verified);

-- Ensure only one default bank account per user
CREATE UNIQUE INDEX IF NOT EXISTS "idx_bank_accounts_one_default_per_user" 
    ON bank_accounts(user_id) 
    WHERE is_default = TRUE AND deleted_at IS NULL;

-- ============================================================
-- SMART CONVERSION (Auto-Convert & Rate Alerts)
-- ============================================================

-- Table: conversion_rules
-- Stores user-defined automated conversion rules
CREATE TABLE IF NOT EXISTS "conversion_rules" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Conversion configuration
    "source_currency" VARCHAR(10) NOT NULL CHECK(source_currency IN ('USD', 'NGN', 'USDT', 'USDC')),
    "target_currency" VARCHAR(10) NOT NULL CHECK(target_currency IN ('USD', 'NGN', 'USDT', 'USDC')),
    "source_wallet_id" UUID REFERENCES swift_wallets(id) ON DELETE SET NULL,
    "target_wallet_id" UUID REFERENCES swift_wallets(id) ON DELETE SET NULL,

    -- Trigger conditions
    "trigger_rate" DECIMAL(19,4),
    "trigger_type" VARCHAR(20) NOT NULL,
    "trigger_condition" VARCHAR(10),
    "percentage_threshold" DECIMAL(5,2),

    -- Conversion amount settings
    "conversion_type" VARCHAR(20) NOT NULL,
    "fixed_amount" DECIMAL(19,4),
    "percentage" DECIMAL(5,2),

    -- Schedule settings
    "schedule_frequency" VARCHAR(20),
    "schedule_day_of_week" INTEGER,
    "schedule_day_of_month" INTEGER,
    "schedule_time" TIME,
    "next_execution_at" TIMESTAMPTZ,
    "timezone" VARCHAR(50) DEFAULT 'UTC',

    -- Rule status
    "status" VARCHAR(20) NOT NULL CHECK (status IN ('active', 'paused')) DEFAULT 'active',
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "last_triggered_at" TIMESTAMPTZ,
    "last_trigger_rate" DECIMAL(19,4),
    "execution_count" INTEGER NOT NULL DEFAULT 0,
    "max_executions" INTEGER,
    "failure_count" INTEGER NOT NULL DEFAULT 0,
    "last_failure_reason" TEXT,

    -- Metadata
    "description" TEXT,
    "label" VARCHAR(100),
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "deleted_at" TIMESTAMPTZ,
    
    CONSTRAINT "valid_trigger_type" CHECK (trigger_type IN ('rate_based', 'scheduled', 'percentage')),
    CONSTRAINT "valid_conversion_type" CHECK (conversion_type IN ('fixed_amount', 'percentage', 'full_balance')),
    CONSTRAINT "valid_schedule_frequency" CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly', 'custom')),
    CONSTRAINT "valid_percentage" CHECK (percentage IS NULL OR (percentage > 0 AND percentage <= 100)),
    CONSTRAINT "positive_amounts" CHECK (fixed_amount IS NULL OR fixed_amount > 0),
    CONSTRAINT "valid_trigger_condition" CHECK (trigger_condition IS NULL OR trigger_condition IN ('gte', 'lte', 'eq')),
    CONSTRAINT "different_currencies" CHECK (source_currency != target_currency)
);

CREATE INDEX IF NOT EXISTS "idx_conversion_rules_user" ON conversion_rules(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_conversion_rules_status" ON conversion_rules(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_conversion_rules_active" ON conversion_rules(is_active, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_conversion_rules_next_execution" ON conversion_rules(next_execution_at) 
    WHERE status = 'active' AND is_active = TRUE AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_conversion_rules_currency_pair" ON conversion_rules(source_currency, target_currency);
CREATE INDEX IF NOT EXISTS "idx_conversion_rules_source_wallet" ON conversion_rules(source_wallet_id);
CREATE INDEX IF NOT EXISTS "idx_conversion_rules_target_wallet" ON conversion_rules(target_wallet_id);

-- Table: conversion_history
-- Audit trail of all conversion executions
CREATE TABLE IF NOT EXISTS "conversion_history" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "conversion_rule_id" UUID REFERENCES conversion_rules(id) ON DELETE SET NULL,
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "transaction_id" UUID REFERENCES transactions(id) ON DELETE SET NULL,

    -- Conversion details
    "source_currency" VARCHAR(10) NOT NULL,
    "target_currency" VARCHAR(10) NOT NULL,
    "source_wallet_id" UUID REFERENCES swift_wallets(id) ON DELETE SET NULL,
    "target_wallet_id" UUID REFERENCES swift_wallets(id) ON DELETE SET NULL,
    
    -- Rate information
    "trigger_rate" DECIMAL(19,4),
    "executed_rate" DECIMAL(19,4) NOT NULL,
    "rate_difference_percentage" DECIMAL(5,2),
    "rate_provider" VARCHAR(50),
    
    -- Amount information
    "source_amount" DECIMAL(19,4) NOT NULL,
    "target_amount" DECIMAL(19,4) NOT NULL,
    "fees" DECIMAL(19,4) DEFAULT 0,
    "net_amount" DECIMAL(19,4) NOT NULL,

    -- Balance tracking
    "source_balance_before" DECIMAL(19,4),
    "source_balance_after" DECIMAL(19,4),
    "target_balance_before" DECIMAL(19,4),
    "target_balance_after" DECIMAL(19,4),

    -- Execution details
    "execution_type" VARCHAR(20) NOT NULL,
    "trigger_type" VARCHAR(20),
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending',
    "failure_reason" TEXT,

    -- Audit
    "executed_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT "valid_conversion_status" CHECK (status IN ('success', 'failed', 'pending', 'partial', 'cancelled')),
    CONSTRAINT "valid_execution_type" CHECK (execution_type IN ('automatic', 'manual', 'scheduled')),
    CONSTRAINT "positive_conversion_amounts" CHECK (source_amount > 0 AND target_amount >= 0)
);

CREATE INDEX IF NOT EXISTS "idx_conversion_history_user" ON conversion_history(user_id);
CREATE INDEX IF NOT EXISTS "idx_conversion_history_rule" ON conversion_history(conversion_rule_id);
CREATE INDEX IF NOT EXISTS "idx_conversion_history_transaction" ON conversion_history(transaction_id);
CREATE INDEX IF NOT EXISTS "idx_conversion_history_status" ON conversion_history(status);
CREATE INDEX IF NOT EXISTS "idx_conversion_history_executed_at" ON conversion_history(executed_at DESC);
CREATE INDEX IF NOT EXISTS "idx_conversion_history_currency_pair" ON conversion_history(source_currency, target_currency);
CREATE INDEX IF NOT EXISTS "idx_conversion_history_user_status" ON conversion_history(user_id, status);

-- ============================================================
-- RAPID RAMP QR CODES (Cryptomus Integration)
-- ============================================================

-- Table: qr_codes
-- Generated QR codes for receiving crypto payments via Cryptomus
CREATE TABLE IF NOT EXISTS "qr_codes" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "token" UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- QR configuration
    "qr_type" VARCHAR(20) NOT NULL DEFAULT 'payment',
    "currency_preference" VARCHAR(10) NOT NULL,
    "conversion_mode" VARCHAR(20) NOT NULL DEFAULT 'auto',

    -- Cryptomus integration
    "network" VARCHAR(50) NOT NULL,
    "crypto_currency" VARCHAR(10) NOT NULL,
    "cryptomus_address_id" UUID REFERENCES cryptomus_addresses(id) ON DELETE SET NULL,

    -- Linked accounts
    "linked_wallet_id" UUID REFERENCES swift_wallets(id) ON DELETE SET NULL,
    "linked_bank_account_id" UUID REFERENCES bank_accounts(id) ON DELETE SET NULL,

    -- Amount settings
    "amount" DECIMAL(19,4) NOT NULL,

    -- QR metadata
    "qr_code_data" TEXT NOT NULL,
    "qr_code_image_url" TEXT,
    "description" TEXT,
    "label" VARCHAR(100),

    -- Status and lifecycle
    "status" VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'used', 'expired', 'disabled')),
    "usage_limit" INTEGER,
    "usage_count" INTEGER NOT NULL DEFAULT 0,
    "expires_at" TIMESTAMPTZ,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "last_used_at" TIMESTAMPTZ,
    "deleted_at" TIMESTAMPTZ,
    
    CONSTRAINT "valid_qr_type" CHECK (qr_type IN ('payment', 'deposit')),
    CONSTRAINT "valid_qr_conversion_mode" CHECK (conversion_mode IN ('auto', 'manual')),
    CONSTRAINT "valid_qr_status" CHECK (status IN ('active', 'used', 'expired', 'disabled')),
    CONSTRAINT "positive_qr_amounts" CHECK (
        (fixed_amount IS NULL OR fixed_amount > 0) AND
        (min_amount IS NULL OR min_amount > 0) AND
        (max_amount IS NULL OR max_amount > 0)
    )
);

CREATE INDEX IF NOT EXISTS "idx_qr_codes_user" ON qr_codes(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_qr_codes_token" ON qr_codes(token);
CREATE INDEX IF NOT EXISTS "idx_qr_codes_status" ON qr_codes(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS "idx_qr_codes_wallet" ON qr_codes(linked_wallet_id);
CREATE INDEX IF NOT EXISTS "idx_qr_codes_bank_account" ON qr_codes(linked_bank_account_id);
CREATE INDEX IF NOT EXISTS "idx_qr_codes_cryptomus_address" ON qr_codes(cryptomus_address_id);
CREATE INDEX IF NOT EXISTS "idx_qr_codes_crypto_network" ON qr_codes(network, crypto_currency);

-- Table: qr_transactions
-- Complete lifecycle tracking of QR-initiated payments
-- Flow: pending → received → converting → sending_to_bank → completed
CREATE TABLE IF NOT EXISTS "qr_transactions" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "qr_code_id" UUID NOT NULL REFERENCES qr_codes(id) ON DELETE RESTRICT,
    "transaction_id" UUID REFERENCES transactions(id) ON DELETE SET NULL,
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Cryptomus webhook data
    "cryptomus_transaction_id" VARCHAR(100) UNIQUE,
    "cryptomus_order_id" VARCHAR(100),
    "cryptomus_uuid" VARCHAR(100), 
    "order_id" VARCHAR(100),
    "cryptomus_address_id" UUID REFERENCES cryptomus_addresses(id) ON DELETE SET NULL,
    "webhook_data" JSONB,

    -- Crypto received
    "crypto_currency" VARCHAR(10) NOT NULL,
    "crypto_network" VARCHAR(50) NOT NULL,
    "crypto_amount" DECIMAL(40,20) NOT NULL,
    "crypto_amount_usd" DECIMAL(19,4),
    "transaction_hash" VARCHAR(200),

    -- Conversion details
    "conversion_rate" DECIMAL(19,4),
    "fiat_currency" VARCHAR(10),
    "fiat_amount" DECIMAL(19,4),
    "conversion_fees" DECIMAL(19,4) NOT NULL DEFAULT 0,
    "platform_fees" DECIMAL(19,4) NOT NULL DEFAULT 0,
    "network_fees" DECIMAL(19,4) NOT NULL DEFAULT 0,
    "total_fees" DECIMAL(19,4) NOT NULL DEFAULT 0,
    "net_amount" DECIMAL(19,4),

    -- Bank payout details
    "bank_account_id" UUID REFERENCES bank_accounts(id) ON DELETE SET NULL,
    "bank_account_name" VARCHAR(100),
    "bank_account_number" VARCHAR(20),
    "bank_code" VARCHAR(20),
    "payout_reference" VARCHAR(100),
    "payout_provider" VARCHAR(100),
    "payout_provider_response" JSONB,

    -- Transaction lifecycle
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending',
    "payment_received_at" TIMESTAMPTZ,
    "payment_confirmed_at" TIMESTAMPTZ,
    "conversion_started_at" TIMESTAMPTZ,
    "conversion_completed_at" TIMESTAMPTZ,
    "payout_initiated_at" TIMESTAMPTZ,
    "payout_completed_at" TIMESTAMPTZ,
    
    -- Error handling
    "failure_reason" TEXT,
    "failure_stage" VARCHAR(50),
    "retry_count" INTEGER NOT NULL DEFAULT 0,
    "last_retry_at" TIMESTAMPTZ,
    "max_retries" INTEGER DEFAULT 3,

    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT "valid_qr_transaction_status" CHECK (status IN (
        'pending', 'received', 'converting', 
        'sending_to_bank', 'completed', 'failed', 'cancelled', 'expired'
    )),
    CONSTRAINT "positive_qr_crypto_amount" CHECK (crypto_amount > 0)
);

CREATE INDEX IF NOT EXISTS "idx_qr_transactions_qr_code" ON qr_transactions(qr_code_id);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_user" ON qr_transactions(user_id);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_transaction" ON qr_transactions(transaction_id);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_status" ON qr_transactions(status);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_cryptomus_id" ON qr_transactions(cryptomus_transaction_id);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_cryptomus_order" ON qr_transactions(cryptomus_order_id);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_transaction_hash" ON qr_transactions(transaction_hash);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_created_at" ON qr_transactions(created_at DESC);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_payout_ref" ON qr_transactions(payout_reference);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_bank_account" ON qr_transactions(bank_account_id);
CREATE INDEX IF NOT EXISTS "idx_qr_transactions_cryptomus_address" ON qr_transactions(cryptomus_address_id);
-- CREATE INDEX IF NOT EXISTS "idx_qr_transactions_pending_confirmation" ON qr_transactions(status, confirmation_blocks, required_confirmations) 
--     WHERE status IN ('received', 'confirming');

-- ============================================================
-- AUTO UPDATE TRIGGERS
-- ============================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at_bank_accounts
    BEFORE UPDATE ON bank_accounts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_updated_at_conversion_rules
    BEFORE UPDATE ON conversion_rules
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_updated_at_qr_codes
    BEFORE UPDATE ON qr_codes
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_updated_at_qr_transactions
    BEFORE UPDATE ON qr_transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();


-- ============================================================
-- TABLE COMMENTS
-- ============================================================

COMMENT ON TABLE bank_accounts IS 'User bank accounts for withdrawals and QR payments - users can have multiple accounts with one default';
COMMENT ON TABLE conversion_rules IS 'User-defined automated conversion rules with rate-based and scheduled triggers';
COMMENT ON TABLE conversion_history IS 'Complete audit trail of all conversion executions';
COMMENT ON TABLE qr_codes IS 'Generated QR codes for receiving crypto via Cryptomus with auto-conversion to fiat';
COMMENT ON TABLE qr_transactions IS 'Complete lifecycle tracking of QR payments: Received → Converted → Paid Out';
