-- =====================================================
-- DOWN MIGRATION: TRANSACTION VOLUME TRACKING SCHEMA
-- Filename: 000038_transaction_volume_tracking.down.sql
-- =====================================================

-- -----------------------------------------------------
-- Drop views
-- -----------------------------------------------------
DROP VIEW IF EXISTS vw_vip_upgrade_candidates;
DROP VIEW IF EXISTS vw_vip_distribution;

-- -----------------------------------------------------
-- Drop triggers
-- -----------------------------------------------------
DROP TRIGGER IF EXISTS trigger_log_vip_level_change ON user_vip_assignments;
DROP TRIGGER IF EXISTS trigger_update_vip_metrics_cache ON user_transaction_volumes;

-- -----------------------------------------------------
-- Drop functions
-- -----------------------------------------------------
DROP FUNCTION IF EXISTS log_vip_level_change();
DROP FUNCTION IF EXISTS update_user_vip_metrics_cache();

-- -----------------------------------------------------
-- Drop tables (order matters due to FKs)
-- -----------------------------------------------------
DROP TABLE IF EXISTS user_vip_level_history;
DROP TABLE IF EXISTS user_vip_metrics_cache;
DROP TABLE IF EXISTS user_transaction_volumes;
