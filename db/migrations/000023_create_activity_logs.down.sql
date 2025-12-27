-- =========================
-- VIEWS
-- =========================
DROP VIEW IF EXISTS user_activity_timeline;
DROP VIEW IF EXISTS recent_critical_events;

-- =========================
-- TRIGGERS
-- =========================
DROP TRIGGER IF EXISTS trigger_prevent_audit_update ON audit_logs;
DROP TRIGGER IF EXISTS trigger_prevent_audit_delete ON audit_logs;

-- =========================
-- FUNCTIONS
-- =========================
DROP FUNCTION IF EXISTS prevent_audit_log_modification();
DROP FUNCTION IF EXISTS create_monthly_audit_partition();

-- =========================
-- PARTITIONS
-- =========================
DROP TABLE IF EXISTS audit_logs_2024_12;
DROP TABLE IF EXISTS audit_logs_2025_01;
DROP TABLE IF EXISTS audit_logs_2025_02;
DROP TABLE IF EXISTS audit_logs_default;

-- =========================
-- MAIN TABLE
-- =========================
DROP TABLE IF EXISTS audit_logs;

-- =========================
-- ENUMS
-- =========================
DROP TYPE IF EXISTS audit_severity;
DROP TYPE IF EXISTS audit_event_category;
