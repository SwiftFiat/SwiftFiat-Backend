-- Virtual Cards Table
CREATE TABLE IF NOT EXISTS virtual_cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGSERIAL NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    flutterwave_card_id VARCHAR(255) NOT NULL UNIQUE, -- id of the card in the flutterwave platform
    card_pan_last4 VARCHAR(4), -- last 4 digits of the card
    card_brand VARCHAR(50), -- visa, mastercard, etc
    card_type VARCHAR(50), -- debit, credit, prepaid, etc
    balance DECIMAL(20, 2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD', -- USD, EUR, GBP, NGN, etc
    name_on_card VARCHAR(255),
    expiry_month VARCHAR(2), -- 01, 02, 03, etc
    expiry_year VARCHAR(4), -- 2025, 2026, 2027, etc
    cvv_encrypted TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    billing_address JSONB,
    metadata JSONB,
    is_frozen BOOLEAN DEFAULT false,
    freeze_reason TEXT, -- reason for freezing the card
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_virtual_cards_user_id ON virtual_cards(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_virtual_cards_status ON virtual_cards(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_virtual_cards_flutterwave_id ON virtual_cards(flutterwave_card_id);

-- Subscription Categories Table
CREATE TABLE IF NOT EXISTS subscription_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    icon_url VARCHAR(500),
    display_order INT DEFAULT 0, -- order of display in the app
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Subscription Merchants Table
CREATE TABLE IF NOT EXISTS subscription_merchants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_name VARCHAR(255) NOT NULL, -- name of the merchant. For example, "Netflix", "Amazon", "Apple", "Google", etc.
    merchant_identifier VARCHAR(255) NOT NULL UNIQUE, -- normalized name for matching subscriptions (e.g. "netflix", "amazon", "apple", "google", etc)
    category_id UUID REFERENCES subscription_categories(id),
    logo_url VARCHAR(500),
    description TEXT,
    website_url VARCHAR(500),
    default_renewal_days INT DEFAULT 30,
    common_amounts DECIMAL[] DEFAULT ARRAY[]::DECIMAL[], -- common amounts for the merchant
    is_active BOOLEAN DEFAULT true,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscription_merchants_identifier ON subscription_merchants(merchant_identifier);
CREATE INDEX idx_subscription_merchants_category ON subscription_merchants(category_id);
CREATE INDEX idx_subscription_merchants_active ON subscription_merchants(is_active) WHERE is_active = true;


-- Subscriptions Table
CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGSERIAL NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    merchant_id UUID REFERENCES subscription_merchants(id),
    merchant_name VARCHAR(255) NOT NULL,
    amount DECIMAL(20, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    first_transaction_date TIMESTAMP WITH TIME ZONE NOT NULL,
    last_transaction_date TIMESTAMP WITH TIME ZONE,
    next_estimated_renewal_date TIMESTAMP WITH TIME ZONE NOT NULL,
    renewal_interval_days INT DEFAULT 30,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    confidence_score DECIMAL(3, 2) DEFAULT 1.0, -- how confident we are this is a subscription
    total_spend DECIMAL(20, 2) DEFAULT 0,
    transaction_count INT DEFAULT 1,
    failed_payment_count INT DEFAULT 0,
    last_failed_payment_date TIMESTAMP WITH TIME ZONE,
    cancellation_date TIMESTAMP WITH TIME ZONE,
    cancellation_reason TEXT,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX idx_subscriptions_card_id ON subscriptions(card_id);
CREATE INDEX idx_subscriptions_merchant_id ON subscriptions(merchant_id);
CREATE INDEX idx_subscriptions_status ON subscriptions(status);
CREATE INDEX idx_subscriptions_next_renewal ON subscriptions(next_estimated_renewal_date) WHERE status = 'active';
CREATE INDEX idx_subscriptions_user_status ON subscriptions(user_id, status);


-- Subscription Transactions Table
CREATE TABLE IF NOT EXISTS subscription_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    transaction_id UUID REFERENCES transactions(id),
    flutterwave_transaction_id VARCHAR(255),
    amount DECIMAL(20, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    transaction_date TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(50) NOT NULL,
    failure_reason TEXT,
    merchant_descriptor VARCHAR(500),
    is_renewal BOOLEAN DEFAULT true,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscription_transactions_subscription ON subscription_transactions(subscription_id);
CREATE INDEX idx_subscription_transactions_date ON subscription_transactions(transaction_date DESC);
CREATE INDEX idx_subscription_transactions_status ON subscription_transactions(status);


-- Auto Top-up Settings Table
CREATE TABLE IF NOT EXISTS auto_topup_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGSERIAL NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN DEFAULT false,
    default_card_id UUID REFERENCES virtual_cards(id),
    topup_strategy VARCHAR(50) DEFAULT 'subscription_plus_buffer', -- fixed, percentage, subscription_plus_buffer
    fixed_amount DECIMAL(20, 2),
    buffer_percentage DECIMAL(5, 2) DEFAULT 10.00,
    buffer_fixed_amount DECIMAL(20, 2) DEFAULT 10.00,
    min_wallet_balance_required DECIMAL(20, 2) DEFAULT 0,
    check_time_hours_before INT DEFAULT 24,
    max_topup_per_day DECIMAL(20, 2),
    daily_topup_count INT DEFAULT 0,
    last_topup_date DATE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_auto_topup_settings_user ON auto_topup_settings(user_id);
CREATE INDEX idx_auto_topup_settings_enabled ON auto_topup_settings(enabled) WHERE enabled = true;


-- Auto Top-up Logs Table
CREATE TABLE IF NOT EXISTS auto_topup_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGSERIAL NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    subscription_id UUID REFERENCES subscriptions(id),
    topup_amount DECIMAL(20, 2) NOT NULL,
    wallet_balance_before DECIMAL(20, 2),
    wallet_balance_after DECIMAL(20, 2),
    card_balance_before DECIMAL(20, 2),
    card_balance_after DECIMAL(20, 2),
    status VARCHAR(50) NOT NULL,
    failure_reason TEXT,
    transaction_id UUID REFERENCES transactions(id),
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_auto_topup_logs_user ON auto_topup_logs(user_id);
CREATE INDEX idx_auto_topup_logs_card ON auto_topup_logs(card_id);
CREATE INDEX idx_auto_topup_logs_subscription ON auto_topup_logs(subscription_id);
CREATE INDEX idx_auto_topup_logs_status ON auto_topup_logs(status);
CREATE INDEX idx_auto_topup_logs_created ON auto_topup_logs(created_at DESC);


-- Subscription Notifications Table
CREATE TABLE IF NOT EXISTS subscription_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGSERIAL NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subscription_id UUID REFERENCES subscriptions(id) ON DELETE CASCADE,
    notification_type VARCHAR(100) NOT NULL,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    action_url VARCHAR(500),
    priority VARCHAR(20) DEFAULT 'normal',
    sent_at TIMESTAMP WITH TIME ZONE,
    read_at TIMESTAMP WITH TIME ZONE,
    clicked_at TIMESTAMP WITH TIME ZONE,
    delivery_status VARCHAR(50) DEFAULT 'pending',
    delivery_channel VARCHAR(50) DEFAULT 'push', -- push, email, sms, in_app
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscription_notifications_user ON subscription_notifications(user_id);
CREATE INDEX idx_subscription_notifications_subscription ON subscription_notifications(subscription_id);
CREATE INDEX idx_subscription_notifications_type ON subscription_notifications(notification_type);
CREATE INDEX idx_subscription_notifications_unread ON subscription_notifications(user_id, read_at) WHERE read_at IS NULL;
CREATE INDEX idx_subscription_notifications_sent ON subscription_notifications(sent_at) WHERE sent_at IS NOT NULL;

-- Card Funding History Table (track funding from USD wallets to card)
CREATE TABLE IF NOT EXISTS card_funding_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGSERIAL NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES virtual_cards(id) ON DELETE CASCADE,
    wallet_id UUID NOT NULL REFERENCES swift_wallets(id),
    amount DECIMAL(20, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    funding_type VARCHAR(50) NOT NULL, -- manual, auto_topup, initial_funding
    transaction_id UUID REFERENCES transactions(id),
    ledger_entry_id UUID,
    status VARCHAR(50) NOT NULL,
    failure_reason TEXT,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_card_funding_history_user ON card_funding_history(user_id);
CREATE INDEX idx_card_funding_history_card ON card_funding_history(card_id);
CREATE INDEX idx_card_funding_history_wallet ON card_funding_history(wallet_id);
CREATE INDEX idx_card_funding_history_created ON card_funding_history(created_at DESC);


-- Subscription Spending Analytics (Materialized View for Performance)
CREATE MATERIALIZED VIEW subscription_spending_analytics AS
SELECT
    s.user_id,
    DATE_TRUNC('month', st.transaction_date) AS month,
    sc.name AS category_name,
    COUNT(DISTINCT s.id) AS active_subscriptions,
    COUNT(st.id) AS total_transactions,
    SUM(CASE WHEN st.status = 'success' THEN st.amount ELSE 0 END) AS total_spent,
    SUM(CASE WHEN st.status = 'failed' THEN 1 ELSE 0 END) AS failed_transactions,
    AVG(CASE WHEN st.status = 'success' THEN st.amount ELSE NULL END) AS avg_transaction_amount
FROM subscriptions s
JOIN subscription_transactions st ON s.id = st.subscription_id
LEFT JOIN subscription_merchants sm ON s.merchant_id = sm.id
LEFT JOIN subscription_categories sc ON sm.category_id = sc.id
WHERE s.status = 'active'
GROUP BY s.user_id, DATE_TRUNC('month', st.transaction_date), sc.name;

CREATE UNIQUE INDEX idx_subscription_spending_analytics_unique 
ON subscription_spending_analytics(user_id, month, COALESCE(category_name, ''));

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Triggers for updated_at
CREATE TRIGGER update_virtual_cards_updated_at BEFORE UPDATE ON virtual_cards
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_subscription_merchants_updated_at BEFORE UPDATE ON subscription_merchants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_subscriptions_updated_at BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_auto_topup_settings_updated_at BEFORE UPDATE ON auto_topup_settings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Function to calculate next renewal date based on historical pattern
CREATE OR REPLACE FUNCTION calculate_next_renewal_date(
    p_subscription_id UUID
) RETURNS TIMESTAMP WITH TIME ZONE AS $$
DECLARE
    v_interval_days INT;
    v_last_transaction_date TIMESTAMP WITH TIME ZONE;
BEGIN
    -- Calculate average interval between transactions
    SELECT 
        ROUND(AVG(EXTRACT(EPOCH FROM (transaction_date - LAG(transaction_date) OVER (ORDER BY transaction_date))) / 86400))::INT,
        MAX(transaction_date)
    INTO v_interval_days, v_last_transaction_date
    FROM subscription_transactions
    WHERE subscription_id = p_subscription_id
      AND status = 'success';
    
    -- If we have historical data, use calculated interval; otherwise use default 30 days
    RETURN v_last_transaction_date + INTERVAL '1 day' * COALESCE(v_interval_days, 30);
END;
$$ LANGUAGE plpgsql;






