
-- =====================================================
-- DOWN MIGRATION: RATE MANAGER MODULE
-- =====================================================

-- -----------------------------------------------------
-- Drop triggers
-- -----------------------------------------------------
DROP TRIGGER IF EXISTS update_user_vip_updated_at ON user_vip_assignments;
DROP TRIGGER IF EXISTS update_rate_rules_updated_at ON rate_adjustment_rules;
DROP TRIGGER IF EXISTS update_vip_levels_updated_at ON vip_levels;

-- -----------------------------------------------------
-- Drop shared trigger function
-- -----------------------------------------------------
DROP FUNCTION IF EXISTS update_updated_at_column();

-- -----------------------------------------------------
-- Drop indexes (rate_admin_notifications)
-- -----------------------------------------------------
DROP INDEX IF EXISTS idx_rate_admin_notifs_type;
DROP INDEX IF EXISTS idx_rate_admin_notifs_severity;
DROP INDEX IF EXISTS idx_rate_admin_notifs_unread;

-- -----------------------------------------------------
-- Drop indexes (rate_change_history)
-- -----------------------------------------------------
DROP INDEX IF EXISTS idx_rate_history_rule;
DROP INDEX IF EXISTS idx_rate_history_user;
DROP INDEX IF EXISTS idx_rate_history_created_at;
DROP INDEX IF EXISTS idx_rate_history_currency_pair;

-- -----------------------------------------------------
-- Drop indexes (user_vip_assignments)
-- -----------------------------------------------------
DROP INDEX IF EXISTS idx_user_vip_expires;
DROP INDEX IF EXISTS idx_user_vip_active;
DROP INDEX IF EXISTS idx_user_vip_level_id;
DROP INDEX IF EXISTS idx_user_vip_user_id;
DROP INDEX IF EXISTS unique_active_user_vip_idx;

-- -----------------------------------------------------
-- Drop indexes (rate_adjustment_rules)
-- -----------------------------------------------------
DROP INDEX IF EXISTS idx_rate_rules_global;
DROP INDEX IF EXISTS idx_rate_rules_priority;
DROP INDEX IF EXISTS idx_rate_rules_currency_pair;
DROP INDEX IF EXISTS idx_rate_rules_active;
DROP INDEX IF EXISTS idx_rate_rules_vip_level;
DROP INDEX IF EXISTS unique_active_global_rule_idx;

-- -----------------------------------------------------
-- Drop indexes (vip_levels)
-- -----------------------------------------------------
DROP INDEX IF EXISTS idx_vip_levels_volume;
DROP INDEX IF EXISTS idx_vip_levels_rank;
DROP INDEX IF EXISTS idx_vip_levels_active;
DROP INDEX IF EXISTS unique_default_vip_level_idx;

-- -----------------------------------------------------
-- Drop tables (order matters due to FKs)
-- -----------------------------------------------------
DROP TABLE IF EXISTS rate_admin_notifications;
DROP TABLE IF EXISTS rate_change_history;
DROP TABLE IF EXISTS user_vip_assignments;
DROP TABLE IF EXISTS rate_adjustment_rules;
DROP TABLE IF EXISTS vip_levels;
