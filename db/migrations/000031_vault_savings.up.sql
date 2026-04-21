CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- VAULT SAVINGS TABLE
-- ============================================================================
CREATE TABLE IF NOT EXISTS "vault_savings" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" UUID NOT NULL REFERENCES users("id") ON DELETE CASCADE,
    "vault_name" VARCHAR(100) NOT NULL,
    "description" TEXT,
    "goal_amount" DECIMAL(19,4) DEFAULT 0,
    "current_balance" DECIMAL(19,4) DEFAULT 0,
    "category" VARCHAR(100) NOT NULL,
    "currency" VARCHAR(4) NOT NULL DEFAULT 'USDT' CHECK (currency IN ('USDT', 'USDC', 'NGN', 'USD')),
    
    -- Auto-save configuration
    "auto_save_enabled" BOOLEAN NOT NULL DEFAULT FALSE,
    "auto_save_frequency" VARCHAR(10) CHECK(auto_save_frequency IN ('daily', 'weekly', 'monthly')),
    "auto_save_amount" DECIMAL(19,4) DEFAULT 0,
    "next_auto_save" TIMESTAMPTZ,
    
    -- Flexible recurring rule (for complex scenarios)
    "recurring_rule" JSONB,
    
    -- Yield tracking
    "total_yield_earned" DECIMAL(19,4) DEFAULT 0,
    "next_yield_calculation" TIMESTAMPTZ,
    "last_yield_calculation" TIMESTAMPTZ,
    
    -- Status and type
    "status" VARCHAR(15) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'cancelled', 'completed')),
    "vault_type" VARCHAR(20) NOT NULL DEFAULT 'flexible' CHECK (vault_type IN ('flexible', 'locked')),
    
    -- Audit timestamps
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "completed_at" TIMESTAMPTZ
);
 
-- ============================================================================
-- VAULT TRANSACTIONS TABLE
-- ============================================================================
CREATE TABLE IF NOT EXISTS "vault_transactions" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),  
    "user_id" UUID NOT NULL REFERENCES users("id") ON DELETE CASCADE,
    "vault_id" UUID NOT NULL REFERENCES vault_savings("id") ON DELETE CASCADE,
    "transaction_type" VARCHAR(30) NOT NULL CHECK(transaction_type IN ('deposit', 'withdrawal', 'auto_save', 'yield_credit')),
    "transaction_id" UUID REFERENCES transactions(id) ON DELETE CASCADE,
    "amount" DECIMAL(19, 4) NOT NULL,
    "currency" VARCHAR(4) NOT NULL CHECK(currency IN ('USDT', 'USDC', 'NGN', 'USD')),
    
    -- Wallet references
    "source_wallet" UUID REFERENCES swift_wallets(id),
    "destination_wallet" UUID REFERENCES swift_wallets(id),
    
    -- Balance tracking
    "balance_before" DECIMAL(19, 4) NOT NULL,
    "balance_after" DECIMAL(19, 4) NOT NULL,
    
    -- Transaction metadata
    "reference" VARCHAR(100) UNIQUE, -- for idempotency
    "description" VARCHAR(255),
    "metadata" JSONB,
     
    -- Status tracking
    "status" VARCHAR(20) DEFAULT 'pending' CHECK(status IN ('pending', 'successful', 'failed', 'cancelled')),
    
    -- Security & approval
    "requires_2fa" BOOLEAN DEFAULT FALSE,
    "two_fa_verified_at" TIMESTAMPTZ,
    
    -- Timestamps
    "completed_at" TIMESTAMPTZ,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- VAULT YIELDS TABLE
