-- =====================================================
-- RATE MANAGER MODULE - SQL SCHEMA
-- =====================================================
-- This schema supports VIP-based rate adjustments with:
-- 1. VIP Level management
-- 2. Rate adjustment rules (fixed/percentage mark-ups)
-- 3. Rate change history
-- 4. User VIP tier assignments
-- =====================================================

-- =====================================================
-- VIP LEVELS TABLE
-- =====================================================
/**
 * Stores VIP tier definitions with transaction volume thresholds
 * Used to classify users and apply differential rate mark-ups
 */
 CREATE TABLE IF NOT EXISTS "vip_levels" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
     
    -- VIP level identification
    "level_name" VARCHAR(50) UNIQUE NOT NULL, -- e.g., "VIP 1", "VIP Elite"
    "level_code" VARCHAR(20) UNIQUE NOT NULL, -- e.g., "VIP1", "ELITE"
    "level_rank" INTEGER UNIQUE NOT NULL,     -- Ordering: 1, 2, 3... (higher = better)
    
    -- Eligibility criteria
    "min_conversion_volume" DECIMAL(20, 2) NOT NULL DEFAULT 0, -- Lifetime conversion volume in USD    
    -- Metadata
    "description" TEXT,
    "benefits_description" TEXT,  -- User-facing benefits text
    "badge_color" VARCHAR(7),     -- Hex color for UI badge
    "icon_url" TEXT,              -- VIP badge icon
    
    -- Status
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "is_default" BOOLEAN NOT NULL DEFAULT FALSE, -- Only one default level allowed
    
    -- Audit
    "created_by" UUID REFERENCES users(id),
    "updated_by" UUID REFERENCES users(id),
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "deleted_at" TIMESTAMPTZ,
    
    -- Constraints
    CONSTRAINT "positive_conversion_volume" CHECK ("min_conversion_volume" >= 0),
    CONSTRAINT "valid_level_rank" CHECK ("level_rank" > 0)
);

-- Indexes for VIP levels
    CREATE UNIQUE INDEX unique_default_vip_level_idx
        ON vip_levels (is_default)
        WHERE is_default = TRUE AND deleted_at IS NULL;
CREATE INDEX "idx_vip_levels_active" ON "vip_levels"("is_active") WHERE "deleted_at" IS NULL;
CREATE INDEX "idx_vip_levels_rank" ON "vip_levels"("level_rank") WHERE "deleted_at" IS NULL;
CREATE INDEX "idx_vip_levels_volume" ON "vip_levels"("min_conversion_volume") WHERE "deleted_at" IS NULL;

-- =====================================================
-- RATE ADJUSTMENT RULES TABLE
-- =====================================================
/**
 * Defines automatic rate mark-up rules for different VIP levels
 * Supports both fixed amount and percentage-based adjustments
 */
CREATE TABLE IF NOT EXISTS "rate_adjustment_rules" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Rule identification
    "rule_name" VARCHAR(100) NOT NULL,
    "rule_description" TEXT,
    
    -- VIP level association (NULL = applies to all users)
    "vip_level_id" UUID REFERENCES "vip_levels"("id") ON DELETE CASCADE,
    "is_global_rule" BOOLEAN NOT NULL DEFAULT FALSE, -- If true, applies to all users
    
    -- Currency pair
    "source_currency" VARCHAR(10) NOT NULL, -- USD, NGN, USDT, USDC
    "target_currency" VARCHAR(10) NOT NULL,
    
    -- Adjustment type and value
    "adjustment_type" VARCHAR(20) NOT NULL, -- 'fixed' or 'percentage'
    "adjustment_value" DECIMAL(20, 8) NOT NULL, -- Amount (NGN) or percentage (e.g., 1.2 for 1.2%)
    
    -- Direction: 'add' or 'subtract' (typically 'add' for mark-up)
    "adjustment_direction" VARCHAR(10) NOT NULL DEFAULT 'add',
    
    -- Priority (higher number = higher priority when multiple rules apply)
    "priority" INTEGER NOT NULL DEFAULT 0,
    
    -- Constraints
    "min_conversion_amount" DECIMAL(20, 2), -- Rule only applies above this amount
    "max_conversion_amount" DECIMAL(20, 2), -- Rule only applies below this amount
    
    -- Validity period
    "valid_from" TIMESTAMPTZ,
    "valid_until" TIMESTAMPTZ,

    -- Status
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    
    -- Audit
    "created_by" UUID REFERENCES users(id),
    "updated_by" UUID REFERENCES users(id),
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "deleted_at" TIMESTAMPTZ,
    
    -- Constraints
    CONSTRAINT "valid_adjustment_type" CHECK ("adjustment_type" IN ('fixed', 'percentage')),
    CONSTRAINT "valid_adjustment_direction" CHECK ("adjustment_direction" IN ('add', 'subtract')),
    CONSTRAINT "positive_adjustment_value" CHECK ("adjustment_value" > 0),
    CONSTRAINT "valid_amount_range" CHECK ("max_conversion_amount" IS NULL OR "min_conversion_amount" IS NULL OR "max_conversion_amount" > "min_conversion_amount"),
    CONSTRAINT "valid_validity_period" CHECK ("valid_until" IS NULL OR "valid_from" IS NULL OR "valid_until" > "valid_from"),
    CONSTRAINT "global_or_vip_specific" CHECK (
        ("is_global_rule" = TRUE AND "vip_level_id" IS NULL) OR
        ("is_global_rule" = FALSE AND "vip_level_id" IS NOT NULL)
    )
);

