/**
 * Virtual Cards & Subscriptions System Schema
 * Provider: BridgeCard (https://docs.bridgecard.co/)
 * Currency: USD only
 * 
 * IMPORTANT: Card details (PAN, CVV, expiry) are stored ONLY in BridgeCard.
 * We only store references and our business logic metadata.
 */

-- ============================================================================
-- CARD PLANS (Admin-Managed)
-- ============================================================================

/**
 * Table: card_plans
 * Purpose: Define card plan tiers with associated fees and limits
 * Examples: Standard, Platinum, Business
 */
 CREATE TABLE IF NOT EXISTS "card_plans" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- Plan identification
    "name" VARCHAR(100) NOT NULL UNIQUE, -- e.g., "Standard", "Platinum"
    "description" TEXT,
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    
    -- Fees 
    "creation_fee" DECIMAL(10,2) NOT NULL DEFAULT 0, -- One-time card creation fee
    "monthly_maintenance_fee" DECIMAL(10,2) NOT NULL DEFAULT 0, -- Recurring monthly fee
    
    -- Limits (in USD)
    "monthly_spending_limit" DECIMAL(20,2) NOT NULL, -- Max spend per month
    "transaction_limit" DECIMAL(20,2) NOT NULL, -- Max per transaction
    "daily_spending_limit" DECIMAL(20,2), -- Optional daily limit
    "card_limit" DECIMAL(20,2), -- max amount a card can have
     
    -- Additional features
    "max_cards_per_user" INT NOT NULL DEFAULT 1,
    "failed_tx_count_before_block" INT DEFAULT 3,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "deleted_at" TIMESTAMPTZ
);

CREATE INDEX idx_card_plans_active ON card_plans(is_active) WHERE deleted_at IS NULL;

/**
 * Table: virtual_cards
 * Purpose: Track user's virtual cards and link to BridgeCard
 * 
 * NOTE: NO card details stored here. Use BridgeCard API to:
 * - Get card details: GET /issuing/cards/{card_id}
 * - Get secure details: GET /issuing/cards/{card_id}/secure
 * - Fund card: POST /issuing/cards/{card_id}/fund
 * - Freeze/Unfreeze: POST /issuing/cards/{card_id}/freeze|unfreeze
 * - Terminate: POST /issuing/cards/{card_id}/terminate
 */
 CREATE TABLE IF NOT EXISTS "virtual_cards" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Relationships
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "card_plan_id" BIGINT NOT NULL REFERENCES card_plans(id),
    
    -- BridgeCard Integration (ONLY reference)
    "bridgecard_card_id" VARCHAR(255) UNIQUE NOT NULL, -- BridgeCard's card ID
    
    -- User-defined metadata
    "card_name" VARCHAR(100) NOT NULL, -- User-friendly name like "Netflix Card"
    "card_color" VARCHAR(7), -- Hex color for UI display
    
    -- Business logic tracking
    "currency" VARCHAR(3) NOT NULL DEFAULT 'USD',
    
    -- Spending tracking (resets monthly, for our limits enforcement)
    "current_month_spend" BIGINT DEFAULT 0, -- current_month_spend means 
    "current_day_spend" BIGINT DEFAULT 0,
    "spending_month" VARCHAR(20), -- Format: YYYY-MM
    "spending_day" VARCHAR(20), -- For daily limit reset
    
    -- Status (mirrors BridgeCard status but cached for quick queries)
    "status" VARCHAR(20) NOT NULL DEFAULT 'active', 
    -- Status: 'active', 'frozen', 'terminated', 'inactive'
    "status_reason" TEXT,
     
    -- Auto top-up feature (our business logic)
    "auto_topup_enabled" BOOLEAN NOT NULL DEFAULT FALSE,
    "auto_topup_threshold" BIGINT, -- Top up when balance falls below
    "auto_topup_amount" BIGINT, -- Amount to add
    "auto_topup_source_wallet_id" UUID, -- Which wallet to pull funds from
    
    -- Billing cycle (for monthly maintenance fee)
    "next_billing_date" TIMESTAMPTZ,
    "last_billing_date" TIMESTAMPTZ,
    
    -- Usage tracking
    "last_transaction_at" TIMESTAMPTZ,
    "total_transactions_count" BIGINT NOT NULL DEFAULT 0,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "terminated_at" TIMESTAMPTZ
);

