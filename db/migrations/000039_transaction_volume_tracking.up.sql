-- =====================================================
-- TRANSACTION VOLUME TRACKING SCHEMA
-- Filename: 000038_transaction_volume_tracking.up.sql
-- =====================================================

-- Table to track transaction volumes for VIP evaluation
CREATE TABLE IF NOT EXISTS "user_transaction_volumes" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- User reference
    "user_id" UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Transaction details
    "transaction_type" VARCHAR(50) NOT NULL, -- airtime, data, tv, electricity, swap, transfer
    "amount" DECIMAL(20, 2) NOT NULL,
    "currency" VARCHAR(10) NOT NULL,
    "transaction_date" DATE NOT NULL,
    
    -- Aggregation fields (updated daily by scheduler)
    "daily_volume" DECIMAL(20, 2) NOT NULL DEFAULT 0,
    "daily_count" INTEGER NOT NULL DEFAULT 0,
    
    -- Timestamps
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CONSTRAINT "positive_amount" CHECK ("amount" > 0)
);

-- Indexes for efficient queries
CREATE INDEX "idx_user_transaction_volumes_user_id" ON "user_transaction_volumes"("user_id");
CREATE INDEX "idx_user_transaction_volumes_date" ON "user_transaction_volumes"("transaction_date" DESC);
CREATE INDEX "idx_user_transaction_volumes_user_date" ON "user_transaction_volumes"("user_id", "transaction_date");
CREATE INDEX "idx_user_transaction_volumes_type" ON "user_transaction_volumes"("transaction_type");

-- Unique constraint to prevent duplicates for same user/date
CREATE UNIQUE INDEX "idx_unique_user_date_volume" 
    ON "user_transaction_volumes"("user_id", "transaction_date");

-- =====================================================
-- AGGREGATED METRICS TABLE (for faster queries)
-- =====================================================

CREATE TABLE IF NOT EXISTS "user_vip_metrics_cache" (
    "user_id" UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    
    -- Lifetime metrics
    "total_transaction_volume" DECIMAL(20, 2) NOT NULL DEFAULT 0,
    "total_conversion_count" INTEGER NOT NULL DEFAULT 0,
    
    -- Monthly metrics (rolling 30 days)
    "monthly_volume" DECIMAL(20, 2) NOT NULL DEFAULT 0,
    "monthly_count" INTEGER NOT NULL DEFAULT 0,
    
    -- Last 7 days
    "weekly_volume" DECIMAL(20, 2) NOT NULL DEFAULT 0,
    "weekly_count" INTEGER NOT NULL DEFAULT 0,
    
    -- Current VIP status
    "current_vip_level_id" UUID REFERENCES vip_levels(id) ON DELETE SET NULL,
    "current_vip_level_name" VARCHAR(50),
    "current_vip_level_rank" INTEGER,
    
    -- Activity tracking
    "last_transaction_date" TIMESTAMPTZ,
    "first_transaction_date" TIMESTAMPTZ,
    
    -- Timestamps
    "last_calculated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for user_vip_metrics_cache
CREATE INDEX "idx_vip_metrics_cache_level" ON "user_vip_metrics_cache"("current_vip_level_id") WHERE "current_vip_level_id" IS NOT NULL;
CREATE INDEX "idx_vip_metrics_cache_volume" ON "user_vip_metrics_cache"("total_transaction_volume" DESC);
CREATE INDEX "idx_vip_metrics_cache_last_tx" ON "user_vip_metrics_cache"("last_transaction_date" DESC);

-- =====================================================
-- VIP LEVEL HISTORY TABLE (track changes over time)
-- =====================================================

CREATE TABLE IF NOT EXISTS "user_vip_level_history" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- User reference
    "user_id" UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Level change details
    "previous_level_id" UUID REFERENCES vip_levels(id) ON DELETE SET NULL,
    "previous_level_name" VARCHAR(50),
    "new_level_id" UUID REFERENCES vip_levels(id) ON DELETE SET NULL,
    "new_level_name" VARCHAR(50),
    
    -- Reason for change
    "change_reason" VARCHAR(50) NOT NULL, -- 'volume_threshold', 'manual', 'expiration', 'promotional'
    "change_type" VARCHAR(20) NOT NULL, -- 'upgrade', 'downgrade', 'initial'
    
    -- Metrics at time of change
    "total_volume_at_change" DECIMAL(20, 2),
    "conversion_count_at_change" INTEGER,
    
    -- Who made the change
    "changed_by" UUID REFERENCES users(id) ON DELETE SET NULL,
    "changed_by_type" VARCHAR(20), -- 'system', 'admin', 'user'
    
    -- Timestamp
    "changed_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT "valid_change_type" CHECK ("change_type" IN ('upgrade', 'downgrade', 'initial'))
);