-- Enforce unique active global rule per currency pair
CREATE UNIQUE INDEX unique_active_global_rule_idx
    ON rate_adjustment_rules(source_currency, target_currency, is_global_rule)
    WHERE is_global_rule = TRUE
      AND is_active = TRUE
      AND deleted_at IS NULL;

-- Indexes for rate adjustment rules
CREATE INDEX "idx_rate_rules_vip_level" ON "rate_adjustment_rules"("vip_level_id") WHERE "deleted_at" IS NULL;
CREATE INDEX "idx_rate_rules_active" ON "rate_adjustment_rules"("is_active") WHERE "deleted_at" IS NULL;
CREATE INDEX "idx_rate_rules_currency_pair" ON "rate_adjustment_rules"("source_currency", "target_currency") WHERE "deleted_at" IS NULL;
CREATE INDEX "idx_rate_rules_priority" ON "rate_adjustment_rules"("priority" DESC) WHERE "is_active" = TRUE AND "deleted_at" IS NULL;
CREATE INDEX "idx_rate_rules_global" ON "rate_adjustment_rules"("is_global_rule") WHERE "is_global_rule" = TRUE AND "deleted_at" IS NULL;


-- =====================================================
-- USER VIP ASSIGNMENTS TABLE
-- =====================================================
/**
 * Tracks user VIP level assignments and tier progression
 */
CREATE TABLE IF NOT EXISTS "user_vip_assignments" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- User and VIP level
    "user_id" UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "vip_level_id" UUID NOT NULL REFERENCES "vip_levels"("id") ON DELETE RESTRICT,
    
    -- Assignment details
    "assigned_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "assigned_by" UUID REFERENCES users(id), -- NULL if auto-assigned
    "assignment_type" VARCHAR(20) NOT NULL DEFAULT 'automatic', -- 'automatic' or 'manual'
    
    -- Metrics at time of assignment
    "total_conversion_volume" DECIMAL(20, 2) NOT NULL DEFAULT 0,    
    -- Status
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "expires_at" TIMESTAMPTZ, -- Optional expiration for temporary VIP status
    
    -- Audit
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CONSTRAINT "valid_assignment_type" CHECK ("assignment_type" IN ('automatic', 'manual', 'promotional'))

);

-- Only one active VIP assignment per user
CREATE UNIQUE INDEX unique_active_user_vip_idx
    ON user_vip_assignments(user_id)
    WHERE is_active = TRUE;

-- Indexes for user VIP assignments
CREATE INDEX "idx_user_vip_user_id" ON "user_vip_assignments"("user_id");
CREATE INDEX "idx_user_vip_level_id" ON "user_vip_assignments"("vip_level_id");
CREATE INDEX "idx_user_vip_active" ON "user_vip_assignments"("is_active", "user_id");
CREATE INDEX "idx_user_vip_expires" ON "user_vip_assignments"("expires_at") WHERE "expires_at" IS NOT NULL AND "is_active" = TRUE;


-- =====================================================
-- RATE CHANGE HISTORY TABLE
-- =====================================================
/**
 * Tracks all rate adjustments and their application
 * Provides audit trail for rate changes
 */
CREATE TABLE IF NOT EXISTS "rate_change_history" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Rate details
    "source_currency" VARCHAR(10) NOT NULL,
    "target_currency" VARCHAR(10) NOT NULL,
    
    -- Rates
    "base_rate" DECIMAL(20, 8) NOT NULL,        -- Original market rate
    "adjusted_rate" DECIMAL(20, 8) NOT NULL,     -- Final rate after mark-up
    "adjustment_amount" DECIMAL(20, 8) NOT NULL, -- Actual adjustment applied
    
    -- Rule applied
    "rule_id" UUID REFERENCES "rate_adjustment_rules"("id") ON DELETE SET NULL,
    "rule_name" VARCHAR(100),
    "vip_level_id" UUID REFERENCES "vip_levels"("id") ON DELETE SET NULL,
    "vip_level_name" VARCHAR(50),
    
    -- Context
    "rate_provider" VARCHAR(50), -- e.g., "Binance P2P", "ExchangeRate-API"
    "applied_to_user_id" UUID REFERENCES users(id) ON DELETE SET NULL,
    "conversion_id" UUID, -- Link to actual conversion if applied
    
    -- Metadata
    "change_reason" TEXT,
    "changed_by" UUID REFERENCES users(id),
    
    -- Timestamp
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Partitioning hint
    CONSTRAINT "valid_currencies" CHECK ("source_currency" != "target_currency")
);

