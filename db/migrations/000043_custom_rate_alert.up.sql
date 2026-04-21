-- Migration: Add Price Alerts Feature
-- Description: Creates tables and indexes for custom price alert functionality

-- =====================================================
-- Price Alerts Table
-- =====================================================
CREATE TABLE IF NOT EXISTS price_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Currency pair
    source_currency VARCHAR(10) NOT NULL,
    target_currency VARCHAR(10) NOT NULL,
    
    -- Alert configuration
    alert_condition VARCHAR(20) NOT NULL CHECK (
        alert_condition IN ('above', 'below', 'equals', 'percent_up', 'percent_down', 'range', 'breakout')
    ),
    alert_type VARCHAR(20) NOT NULL CHECK (
        alert_type IN ('one_time', 'recurring', 'trailing')
    ),
    priority VARCHAR(20) NOT NULL DEFAULT 'medium' CHECK (
        priority IN ('low', 'medium', 'high', 'critical')
    ),
    
    -- Condition parameters (stored as numeric strings for precision)
    target_rate TEXT,                    -- For above/below/equals/breakout conditions
    percentage_change TEXT,              -- For percent_up/percent_down conditions
    range_min TEXT,                      -- For range condition
    range_max TEXT,                      -- For range condition
    baseline_rate TEXT,                  -- Reference rate for percentage and trailing alerts
    
    -- Trailing alert specific
    trailing_distance TEXT,              -- Percentage distance for trailing (e.g., "5" for 5%)
    max_trailing_rate TEXT,              -- Highest rate seen (for trailing down)
    min_trailing_rate TEXT,              -- Lowest rate seen (for trailing up)
    
    -- Metadata
    description TEXT,
    label VARCHAR(100),
    
    -- State tracking
    is_active BOOLEAN NOT NULL DEFAULT true,
    triggered_count INTEGER NOT NULL DEFAULT 0,
    last_triggered_at TIMESTAMP WITH TIME ZONE,
    last_checked_at TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE,
    
    -- Notification preferences
    notify_push BOOLEAN NOT NULL DEFAULT false,
    notify_in_app BOOLEAN NOT NULL DEFAULT true,
    
    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

