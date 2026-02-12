-- ============================================================================
-- REWARD POINTS SYSTEM
-- ============================================================================
-- This migration creates the reward points system that allows users to earn
-- points from bill payments and redeem them for future bill payments.
-- Key features:
-- - 1 Point = ₦1 (NGN)
-- - Non-withdrawable and non-transferable
-- - Admin-configurable reward rates
-- - Full transaction history tracking
-- ============================================================================

-- ============================================================================
-- 1. Update users table with reward balance tracking
-- ============================================================================
ALTER TABLE users
    ADD COLUMN reward_balance DECIMAL(10, 2) NOT NULL DEFAULT 0,
    ADD COLUMN total_reward_earned DECIMAL(10, 2) NOT NULL DEFAULT 0,
    ADD COLUMN total_reward_redeemed DECIMAL(10, 2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN users.reward_balance IS 'Current available reward points balance (₦ value)';
COMMENT ON COLUMN users.total_reward_earned IS 'Lifetime total reward points earned';
COMMENT ON COLUMN users.total_reward_redeemed IS 'Lifetime total reward points redeemed';

-- Add check constraint to ensure reward_balance is never negative
ALTER TABLE users
    ADD CONSTRAINT users_reward_balance_check CHECK (reward_balance >= 0);

-- Add check constraint to ensure total_reward_earned is never negative
ALTER TABLE users
    ADD CONSTRAINT users_total_reward_earned_check CHECK (total_reward_earned >= 0);

-- Add check constraint to ensure total_reward_redeemed is never negative
ALTER TABLE users
    ADD CONSTRAINT users_total_reward_redeemed_check CHECK (total_reward_redeemed >= 0);


-- ============================================================================
-- 2. Create reward_configurations table (Admin Control)
-- ============================================================================
-- This table stores admin-defined reward rate configurations
-- Admins can set different earning rates and activate/deactivate policies
CREATE TABLE reward_configurations (
    id BIGSERIAL PRIMARY KEY,
    
    -- Configuration name for identification
    config_name VARCHAR(255) NOT NULL,
    
    -- Reward rate: e.g., 0.01 means earn 1% in reward points per ₦1 spent
    -- Example: ₦100 spent = ₦1 reward points (1%)
    -- Example: ₦100 spent = ₦2 reward points (2%) with rate = 0.02
    reward_rate DECIMAL(5, 4) NOT NULL,
    
    -- Transaction types eligible for rewards
    transaction_type VARCHAR(50) NOT NULL CHECK (transaction_type IN ('bill_payment', 'inapp_transfer', 'conversion')),
    
    -- Minimum transaction amount to earn rewards (in Naira)
    min_transaction_amount DECIMAL(10, 2) NOT NULL DEFAULT 0,
    
    -- Maximum points that can be earned per transaction
    max_points_per_transaction DECIMAL(10, 2),
    
    -- Status: active or inactive
    is_active BOOLEAN NOT NULL DEFAULT true,
    
    -- Validity period
    valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMPTZ,
    
    -- Audit fields
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Ensure reward_rate is positive
    CONSTRAINT reward_rate_check CHECK (reward_rate > 0 AND reward_rate <= 1),
    
    -- Ensure min_transaction_amount is non-negative
    CONSTRAINT min_transaction_amount_check CHECK (min_transaction_amount >= 0)
);
COMMENT ON TABLE reward_configurations IS 'Admin-defined reward rate configurations';
COMMENT ON COLUMN reward_configurations.reward_rate IS 'Percentage of transaction amount earned as reward (e.g., 0.01 = 1%)';
COMMENT ON COLUMN reward_configurations.transaction_type IS 'Type of transaction eligible for rewards (e.g., bill_payment)';
COMMENT ON COLUMN reward_configurations.min_transaction_amount IS 'Minimum transaction amount to earn rewards (Naira)';
COMMENT ON COLUMN reward_configurations.max_points_per_transaction IS 'Maximum reward points per single transaction';

-- Index for finding active configurations efficiently
CREATE INDEX idx_reward_configurations_active ON reward_configurations(is_active, valid_from, valid_until)
    WHERE is_active = true;

-- Index for transaction type lookups
CREATE INDEX idx_reward_configurations_type ON reward_configurations(transaction_type, is_active);

-- Ensure only one active configuration per transaction type
-- This constraint is enforced at the application level to allow for future flexibility (e.g., time-based configurations)
CREATE UNIQUE INDEX unique_active_configuration_per_type
ON reward_configurations (transaction_type)
WHERE is_active = true;


-- ============================================================================
-- 3. Create reward_transactions table (Transaction History)
-- ============================================================================
-- This table tracks all reward point earning and redemption activities
-- Provides complete audit trail for user reward points
CREATE TABLE reward_transactions (
    id BIGSERIAL PRIMARY KEY, 
    -- User reference
    user_id BIGSERIAL NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Related transaction reference (bill payment, etc.)
    transaction_id UUID REFERENCES transactions(id) ON DELETE SET NULL,
    
    -- Transaction type: 'earned' or 'redeemed'
    transaction_type VARCHAR(20) NOT NULL,
    
    -- Source transaction type (bill_payment, etc.)
    source_transaction_type VARCHAR(50),
    
    -- Original transaction amount (in Naira)
    transaction_amount DECIMAL(10, 2),
    
    -- Reward points involved (positive for earned, negative for redeemed too)
    points_amount DECIMAL(10, 2) NOT NULL,
    
    -- Equivalent Naira value (1 point = ₦1)
    naira_value DECIMAL(10, 2) NOT NULL,
    
    -- Reward configuration used (for earned transactions)
    reward_config_id BIGINT REFERENCES reward_configurations(id) ON DELETE SET NULL,
    
    -- Description of the transaction
    description TEXT,
    
    -- Status: 'completed', 'pending', 'reversed', 'failed'
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
     
    -- Balance after this transaction
    balance_after DECIMAL(10, 2) NOT NULL,
    
    -- Metadata for additional information (JSONB for flexibility)
    metadata JSONB,
    
    -- Audit fields
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Ensure points_amount is positive
    CONSTRAINT points_amount_check CHECK (points_amount > 0),
    
    -- Ensure naira_value is positive
    CONSTRAINT naira_value_check CHECK (naira_value > 0),
    
    -- Ensure balance_after is non-negative
    CONSTRAINT balance_after_check CHECK (balance_after >= 0),
    
    -- Ensure transaction_type is valid
    CONSTRAINT transaction_type_check CHECK (transaction_type IN ('earned', 'redeemed')),
    
    -- Ensure status is valid
    CONSTRAINT status_check CHECK (status IN ('completed', 'pending', 'reversed', 'failed'))
);

COMMENT ON TABLE reward_transactions IS 'Complete history of reward points earned and redeemed';
COMMENT ON COLUMN reward_transactions.transaction_type IS 'Type: earned or redeemed';
COMMENT ON COLUMN reward_transactions.points_amount IS 'Reward points involved in transaction';
COMMENT ON COLUMN reward_transactions.naira_value IS 'Equivalent Naira value (1 point = ₦1)';
COMMENT ON COLUMN reward_transactions.balance_after IS 'User reward balance after this transaction';

-- Index for user reward history lookups (most common query)
CREATE INDEX idx_reward_transactions_user_created ON reward_transactions(user_id, created_at DESC);

-- Index for transaction reference lookups
CREATE INDEX idx_reward_transactions_transaction_id ON reward_transactions(transaction_id);

-- Index for transaction type filtering
CREATE INDEX idx_reward_transactions_type ON reward_transactions(user_id, transaction_type);

-- Index for status queries
CREATE INDEX idx_reward_transactions_status ON reward_transactions(status, created_at DESC);

-- Composite index for filtering by type and date range
CREATE INDEX idx_reward_transactions_user_type_date ON reward_transactions(user_id, transaction_type, created_at DESC);

-- ============================================================================
-- 4. Create reward_redemptions table (Detailed Redemption Records)
-- ============================================================================
-- This table provides additional details specifically for reward redemptions
-- Links redemptions to the actual bill payment transactions
CREATE TABLE reward_redemptions (
    id BIGSERIAL PRIMARY KEY, 
    
    -- Reference to the reward transaction
    reward_transaction_id BIGINT NOT NULL REFERENCES reward_transactions(id) ON DELETE CASCADE,
    
    -- User who redeemed
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Bill payment transaction where points were redeemed
    bill_payment_transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    
    -- Points redeemed
    points_redeemed DECIMAL(10, 2) NOT NULL,
    
    -- Discount amount applied (₦ value)
    discount_amount DECIMAL(10, 2) NOT NULL,
    
    -- Original bill amount before discount
    original_bill_amount DECIMAL(10, 2) NOT NULL,
    
    -- Final amount paid after discount
    final_amount_paid DECIMAL(10, 2) NOT NULL,
    
    -- Service type (e.g., 'airtime', 'data', 'electricity', 'tv')
    service_type VARCHAR(50),
    
    -- Service provider (e.g., 'MTN', 'DSTV', 'EKEDC')
    service_provider VARCHAR(100),
    
    -- Audit fields
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Ensure points_redeemed matches discount_amount (1:1 ratio)
    CONSTRAINT points_discount_match CHECK (points_redeemed = discount_amount),
    
    -- Ensure final amount is correct
    CONSTRAINT final_amount_check CHECK (final_amount_paid = original_bill_amount - discount_amount),
    
    -- Ensure all amounts are positive
    CONSTRAINT redemption_amounts_check CHECK (
        points_redeemed > 0 AND 
        discount_amount > 0 AND 
        original_bill_amount > 0 AND 
        final_amount_paid >= 0
    )
);

COMMENT ON TABLE reward_redemptions IS 'Detailed records of reward point redemptions on bill payments';
COMMENT ON COLUMN reward_redemptions.points_redeemed IS 'Reward points redeemed (₦ value)';
COMMENT ON COLUMN reward_redemptions.discount_amount IS 'Discount applied to bill payment (₦ value)';

-- Index for user redemption history
CREATE INDEX idx_reward_redemptions_user ON reward_redemptions(user_id, redeemed_at DESC);

-- Index for bill payment lookups
CREATE INDEX idx_reward_redemptions_bill_payment ON reward_redemptions(bill_payment_transaction_id);

-- Index for reward transaction reference
CREATE INDEX idx_reward_redemptions_reward_tx ON reward_redemptions(reward_transaction_id);

-- ============================================================================
-- 5. Insert default reward configuration
-- ============================================================================
-- Insert a default configuration: earn 1% reward points on bill payments
-- This can be updated by admins through the admin dashboard
INSERT INTO reward_configurations (
    config_name,
    reward_rate,
    transaction_type,
    min_transaction_amount,
    is_active,
    valid_from
) VALUES (
    'Default Bill Payment Rewards',
    0.01, -- 1% reward rate
    'bill_payment',
    0, -- No minimum transaction amount
    true,
    NOW()
);

INSERT INTO reward_configurations (
    config_name,
    reward_rate,
    transaction_type,
    min_transaction_amount,
    is_active,
    valid_from
) VALUES (
    'Default inapp transfer Rewards',
    0.01, -- 1% reward rate
    'inapp_transfer',
    0, -- No minimum transaction amount
    true,
    NOW()
);

INSERT INTO reward_configurations (
    config_name,
    reward_rate,
    transaction_type,
    min_transaction_amount,
    is_active,
    valid_from
) VALUES (
    'Default conversion Rewards',
    0.01, -- 1% reward rate
    'conversion',
    0, -- No minimum transaction amount
    true,
    NOW()
);

-- ============================================================================
-- 7. Create function to redeem reward points
-- ============================================================================
-- This function handles the redemption of reward points during bill payment
-- It validates the balance, deducts points, and creates transaction records
CREATE OR REPLACE FUNCTION redeem_reward_points(
    p_user_id INTEGER,
    p_points_to_redeem DECIMAL(10, 2),
    p_bill_transaction_id BIGINT,
    p_original_bill_amount DECIMAL(10, 2),
    p_service_type VARCHAR(50) DEFAULT NULL,
    p_service_provider VARCHAR(100) DEFAULT NULL
)
RETURNS BIGINT -- Returns the reward_transaction_id
LANGUAGE plpgsql
AS $$
DECLARE
    v_current_balance DECIMAL(10, 2);
    v_reward_transaction_id BIGINT;
    v_redemption_id BIGINT;
    v_new_balance DECIMAL(10, 2);
    v_final_amount DECIMAL(10, 2);
BEGIN
    -- Validate points_to_redeem is positive
    IF p_points_to_redeem <= 0 THEN
        RAISE EXCEPTION 'Points to redeem must be greater than zero';
    END IF;
    
    -- Get current reward balance
    SELECT reward_balance INTO v_current_balance
    FROM users
    WHERE id = p_user_id;
    
    -- Check if user exists
    IF NOT FOUND THEN
        RAISE EXCEPTION 'User not found';
    END IF;
    
    -- Check if user has sufficient balance
    IF v_current_balance < p_points_to_redeem THEN
        RAISE EXCEPTION 'Insufficient reward balance. Available: %, Requested: %', 
                        v_current_balance, p_points_to_redeem;
    END IF;
    
    -- Check if redemption amount exceeds bill amount
    IF p_points_to_redeem > p_original_bill_amount THEN
        RAISE EXCEPTION 'Cannot redeem more points than bill amount. Bill: %, Points: %', 
                        p_original_bill_amount, p_points_to_redeem;
    END IF;
    
    -- Calculate final amount after discount
    v_final_amount := p_original_bill_amount - p_points_to_redeem;
    
    -- Update user's reward balance and totals
    UPDATE users
    SET reward_balance = reward_balance - p_points_to_redeem,
        total_reward_redeemed = total_reward_redeemed + p_points_to_redeem,
        updated_at = NOW()
    WHERE id = p_user_id
    RETURNING reward_balance INTO v_new_balance;
    
    -- Create reward transaction record
    INSERT INTO reward_transactions (
        user_id,
        transaction_id,
        transaction_type,
        source_transaction_type,
        transaction_amount,
        points_amount,
        naira_value,
        description,
        status,
        balance_after
    ) VALUES (
        p_user_id,
        p_bill_transaction_id,
        'redeemed',
        'bill_payment',
        p_original_bill_amount,
        p_points_to_redeem,
        p_points_to_redeem, -- 1 point = ₦1
        format('Redeemed %s reward points on bill payment', p_points_to_redeem),
        'completed',
        v_new_balance
    )
    RETURNING id INTO v_reward_transaction_id;
    
    -- Create detailed redemption record
    INSERT INTO reward_redemptions (
        reward_transaction_id,
        user_id,
        bill_payment_transaction_id,
        points_redeemed,
        discount_amount,
        original_bill_amount,
        final_amount_paid,
        service_type,
        service_provider
    ) VALUES (
        v_reward_transaction_id,
        p_user_id,
        p_bill_transaction_id,
        p_points_to_redeem,
        p_points_to_redeem, -- 1 point = ₦1 discount
        p_original_bill_amount,
        v_final_amount,
        p_service_type,
        p_service_provider
    )
    RETURNING id INTO v_redemption_id;
    
    RETURN v_reward_transaction_id;
END;
$$;

COMMENT ON FUNCTION redeem_reward_points IS 'Redeems reward points and applies discount to bill payment';