CREATE INDEX idx_virtual_cards_user ON virtual_cards(user_id) WHERE terminated_at IS NULL;
CREATE INDEX idx_virtual_cards_status ON virtual_cards(status) WHERE terminated_at IS NULL;
CREATE INDEX idx_virtual_cards_bridgecard ON virtual_cards(bridgecard_card_id);
CREATE INDEX idx_virtual_cards_billing ON virtual_cards(next_billing_date) WHERE status = 'active';
CREATE INDEX idx_virtual_cards_autotopup ON virtual_cards(auto_topup_enabled) WHERE status = 'active';

/**
 * Table: card_funding_history
 * Purpose: Track all card funding operations (top-ups from wallets)
 */
 CREATE TABLE IF NOT EXISTS "card_funding_history" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Relationships
    "card_id" UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    "user_id" BIGINT NOT NULL REFERENCES users(id),
    "source_wallet_id" UUID NOT NULL References swift_wallets(id), -- References wallets table
    
    -- Funding details
    "amount" DECIMAL(10,2) NOT NULL,
    "currency" VARCHAR(3) NOT NULL DEFAULT 'USD',
    "source_currency" VARCHAR(3) NOT NULL, -- Wallet currency (USD, USDC, USDT)
    "exchange_rate" DECIMAL(20, 8), -- If conversion happened
    
    -- BridgeCard reference
    "bridgecard_transaction_id" VARCHAR(255),
    
    -- Metadata
    "funding_type" VARCHAR(20) NOT NULL, 
    -- Types: 'manual', 'auto_topup', 'creation_fee', 'maintenance_fee_refund'
    "initiated_by" VARCHAR(20) NOT NULL, -- 'user', 'system'
    
    -- Status
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'successful', 'failed', 'reversed')),
    -- Status: 'pending', 'completed', 'failed', 'reversed'
    "failure_reason" TEXT,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "completed_at" TIMESTAMPTZ
);

CREATE INDEX idx_card_funding_card ON card_funding_history(card_id);
CREATE INDEX idx_card_funding_user ON card_funding_history(user_id);
CREATE INDEX idx_card_funding_status ON card_funding_history(status);
CREATE INDEX idx_card_funding_created ON card_funding_history(created_at DESC);

-- ============================================================================
-- CARD TRANSACTIONS
-- ============================================================================

/**
 * Table: card_transactions
 * Purpose: Log all card transactions from BridgeCard webhooks
 * Source: BridgeCard webhook notifications
 */
 CREATE TABLE IF NOT EXISTS "card_transactions" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "transaction_id" UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    
    -- Relationships
    "card_id" UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    "user_id" BIGINT NOT NULL REFERENCES users(id),
    
    -- BridgeCard transaction reference
    "bridgecard_transaction_id" VARCHAR(255) UNIQUE NOT NULL,
    "transaction_type" VARCHAR(50) NOT NULL, 
    -- Types: 'debit', 'credit', 'reversal', 'refund'
    
    -- Merchant details (from BridgeCard webhook) 
    "merchant_name" VARCHAR(255),
    "merchant_category" VARCHAR(100), -- MCC category
    "merchant_category_code" VARCHAR(10), -- MCC code
    
    -- Amounts
    "amount" BIGINT NOT NULL,
    "fee" BIGINT NOT NULL DEFAULT 0,
    "currency" VARCHAR(3) NOT NULL DEFAULT 'USD',
    
    -- Original amounts (if different currency)
    "billing_amount" BIGINT,
    "billing_currency" VARCHAR(3),
    
    -- Status
    "status" VARCHAR(20) NOT NULL,
    -- Status: 'pending', 'successful', 'declined', 'reversed'
    "decline_reason" TEXT,
    
    -- Classification (for subscription detection)
    "is_recurring_merchant" BOOLEAN NOT NULL DEFAULT FALSE,
    "subscription_id" UUID, -- Links to user_subscriptions if detected
    
    -- Card balance after transaction (from BridgeCard)
    "balance_after" DECIMAL(10,2),
    
    -- Webhook metadata
    "webhook_received_at" TIMESTAMPTZ,
    "raw_webhook_data" JSONB, -- Store full webhook payload for debugging

    "mode" BOOLEAN NOT NULL,
    
    -- Audit
    "transaction_date" TIMESTAMPTZ NOT NULL,
    "transaction_timestamp" TIMESTAMPTZ NOT NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_card_transactions_card ON card_transactions(card_id);