-- ============================================================================
CREATE TABLE IF NOT EXISTS "vault_yields" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" UUID NOT NULL REFERENCES users("id") ON DELETE CASCADE,
    "vault_id" UUID NOT NULL REFERENCES vault_savings("id") ON DELETE CASCADE,
    "yield_amount" DECIMAL(19, 4) NOT NULL,
    "yield_rate" DECIMAL(19, 4) NOT NULL, -- APY percentage applied
    "calculation_period_start" TIMESTAMPTZ NOT NULL,
    "calculation_period_end" TIMESTAMPTZ NOT NULL,
    "vault_balance_snapshot" DECIMAL(19, 4) NOT NULL, -- balance used for calculation
    "status" VARCHAR(20) DEFAULT 'calculated' CHECK(status IN ('calculated', 'credited', 'failed')),
    "credited_at" TIMESTAMPTZ,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- VAULT YIELD CONFIGS TABLE
-- ============================================================================
CREATE TABLE IF NOT EXISTS "vault_yield_configs" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "currency" VARCHAR(4) NOT NULL CHECK(currency IN ('USDT', 'USDC', 'NGN', 'USD')),
    "apy_rate" DECIMAL(19, 4) NOT NULL, -- Annual percentage yield (e.g., 3.5 for 3.5%)
    "min_balance_for_yield" DECIMAL(19, 4) NOT NULL DEFAULT 0, -- Minimum balance to earn yield
    "compound_frequency" VARCHAR(10) CHECK(compound_frequency IN ('daily', 'weekly', 'monthly')),
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "effective_from" TIMESTAMPTZ NOT NULL,
    "effective_until" TIMESTAMPTZ,
    "notes" TEXT, -- admin notes about why this config exists
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- INDEXES FOR PERFORMANCE
-- ============================================================================

-- Vault Savings Indexes
CREATE INDEX idx_vault_savings_user ON vault_savings(user_id);
CREATE INDEX idx_vault_savings_status ON vault_savings(status) WHERE status = 'active';
CREATE INDEX idx_vault_savings_currency ON vault_savings(currency);
CREATE INDEX idx_vault_savings_next_autosave ON vault_savings(next_auto_save) WHERE auto_save_enabled = TRUE;

-- Vault Transactions Indexes
CREATE INDEX idx_vault_txn_user ON vault_transactions(user_id);
CREATE INDEX idx_vault_txn_vault ON vault_transactions(vault_id);
CREATE INDEX idx_vault_txn_type ON vault_transactions(transaction_type);
CREATE INDEX idx_vault_txn_created ON vault_transactions(created_at DESC);
CREATE INDEX idx_vault_txn_status ON vault_transactions(status);
CREATE INDEX idx_vault_txn_reference ON vault_transactions(reference) WHERE reference IS NOT NULL;

-- Vault Yields Indexes
CREATE INDEX idx_vault_yields_vault ON vault_yields(vault_id);
CREATE INDEX idx_vault_yields_user ON vault_yields(user_id);
CREATE INDEX idx_vault_yields_status ON vault_yields(status);
CREATE INDEX idx_vault_yields_period ON vault_yields(calculation_period_start, calculation_period_end);

-- Vault Yield Configs Indexes
CREATE INDEX idx_yield_config_currency ON vault_yield_configs(currency);
CREATE INDEX idx_yield_config_active ON vault_yield_configs(is_active) WHERE is_active = TRUE;
CREATE INDEX idx_yield_config_effective ON vault_yield_configs(effective_from, effective_until);


-- ============================================================================
-- TRIGGERS FOR AUTO-UPDATE timestamps
-- ============================================================================

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_vault_savings_updated_at BEFORE UPDATE ON vault_savings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_vault_yield_configs_updated_at BEFORE UPDATE ON vault_yield_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================================
-- Default values for yield configs
-- ============================================================================
INSERT INTO vault_yield_configs (currency, apy_rate, min_balance_for_yield, compound_frequency, is_active, effective_from, effective_until)
VALUES ('NGN', 3.5, 0, 'daily', TRUE, NOW(), NULL);

INSERT INTO vault_yield_configs (currency, apy_rate, min_balance_for_yield, compound_frequency, is_active, effective_from, effective_until)
VALUES ('USD', 3.5, 0, 'daily', TRUE, NOW(), NULL);