-- Indexes for rate change history
CREATE INDEX "idx_rate_history_currency_pair" ON "rate_change_history"("source_currency", "target_currency");
CREATE INDEX "idx_rate_history_created_at" ON "rate_change_history"("created_at" DESC);
CREATE INDEX "idx_rate_history_user" ON "rate_change_history"("applied_to_user_id") WHERE "applied_to_user_id" IS NOT NULL;
CREATE INDEX "idx_rate_history_rule" ON "rate_change_history"("rule_id") WHERE "rule_id" IS NOT NULL;


-- =====================================================
-- ADMIN RATE NOTIFICATIONS TABLE
-- =====================================================
/**
 * Stores notifications for admins about rate-related events
 * (e.g., rate spikes, rule conflicts, system issues)
 */
CREATE TABLE IF NOT EXISTS "rate_admin_notifications" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Notification details
    "notification_type" VARCHAR(50) NOT NULL, -- 'rate_spike', 'rule_conflict', 'vip_upgrade', etc.
    "severity" VARCHAR(20) NOT NULL DEFAULT 'info', -- 'critical', 'warning', 'info'
    "title" VARCHAR(200) NOT NULL,
    "message" TEXT NOT NULL,
    
    -- Context
    "related_entity_type" VARCHAR(50), -- 'vip_level', 'rate_rule', 'user'
    "related_entity_id" VARCHAR(100),
    "metadata" JSONB,
    
    -- Status
    "is_read" BOOLEAN NOT NULL DEFAULT FALSE,
    "read_at" TIMESTAMPTZ,
    "read_by" UUID REFERENCES users(id),
    
    -- Timestamp
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT "valid_severity" CHECK ("severity" IN ('critical', 'warning', 'info'))
);

-- Indexes for admin notifications
CREATE INDEX "idx_rate_admin_notifs_unread" ON "rate_admin_notifications"("is_read", "created_at" DESC) WHERE "is_read" = FALSE;
CREATE INDEX "idx_rate_admin_notifs_severity" ON "rate_admin_notifications"("severity", "created_at" DESC);
CREATE INDEX "idx_rate_admin_notifs_type" ON "rate_admin_notifications"("notification_type");


-- =====================================================
-- FUNCTIONS AND TRIGGERS
-- =====================================================

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Triggers for updated_at
CREATE TRIGGER update_vip_levels_updated_at
    BEFORE UPDATE ON "vip_levels"
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_rate_rules_updated_at
    BEFORE UPDATE ON "rate_adjustment_rules"
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_user_vip_updated_at
    BEFORE UPDATE ON "user_vip_assignments"
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- SEED DEFAULT VIP LEVELS
-- =====================================================
INSERT INTO "vip_levels" (
    "level_name", 
    "level_code", 
    "level_rank", 
    "min_conversion_volume",
    "description",
    "benefits_description",
    "badge_color",
    "is_default",
    "is_active"
) VALUES 
(
    'Standard', 
    'STANDARD', 
    1, 
    0,
    'Default tier for all new users',
    'Standard conversion rates and features',
    '#6B7280',
    TRUE,
    TRUE
),
(
    'VIP 1', 
    'VIP1', 
    2, 
    500000,
    'First VIP tier - for active traders',
    'Better rates and priority support',
    '#3B82F6',
    FALSE,
    TRUE
),
(
    'VIP 2', 
    'VIP2', 
    3, 
    2000000,
    'Premium VIP tier',
    'Premium rates, dedicated support, and exclusive features',
    '#8B5CF6',
    FALSE,
    TRUE
),
(
    'VIP Elite', 
    'ELITE', 
    4, 
    10000000,
    'Highest VIP tier - for high-volume traders',
    'Best available rates, white-glove service, custom solutions',
    '#F59E0B',
    FALSE,
    TRUE
)
ON CONFLICT DO NOTHING;

-- =====================================================
-- COMMENTS
-- =====================================================
COMMENT ON TABLE "vip_levels" IS 'VIP tier definitions with transaction volume thresholds';
COMMENT ON TABLE "rate_adjustment_rules" IS 'Automatic rate mark-up rules for different VIP levels';
COMMENT ON TABLE "user_vip_assignments" IS 'User VIP level assignments and tier progression tracking';
COMMENT ON TABLE "rate_change_history" IS 'Audit trail for all rate adjustments and applications';
COMMENT ON TABLE "rate_admin_notifications" IS 'Admin notifications for rate-related events';