CREATE INDEX idx_card_transactions_user ON card_transactions(user_id);
CREATE INDEX idx_card_transactions_bridgecard ON card_transactions(bridgecard_transaction_id);
CREATE INDEX idx_card_transactions_merchant ON card_transactions(merchant_name) WHERE merchant_name IS NOT NULL;
CREATE INDEX idx_card_transactions_date ON card_transactions(transaction_date DESC);
CREATE INDEX idx_card_transactions_recurring ON card_transactions(is_recurring_merchant) WHERE is_recurring_merchant = TRUE;
CREATE INDEX idx_card_transactions_subscription ON card_transactions(subscription_id) WHERE subscription_id IS NOT NULL;

-- ============================================================================
-- SUBSCRIPTION MERCHANTS DATABASE
-- ============================================================================

/**
 * Table: subscription_merchants
 * Purpose: Master database of known subscription merchants
 * Used for automatic subscription detection from transactions
 */
 CREATE TABLE IF NOT EXISTS "subscription_merchants" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- Merchant identification
    "merchant_name" VARCHAR(255) NOT NULL UNIQUE, -- Normalized name
    "display_name" VARCHAR(255) NOT NULL, -- User-friendly name
    "aliases" TEXT[], -- Alternative names for matching
    
    -- Classification
    "category" VARCHAR(100) NOT NULL,
    -- Categories: 'streaming', 'cloud_storage', 'gaming', 'music', 
    --             'productivity', 'fitness', 'news', 'utilities', 'other'
    "subcategory" VARCHAR(100),
    
    -- Merchant details
    "logo_url" TEXT,
    "website" VARCHAR(255),
    "description" TEXT,
    "merchant_country" VARCHAR(3),
    
    -- Pattern detection
    "typical_intervals" INT[], -- Common billing intervals in days [30, 365]
    "typical_amounts" BIGINT[], -- Common charge amounts
    "mcc_codes" VARCHAR(10)[], -- Merchant Category Codes
    
    -- Confidence scoring
    "match_confidence" DECIMAL(3, 2) NOT NULL DEFAULT 1.0, -- 0.0 to 1.0
    
    -- Admin controls
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "auto_detect" BOOLEAN NOT NULL DEFAULT TRUE,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subscription_merchants_name ON subscription_merchants(merchant_name);
CREATE INDEX idx_subscription_merchants_category ON subscription_merchants(category);
CREATE INDEX idx_subscription_merchants_active ON subscription_merchants(is_active, auto_detect);

-- ============================================================================
-- USER SUBSCRIPTIONS
-- ============================================================================