-- Indexes for VIP level history
CREATE INDEX "idx_vip_history_user" ON "user_vip_level_history"("user_id", "changed_at" DESC);
CREATE INDEX "idx_vip_history_date" ON "user_vip_level_history"("changed_at" DESC);
CREATE INDEX "idx_vip_history_type" ON "user_vip_level_history"("change_type");

-- =====================================================
-- FUNCTIONS AND TRIGGERS
-- =====================================================

-- Function to update user_vip_metrics_cache after transaction
CREATE OR REPLACE FUNCTION update_user_vip_metrics_cache()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO user_vip_metrics_cache (
        user_id,
        total_transaction_volume,
        total_conversion_count,
        monthly_volume,
        monthly_count,
        weekly_volume,
        weekly_count,
        last_transaction_date,
        first_transaction_date,
        last_calculated_at
    )
    SELECT 
        NEW.user_id,
        COALESCE(SUM(amount), 0),
        COUNT(*),
        COALESCE(SUM(CASE WHEN transaction_date >= CURRENT_DATE - INTERVAL '30 days' THEN amount ELSE 0 END), 0),
        COUNT(CASE WHEN transaction_date >= CURRENT_DATE - INTERVAL '30 days' THEN 1 END),
        COALESCE(SUM(CASE WHEN transaction_date >= CURRENT_DATE - INTERVAL '7 days' THEN amount ELSE 0 END), 0),
        COUNT(CASE WHEN transaction_date >= CURRENT_DATE - INTERVAL '7 days' THEN 1 END),
        MAX(transaction_date),
        MIN(transaction_date),
        NOW()
    FROM user_transaction_volumes
    WHERE user_id = NEW.user_id
    ON CONFLICT (user_id) DO UPDATE SET
        total_transaction_volume = EXCLUDED.total_transaction_volume,
        total_conversion_count = EXCLUDED.total_conversion_count,
        monthly_volume = EXCLUDED.monthly_volume,
        monthly_count = EXCLUDED.monthly_count,
        weekly_volume = EXCLUDED.weekly_volume,
        weekly_count = EXCLUDED.weekly_count,
        last_transaction_date = EXCLUDED.last_transaction_date,
        first_transaction_date = EXCLUDED.first_transaction_date,
        last_calculated_at = NOW(),
        updated_at = NOW();
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to auto-update metrics cache
CREATE TRIGGER trigger_update_vip_metrics_cache
    AFTER INSERT OR UPDATE ON user_transaction_volumes
    FOR EACH ROW
    EXECUTE FUNCTION update_user_vip_metrics_cache();

-- Function to log VIP level changes
CREATE OR REPLACE FUNCTION log_vip_level_change()
RETURNS TRIGGER AS $$
DECLARE
    v_old_level_name VARCHAR(50);
    v_new_level_name VARCHAR(50);
    v_change_type VARCHAR(20);
    v_user_metrics RECORD;
BEGIN
    -- Get level names
    IF OLD.vip_level_id IS NOT NULL THEN
        SELECT level_name INTO v_old_level_name FROM vip_levels WHERE id = OLD.vip_level_id;
    END IF;
    
    IF NEW.vip_level_id IS NOT NULL THEN
        SELECT level_name INTO v_new_level_name FROM vip_levels WHERE id = NEW.vip_level_id;
    END IF;
    
    -- Determine change type
    IF OLD.vip_level_id IS NULL THEN
        v_change_type := 'initial';
    ELSIF (SELECT level_rank FROM vip_levels WHERE id = NEW.vip_level_id) > 
          (SELECT level_rank FROM vip_levels WHERE id = OLD.vip_level_id) THEN
        v_change_type := 'upgrade';
    ELSE
        v_change_type := 'downgrade';
    END IF;
    
    -- Get user metrics
    SELECT 
        total_transaction_volume,
        total_conversion_count
    INTO v_user_metrics
    FROM user_vip_metrics_cache
    WHERE user_id = NEW.user_id;
    
    -- Insert history record
    INSERT INTO user_vip_level_history (
        user_id,
        previous_level_id,
        previous_level_name,
        new_level_id,
        new_level_name,
        change_reason,
        change_type,
        total_volume_at_change,
        conversion_count_at_change,
        changed_by,
        changed_by_type
    ) VALUES (
        NEW.user_id,
        OLD.vip_level_id,
        v_old_level_name,
        NEW.vip_level_id,
        v_new_level_name,
        NEW.assignment_type,
        v_change_type,
        v_user_metrics.total_transaction_volume,
        v_user_metrics.total_conversion_count,
        NEW.assigned_by,
        CASE 
            WHEN NEW.assigned_by IS NULL THEN 'system'
            ELSE 'admin'
        END
    );
    
    -- Update metrics cache with current VIP level
    UPDATE user_vip_metrics_cache
    SET 
        current_vip_level_id = NEW.vip_level_id,
        current_vip_level_name = v_new_level_name,
        current_vip_level_rank = (SELECT level_rank FROM vip_levels WHERE id = NEW.vip_level_id),
        updated_at = NOW()
    WHERE user_id = NEW.user_id;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to log VIP changes
