DROP VIEW IF EXISTS system_alert_metrics;
DROP VIEW IF EXISTS user_alert_stats;

DROP FUNCTION IF EXISTS cleanup_expired_alerts;
DROP FUNCTION IF EXISTS get_active_alert_count;
DROP FUNCTION IF EXISTS validate_price_alert_config;
DROP FUNCTION IF EXISTS update_price_alert_timestamp;

DROP TABLE IF EXISTS alert_trigger_history;
DROP TABLE IF EXISTS price_alerts;