/**
 * Table: user_subscriptions
 * Purpose: Track detected and confirmed user subscriptions
 */
 CREATE TABLE IF NOT EXISTS "user_subscriptions" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Relationships
    "user_id" BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "card_id" UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    "merchant_id" BIGINT REFERENCES subscription_merchants(id),
    
    -- Subscription details
    "merchant_name" VARCHAR(255) NOT NULL,
    "display_name" VARCHAR(255) NOT NULL,
    "category" VARCHAR(100),
    
    -- Billing information
    "amount" BIGINT NOT NULL,
    "currency" VARCHAR(3) NOT NULL DEFAULT 'USD',
    "billing_interval_days" INT NOT NULL, -- Typically 30, 365, etc.
    
    -- Dates
    "first_charge_date" TIMESTAMPTZ NOT NULL, -- When we first detected it
    "last_charge_date" TIMESTAMPTZ NOT NULL, -- Most recent charge
    "next_estimated_charge_date" TIMESTAMPTZ NOT NULL, -- Predicted renewal
    
    -- Status
    "status" VARCHAR(20) NOT NULL DEFAULT 'active',
    -- Status: 'active', 'cancelled', 'failed', 'paused'
    "confidence_score" DECIMAL(3, 2) NOT NULL DEFAULT 0.5, -- Detection confidence
    
    -- Tracking
    "total_charges" INT NOT NULL DEFAULT 1, -- Number of successful charges
    "failed_charges" INT NOT NULL DEFAULT 0,
    "last_failed_date" TIMESTAMPTZ,
    "last_failure_reason" TEXT,
    
    -- User preferences
    "reminder_enabled" BOOLEAN NOT NULL DEFAULT TRUE,
    "reminder_days_before" INT NOT NULL DEFAULT 3,
    "user_confirmed" BOOLEAN NOT NULL DEFAULT FALSE, -- User acknowledged subscription
    "custom_name" VARCHAR(255), -- User can override display name

    "is_custom" BOOLEAN NOT NULL DEFAULT FALSE,
    "custom_billing_cycle" VARCHAR(20), -- 'daily', 'monthly', 'yearly'
    "custom_amount_override" BOOLEAN NOT NULL DEFAULT FALSE,
    "auto_topup_buffer_percent" DECIMAL(5,2), -- percentage buffer for auto topup
    "custom_reminder_timing" INT, -- 'same_day', '3_days_before', '1_day_before'
    "notes" TEXT, -- User notes
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "cancelled_at" TIMESTAMPTZ
);

CREATE INDEX idx_user_subscriptions_user ON user_subscriptions(user_id) WHERE status = 'active';
CREATE INDEX idx_user_subscriptions_card ON user_subscriptions(card_id) WHERE status = 'active';
CREATE INDEX idx_user_subscriptions_merchant ON user_subscriptions(merchant_id);
CREATE INDEX idx_user_subscriptions_next_charge ON user_subscriptions(next_estimated_charge_date) 
    WHERE status = 'active';
CREATE INDEX idx_user_subscriptions_status ON user_subscriptions(status);
ALTER TABLE user_subscriptions ADD CONSTRAINT chk_custom_billing_cycle 
CHECK (custom_billing_cycle IS NULL OR custom_billing_cycle IN ('daily', 'monthly', 'yearly'));


-- ============================================================================
-- SUBSCRIPTION REMINDERS
-- ============================================================================

/**
 * Table: subscription_reminders
 * Purpose: Track sent reminders to avoid duplicates
 */
 CREATE TABLE IF NOT EXISTS "subscription_reminders" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Relationships
    "subscription_id" UUID NOT NULL REFERENCES user_subscriptions(id) ON DELETE CASCADE,
    "user_id" BIGINT NOT NULL REFERENCES users(id),
    
    -- Reminder details
    "reminder_type" VARCHAR(50) NOT NULL,
    -- Types: 'upcoming_renewal', 'payment_failed', 'low_balance', 'cancelled'
    "scheduled_for" TIMESTAMPTZ NOT NULL,
    "sent_at" TIMESTAMPTZ,
    
    -- Content
    "title" VARCHAR(255) NOT NULL,
    "message" TEXT NOT NULL,
    "action_url" VARCHAR(500),
    
    -- Delivery
    "channels" VARCHAR(50)[], -- ['push', 'email', 'sms']
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending',
    -- Status: 'pending', 'sent', 'failed', 'cancelled'
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subscription_reminders_subscription ON subscription_reminders(subscription_id);
CREATE INDEX idx_subscription_reminders_user ON subscription_reminders(user_id);
CREATE INDEX idx_subscription_reminders_scheduled ON subscription_reminders(scheduled_for) 
    WHERE status = 'pending';
CREATE INDEX idx_subscription_reminders_status ON subscription_reminders(status);

-- ============================================================================
-- CARD BILLING HISTORY
-- ============================================================================

/**
 * Table: card_billing_history
 * Purpose: Track monthly maintenance fee charges
 */