CREATE TRIGGER trigger_log_vip_level_change
    AFTER UPDATE OF vip_level_id ON user_vip_assignments
    FOR EACH ROW
    WHEN (OLD.vip_level_id IS DISTINCT FROM NEW.vip_level_id)
    EXECUTE FUNCTION log_vip_level_change();

-- =====================================================
-- HELPER VIEWS
-- =====================================================

-- View for quick VIP level distribution
CREATE OR REPLACE VIEW vw_vip_distribution AS
SELECT 
    vl.id AS vip_level_id,
    vl.level_name,
    vl.level_code,
    vl.level_rank,
    COUNT(DISTINCT umc.user_id) AS user_count,
    COALESCE(SUM(umc.total_transaction_volume), 0) AS total_volume,
    COALESCE(AVG(umc.total_transaction_volume), 0) AS avg_volume_per_user,
    COALESCE(SUM(umc.monthly_volume), 0) AS monthly_volume,
    ROUND(
        COUNT(DISTINCT umc.user_id)::NUMERIC * 100.0 / 
        NULLIF((SELECT COUNT(*) FROM user_vip_metrics_cache), 0),
        2
    ) AS percentage
FROM vip_levels vl
LEFT JOIN user_vip_metrics_cache umc ON umc.current_vip_level_id = vl.id
WHERE vl.is_active = TRUE 
    AND vl.deleted_at IS NULL
GROUP BY vl.id, vl.level_name, vl.level_code, vl.level_rank
ORDER BY vl.level_rank;

-- View for VIP upgrade candidates
CREATE OR REPLACE VIEW vw_vip_upgrade_candidates AS
SELECT 
    u.id AS user_id,
    u.email,
    u.phone_number,
    umc.current_vip_level_name,
    umc.current_vip_level_rank,
    umc.total_transaction_volume,
    umc.total_conversion_count,
    umc.monthly_volume,
    next_level.level_name AS eligible_level_name,
    next_level.level_rank AS eligible_level_rank,
    next_level.id AS eligible_level_id,
    (next_level.min_conversion_volume - umc.total_transaction_volume) AS volume_to_next_level
FROM users u
JOIN user_vip_metrics_cache umc ON umc.user_id = u.id
CROSS JOIN LATERAL (
    SELECT *
    FROM vip_levels
    WHERE min_conversion_volume <= umc.total_transaction_volume
        AND is_active = TRUE
        AND deleted_at IS NULL
        AND level_rank > COALESCE(umc.current_vip_level_rank, 0)
    ORDER BY level_rank DESC
    LIMIT 1
) AS next_level
WHERE u.deleted_at IS NULL
    AND (umc.current_vip_level_id IS NULL OR next_level.id != umc.current_vip_level_id);

-- =====================================================
-- COMMENTS
-- =====================================================
COMMENT ON TABLE user_transaction_volumes IS 'Tracks individual transaction volumes for VIP level evaluation';
COMMENT ON TABLE user_vip_metrics_cache IS 'Cached aggregated metrics for fast VIP level queries';
COMMENT ON TABLE user_vip_level_history IS 'Historical record of VIP level changes';
COMMENT ON VIEW vw_vip_distribution IS 'Current distribution of users across VIP levels';
COMMENT ON VIEW vw_vip_upgrade_candidates IS 'Users eligible for VIP level upgrades';