/**
 * Table: audit_logs
 * Purpose: Immutable audit trail for all critical operations in the fintech system
 *
 * This table provides:
 * - Complete audit trail for compliance (KYC, transactions, account changes)
 * - Security monitoring (failed login attempts, suspicious activities)
 * - Debug capabilities (trace user actions, reproduce issues)
 * - Legal protection (prove actions taken, timestamps)
 *
 * Design principles:
 * - IMMUTABLE: No updates or deletes allowed (enforced by triggers)
 * - PARTITIONED: By timestamp for performance on large datasets
 * - INDEXED: Optimized for common query patterns
 */

 -- Event categories enum
CREATE TYPE audit_event_category AS ENUM (
    'authentication',    -- Login, logout, password changes, 2FA
    'authorization',     -- Role changes, permission grants
    'account',          -- Account creation, updates, deletions
    'transaction',      -- Payments, transfers, withdrawals
    'kyc',             -- KYC submissions, verifications, document uploads
    'card',            -- Virtual card creation, funding, freezing
    'security',        -- Suspicious activities, rate limit hits
    'compliance',      -- Regulatory actions, freezes, reports
    'system',           -- Background jobs, webhooks, integrations
    'vault', -- vault
    'giftcard',
    'rapid_ramp',
    'rate_manager',
    'streaks',
    'crypto',
    'smart_conversion',
    'support',
    'rewards'
);

-- Severity levels
CREATE TYPE audit_severity AS ENUM (
    'info',      -- Normal operations
    'warning',   -- Concerning but not critical
    'error',     -- Failed operations
    'critical'   -- Security incidents, fraud attempts
);

-- Main audit log table with partitioning
CREATE TABLE IF NOT EXISTS "audit_logs" (
    -- Unique identifier
    "id" BIGSERIAL,
    
    -- Event classification
    "event_category" audit_event_category NOT NULL,
    "event_type" VARCHAR(100) NOT NULL,  -- e.g., 'user.login.success', 'transaction.created'
    "severity" audit_severity NOT NULL DEFAULT 'info',
    
    -- Actor information (who performed the action)
    "actor_id" UUID,  -- NULL for system actions
    "actor_type" VARCHAR(50) NOT NULL,  -- 'user', 'admin', 'system', 'webhook'
    "actor_email" VARCHAR(256),  -- Denormalized for quick access
    
    -- Target information (what was affected)
    "entity_type" VARCHAR(50) NOT NULL,  -- 'user', 'transaction', 'card', etc.
    "entity_id" VARCHAR(100) NOT NULL,   -- ID of affected entity
    
    -- Request context
    "ip_address" INET,
    "user_agent" TEXT,
    "request_id" VARCHAR(100),  -- For tracing across services
    
    -- Action details
    "action" VARCHAR(100) NOT NULL,  -- 'create', 'update', 'delete', 'view', 'execute'
    "description" TEXT NOT NULL,     -- Human-readable description
    
    -- Change tracking (for updates)
    "old_values" JSONB,  -- Previous state (if applicable)
    "new_values" JSONB,  -- New state
    "metadata" JSONB,    -- Additional context (error messages, validation failures, etc.)
    
    -- Status
    "success" BOOLEAN NOT NULL DEFAULT TRUE,
    "error_message" TEXT,
    
    -- Timestamp
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Partition key (must be in PRIMARY KEY for partitioned tables)
    PRIMARY KEY ("id", "created_at")
) PARTITION BY RANGE ("created_at");

-- Create indexes for common query patterns
CREATE INDEX idx_audit_logs_actor_id ON audit_logs("actor_id", "created_at" DESC);
CREATE INDEX idx_audit_logs_entity ON audit_logs("entity_type", "entity_id", "created_at" DESC);
CREATE INDEX idx_audit_logs_event_type ON audit_logs("event_type", "created_at" DESC);
CREATE INDEX idx_audit_logs_category ON audit_logs("event_category", "created_at" DESC);
CREATE INDEX idx_audit_logs_severity ON audit_logs("severity", "created_at" DESC) WHERE severity IN ('error', 'critical');
CREATE INDEX idx_audit_logs_request_id ON audit_logs("request_id") WHERE request_id IS NOT NULL;
CREATE INDEX idx_audit_logs_ip_address ON audit_logs("ip_address", "created_at" DESC);

-- GIN index for JSONB searches
CREATE INDEX idx_audit_logs_metadata ON audit_logs USING GIN("metadata");
CREATE INDEX idx_audit_logs_new_values ON audit_logs USING GIN("new_values");

-- Initial partitions (create one per month)
CREATE TABLE audit_logs_2024_12 PARTITION OF audit_logs
    FOR VALUES FROM ('2024-12-01') TO ('2025-01-01');

CREATE TABLE audit_logs_2025_01 PARTITION OF audit_logs
    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

CREATE TABLE audit_logs_2025_02 PARTITION OF audit_logs
    FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');

-- Default partition for future dates
CREATE TABLE audit_logs_default PARTITION OF audit_logs DEFAULT;

-- Trigger to prevent updates and deletes (immutability)
CREATE OR REPLACE FUNCTION prevent_audit_log_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Audit logs are immutable. Modifications are not allowed.';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_prevent_audit_update
    BEFORE UPDATE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_log_modification();

CREATE TRIGGER trigger_prevent_audit_delete
    BEFORE DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_log_modification();

-- View for recent critical events (last 7 days)
CREATE OR REPLACE VIEW recent_critical_events AS
SELECT 
    id,
    event_category,
    event_type,
    severity,
    actor_email,
    entity_type,
    entity_id,
    description,
    ip_address,
    created_at
FROM audit_logs
WHERE 
    created_at >= NOW() - INTERVAL '7 days'
    AND severity IN ('error', 'critical')
ORDER BY created_at DESC;

-- View for user activity timeline
CREATE OR REPLACE VIEW user_activity_timeline AS
SELECT 
    al.actor_id as user_id,
    u.email,
    al.event_type,
    al.entity_type,
    al.entity_id,
    al.action,
    al.description,
    al.success,
    al.ip_address,
    al.created_at
FROM audit_logs al
LEFT JOIN users u ON al.actor_id = u.id
WHERE al.actor_type = 'user'
ORDER BY al.created_at DESC;

-- Function to automatically create monthly partitions
CREATE OR REPLACE FUNCTION create_monthly_audit_partition()
RETURNS void AS $$
DECLARE
    partition_date DATE;
    partition_name TEXT;
    start_date TEXT;
    end_date TEXT;
BEGIN
    -- Create partition for next month
    partition_date := DATE_TRUNC('month', NOW() + INTERVAL '1 month');
    partition_name := 'audit_logs_' || TO_CHAR(partition_date, 'YYYY_MM');
    start_date := partition_date::TEXT;
    end_date := (partition_date + INTERVAL '1 month')::TEXT;
    
    -- Check if partition already exists
    IF NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = partition_name
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF audit_logs FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
        RAISE NOTICE 'Created partition: %', partition_name;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Comments
COMMENT ON TABLE audit_logs IS 'Immutable audit trail for all critical system operations';
COMMENT ON COLUMN audit_logs.event_category IS 'High-level categorization for filtering';
COMMENT ON COLUMN audit_logs.event_type IS 'Specific event identifier (e.g., user.login.success)';
COMMENT ON COLUMN audit_logs.metadata IS 'Flexible JSON storage for event-specific context';
COMMENT ON COLUMN audit_logs.request_id IS 'Trace ID for correlating logs across microservices';