-- =====================================================
-- Alert Trigger History Table
-- =====================================================
CREATE TABLE IF NOT EXISTS alert_trigger_history (
    id BIGSERIAL PRIMARY KEY,
    alert_id UUID NOT NULL REFERENCES price_alerts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Trigger details
    triggered_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    current_rate TEXT NOT NULL,          -- Rate when alert triggered
    previous_rate TEXT,                  -- Previous baseline rate
    change_percent TEXT,                 -- Percentage change from baseline
    
    -- Alert state snapshot at trigger time
    alert_condition VARCHAR(20) NOT NULL,
    target_rate TEXT,
    
    -- Notification status
    push_notification_sent BOOLEAN DEFAULT false,
    in_app_notification_sent BOOLEAN DEFAULT false,
    notification_error TEXT,
    
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- =====================================================
-- Indexes for Performance
-- =====================================================

-- Primary lookup indexes
CREATE INDEX idx_price_alerts_user_id ON price_alerts(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_price_alerts_active ON price_alerts(is_active) WHERE deleted_at IS NULL AND is_active = true;
CREATE INDEX idx_price_alerts_currency_pair ON price_alerts(source_currency, target_currency) 
    WHERE deleted_at IS NULL AND is_active = true;

-- Composite index for scheduler queries
CREATE INDEX idx_price_alerts_scheduler ON price_alerts(is_active, last_checked_at, expires_at)
    WHERE deleted_at IS NULL;

-- Index for expired alert cleanup
CREATE INDEX idx_price_alerts_expired ON price_alerts(expires_at)
    WHERE deleted_at IS NULL AND expires_at IS NOT NULL;

-- Trigger history indexes
CREATE INDEX idx_alert_trigger_history_alert_id ON alert_trigger_history(alert_id);
CREATE INDEX idx_alert_trigger_history_user_id ON alert_trigger_history(user_id);
CREATE INDEX idx_alert_trigger_history_triggered_at ON alert_trigger_history(triggered_at DESC);

-- =====================================================
-- Functions and Triggers
-- =====================================================

-- Update timestamp trigger function
CREATE OR REPLACE FUNCTION update_price_alert_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to price_alerts table
CREATE TRIGGER price_alerts_update_timestamp
    BEFORE UPDATE ON price_alerts
    FOR EACH ROW
    EXECUTE FUNCTION update_price_alert_timestamp();

-- =====================================================
-- Constraint Validations
-- =====================================================

-- Add constraint to ensure required fields based on alert_condition
CREATE OR REPLACE FUNCTION validate_price_alert_config()
RETURNS TRIGGER AS $$
BEGIN
    -- Validate based on alert_condition
    IF NEW.alert_condition IN ('above', 'below', 'equals', 'breakout') THEN
        IF NEW.target_rate IS NULL THEN
            RAISE EXCEPTION 'target_rate is required for % condition', NEW.alert_condition;
        END IF;
    END IF;
    
    IF NEW.alert_condition IN ('percent_up', 'percent_down') THEN
        IF NEW.percentage_change IS NULL THEN
            RAISE EXCEPTION 'percentage_change is required for % condition', NEW.alert_condition;
        END IF;
    END IF;
    
    IF NEW.alert_condition = 'range' THEN
        IF NEW.range_min IS NULL OR NEW.range_max IS NULL THEN
            RAISE EXCEPTION 'range_min and range_max are required for range condition';
        END IF;
    END IF;
    
    -- Validate trailing alert requirements
    IF NEW.alert_type = 'trailing' THEN
        IF NEW.trailing_distance IS NULL THEN
            RAISE EXCEPTION 'trailing_distance is required for trailing alerts';
        END IF;
        IF NEW.alert_condition NOT IN ('above', 'below') THEN
            RAISE EXCEPTION 'trailing alerts only support above and below conditions';
        END IF;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER validate_price_alert_config_trigger
    BEFORE INSERT OR UPDATE ON price_alerts
    FOR EACH ROW
    EXECUTE FUNCTION validate_price_alert_config();

-- =====================================================
-- Views for Analytics
-- =====================================================

-- View for alert statistics per user
CREATE OR REPLACE VIEW user_alert_stats AS
SELECT 
    user_id,
    COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_alerts,
    COUNT(*) FILTER (WHERE is_active = true AND deleted_at IS NULL) as active_alerts,
    COUNT(*) FILTER (WHERE is_active = false AND deleted_at IS NULL) as paused_alerts,
    SUM(triggered_count) FILTER (WHERE deleted_at IS NULL) as total_triggers,
    COUNT(DISTINCT source_currency || '-' || target_currency) FILTER (WHERE deleted_at IS NULL) as unique_pairs,
    MAX(last_triggered_at) FILTER (WHERE deleted_at IS NULL) as last_trigger_time
FROM price_alerts
GROUP BY user_id;

-- View for system-wide alert metrics
CREATE OR REPLACE VIEW system_alert_metrics AS
SELECT 
    COUNT(*) as total_alerts,
    COUNT(*) FILTER (WHERE is_active = true) as active_alerts,
    COUNT(*) FILTER (WHERE is_active = false) as inactive_alerts,
    COUNT(DISTINCT user_id) as unique_users,
    COUNT(DISTINCT source_currency || '-' || target_currency) as unique_currency_pairs,
    AVG(triggered_count) as avg_triggers_per_alert,
    COUNT(*) FILTER (WHERE alert_type = 'one_time') as one_time_alerts,
    COUNT(*) FILTER (WHERE alert_type = 'recurring') as recurring_alerts,
    COUNT(*) FILTER (WHERE alert_type = 'trailing') as trailing_alerts,
    COUNT(*) FILTER (WHERE priority = 'critical') as critical_alerts,
    COUNT(*) FILTER (WHERE priority = 'high') as high_priority_alerts,
    COUNT(*) FILTER (WHERE expires_at IS NOT NULL AND expires_at < NOW()) as expired_alerts
FROM price_alerts
WHERE deleted_at IS NULL;

-- =====================================================
-- Utility Functions
-- =====================================================

-- Function to get active alerts count
CREATE OR REPLACE FUNCTION get_active_alert_count()
RETURNS BIGINT AS $$
BEGIN
    RETURN (
        SELECT COUNT(*)
        FROM price_alerts
        WHERE is_active = true
        AND deleted_at IS NULL
        AND (expires_at IS NULL OR expires_at > NOW())
    );
END;
$$ LANGUAGE plpgsql;

-- Function to cleanup old expired alerts
CREATE OR REPLACE FUNCTION cleanup_expired_alerts(cutoff_date TIMESTAMP WITH TIME ZONE)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    WITH deleted AS (
        DELETE FROM price_alerts
        WHERE expires_at < cutoff_date
        AND is_active = false
        AND deleted_at IS NULL
        RETURNING id
    )
    SELECT COUNT(*) INTO deleted_count FROM deleted;
    
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- Sample Data (Optional - for testing)
-- =====================================================

-- Uncomment to insert sample alerts

-- INSERT INTO price_alerts (
--     user_id,
--     source_currency,
--     target_currency,
--     alert_condition,
--     alert_type,
--     priority,
--     target_rate,
--     description,
--     label
-- ) VALUES
-- (
--     1, -- Replace with actual user ID
--     'BTC',
--     'USD',
--     'above',
--     'one_time',
--     'high',
--     '100000.00',
--     'Notify when Bitcoin reaches $100k',
--     'BTC $100k Alert'
-- );


-- =====================================================
-- Grants (Adjust based on your user roles)
-- =====================================================

-- Grant permissions to application user
-- GRANT SELECT, INSERT, UPDATE, DELETE ON price_alerts TO app_user;
-- GRANT SELECT, INSERT ON alert_trigger_history TO app_user;
-- GRANT SELECT ON user_alert_stats TO app_user;
-- GRANT SELECT ON system_alert_metrics TO app_user;

-- =====================================================
-- Comments for Documentation
-- =====================================================

COMMENT ON TABLE price_alerts IS 'Stores user-configured price alerts for cryptocurrency-to-fiat rate monitoring';
COMMENT ON TABLE alert_trigger_history IS 'Historical log of when alerts were triggered';
COMMENT ON COLUMN price_alerts.alert_condition IS 'Type of price condition: above, below, equals, percent_up, percent_down, range, breakout';
COMMENT ON COLUMN price_alerts.alert_type IS 'Alert behavior: one_time (trigger once), recurring (trigger multiple times), trailing (dynamic stop-loss/buy)';
COMMENT ON COLUMN price_alerts.trailing_distance IS 'For trailing alerts: percentage distance from peak/trough (e.g., 5 for 5%)';
COMMENT ON VIEW user_alert_stats IS 'Aggregated statistics per user for alert usage';
COMMENT ON VIEW system_alert_metrics IS 'System-wide metrics for monitoring alert system health';