CREATE TABLE IF NOT EXISTS "card_billing_history" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Relationships
    "card_id" UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    "user_id" BIGINT NOT NULL REFERENCES users(id),
    "card_plan_id" BIGINT NOT NULL REFERENCES card_plans(id),
    
    -- Billing details
    "billing_type" VARCHAR(50) NOT NULL,
    -- Types: 'creation_fee', 'monthly_maintenance', 'refund'
    "amount" DECIMAL(10,2) NOT NULL,
    "currency" VARCHAR(3) NOT NULL DEFAULT 'USD',
    
    -- Period
    "billing_period_start" TIMESTAMPTZ NOT NULL,
    "billing_period_end" TIMESTAMPTZ NOT NULL,
    
    -- Payment
    "source_wallet_id" UUID NOT NULL REFERENCES swift_wallets(id), -- Which wallet was charged
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending',
    -- Status: 'pending', 'completed', 'failed', 'waived'
    "failure_reason" TEXT,
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "processed_at" TIMESTAMPTZ
);

CREATE INDEX idx_card_billing_card ON card_billing_history(card_id);
CREATE INDEX idx_card_billing_user ON card_billing_history(user_id);
CREATE INDEX idx_card_billing_status ON card_billing_history(status);
CREATE INDEX idx_card_billing_period ON card_billing_history(billing_period_start DESC);


CREATE TABLE IF NOT EXISTS "subscription_system_settings" (
    "id" BIGSERIAL PRIMARY KEY,
    "setting_key" VARCHAR(100) NOT NULL UNIQUE,
    "setting_value" TEXT NOT NULL,
    "setting_type" VARCHAR(50) NOT NULL, -- 'integer', 'decimal', 'boolean', 'string'
    "description" TEXT,
    "category" VARCHAR(50) NOT NULL, -- 'renewal', 'auto_topup', 'reminder', 'limits'
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "updated_by" BIGINT REFERENCES users(id),
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subscription_settings_key ON subscription_system_settings(setting_key) WHERE is_active = TRUE;
CREATE INDEX idx_subscription_settings_category ON subscription_system_settings(category);

-- View for easy access to active settings
CREATE OR REPLACE VIEW active_subscription_settings AS
SELECT 
    setting_key,
    setting_value,
    setting_type,
    description,
    category,
    CASE setting_type
        WHEN 'integer' THEN setting_value::INTEGER
        WHEN 'decimal' THEN setting_value::DECIMAL
        WHEN 'boolean' THEN setting_value::BOOLEAN
        ELSE NULL
    END as typed_value
FROM subscription_system_settings
WHERE is_active = TRUE;

-- ============================================================================
-- ANALYTICS VIEWS
-- ============================================================================

/**
 * View: user_subscription_summary
 * Purpose: Quick overview of user's subscription spending
 */
CREATE OR REPLACE VIEW user_subscription_summary AS
SELECT 
    user_id,
    COUNT(*) FILTER (WHERE status = 'active') as active_subscriptions,
    COUNT(*) FILTER (WHERE status = 'failed') as failed_subscriptions,
    SUM(amount) FILTER (WHERE status = 'active') as total_monthly_spend,
    ARRAY_AGG(DISTINCT category) FILTER (WHERE status = 'active') as categories,
    MIN(next_estimated_charge_date) FILTER (WHERE status = 'active') as next_charge_date
FROM user_subscriptions
GROUP BY user_id;

/**
 * View: card_spending_summary
 * Purpose: Card spending analytics
 */
CREATE OR REPLACE VIEW card_spending_summary AS
SELECT 
    c.id as card_id,
    c.user_id,
    c.card_name,
    COUNT(t.id) FILTER (WHERE t.status = 'approved' AND t.transaction_date >= NOW() - INTERVAL '30 days') as transactions_30d,
    SUM(t.amount) FILTER (WHERE t.status = 'approved' AND t.transaction_date >= NOW() - INTERVAL '30 days') as spend_30d,
    COUNT(DISTINCT t.merchant_name) FILTER (WHERE t.transaction_date >= NOW() - INTERVAL '30 days') as unique_merchants_30d,
    COUNT(t.id) FILTER (WHERE t.is_recurring_merchant = TRUE) as subscription_transactions
FROM virtual_cards c
LEFT JOIN card_transactions t ON c.id = t.card_id
WHERE c.terminated_at IS NULL
GROUP BY c.id, c.user_id, c.card_name;

-- ============================================================================
-- SEED DATA: Default Card Plans
-- ============================================================================

INSERT INTO card_plans (name, description, creation_fee, monthly_maintenance_fee, 
                        monthly_spending_limit, transaction_limit, daily_spending_limit,
                        max_cards_per_user, card_limit)
VALUES 
    ('Standard', 'Basic virtual card for everyday subscriptions', 
     5, 100, 5000, 500, 200, 1, 1000),
    
    ('Platinum', 'Premium card with higher limits and no monthly fees', 
     10, 0, 20000, 2000, 1000, 1, 2000)
ON CONFLICT (name) DO NOTHING;

-- View for custom subscriptions summary
CREATE OR REPLACE VIEW custom_subscriptions_summary AS
SELECT 
    us.user_id,
    COUNT(*) FILTER (WHERE us.is_custom = TRUE) as custom_subscription_count,
    COUNT(*) FILTER (WHERE us.is_custom = TRUE AND us.status = 'active') as active_custom_count,
    SUM(us.amount) FILTER (WHERE us.is_custom = TRUE AND us.status = 'active') as total_custom_spend,
    COUNT(*) FILTER (WHERE us.custom_billing_cycle = 'daily') as daily_subscriptions,
    COUNT(*) FILTER (WHERE us.custom_billing_cycle = 'monthly') as monthly_subscriptions,
    COUNT(*) FILTER (WHERE us.custom_billing_cycle = 'yearly') as yearly_subscriptions
FROM user_subscriptions us
WHERE us.is_custom = TRUE
GROUP BY us.user_id;

-- ============================================================================
-- SEED DATA: Common Subscription Merchants
-- ============================================================================

INSERT INTO subscription_merchants (merchant_name, display_name, aliases, category, subcategory, 
                                   typical_intervals, typical_amounts, auto_detect)
VALUES 
    ('netflix', 'Netflix', ARRAY['netflix.com', 'nflx'], 'streaming', 'video', ARRAY[30], ARRAY[999, 1999, 2499], TRUE),
    ('spotify', 'Spotify', ARRAY['spotify.com', 'spotifyab'], 'streaming', 'music', ARRAY[30], ARRAY[999, 1999], TRUE),
    ('apple', 'Apple Services', ARRAY['apple.com', 'itunes', 'icloud'], 'utilities', 'cloud', ARRAY[30], ARRAY[99, 299, 999], TRUE),
    ('youtube', 'YouTube Premium', ARRAY['youtube.com', 'google youtube'], 'streaming', 'video', ARRAY[30], ARRAY[1199], TRUE),
    ('amazon', 'Amazon Prime', ARRAY['amazon.com', 'amzn', 'prime'], 'utilities', 'shopping', ARRAY[30, 365], ARRAY[1499, 13900], TRUE),
    ('chatgpt', 'ChatGPT Plus', ARRAY['openai', 'chat.openai.com'], 'productivity', 'ai', ARRAY[30], ARRAY[2000], TRUE),
    ('notion', 'Notion', ARRAY['notion.so'], 'productivity', 'workspace', ARRAY[30, 365], ARRAY[1000, 10000], TRUE),
    ('github', 'GitHub', ARRAY['github.com'], 'productivity', 'developer', ARRAY[30], ARRAY[400, 700, 2100], TRUE),
    ('microsoft', 'Microsoft 365', ARRAY['microsoft.com', 'office365', 'ms'], 'productivity', 'office', ARRAY[30, 365], ARRAY[699, 9999], TRUE),
    ('disney', 'Disney+', ARRAY['disneyplus', 'disney+'], 'streaming', 'video', ARRAY[30], ARRAY[799, 1099], TRUE)
ON CONFLICT (merchant_name) DO NOTHING;
