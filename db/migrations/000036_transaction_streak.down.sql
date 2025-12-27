-- =====================================================
-- DOWN MIGRATION: TRANSACTION STREAKS & BADGES SYSTEM
-- =====================================================

-- -----------------------------------------------------
-- Drop triggers
-- -----------------------------------------------------
DROP TRIGGER IF EXISTS badge_award_check ON transaction_streaks;
DROP TRIGGER IF EXISTS transaction_streak_update ON transactions;

-- -----------------------------------------------------
-- Drop functions
-- -----------------------------------------------------
DROP FUNCTION IF EXISTS reset_broken_streaks();
DROP FUNCTION IF EXISTS check_and_award_badges();
DROP FUNCTION IF EXISTS update_transaction_streak();

-- -----------------------------------------------------
-- Drop indexes
-- -----------------------------------------------------
DROP INDEX IF EXISTS idx_streak_history_event_type;
DROP INDEX IF EXISTS idx_streak_history_transaction_date;
DROP INDEX IF EXISTS idx_streak_history_user_id;

DROP INDEX IF EXISTS idx_user_badges_earned_at;
DROP INDEX IF EXISTS idx_user_badges_badge_id;
DROP INDEX IF EXISTS idx_user_badges_user_id;

DROP INDEX IF EXISTS idx_badges_required_days;
DROP INDEX IF EXISTS idx_badges_tier_level;

DROP INDEX IF EXISTS idx_transaction_streaks_last_transaction_date;
DROP INDEX IF EXISTS idx_transaction_streaks_user_id;

-- -----------------------------------------------------
-- Drop tables (order matters due to foreign keys)
-- -----------------------------------------------------
DROP TABLE IF EXISTS transaction_streak_history;
DROP TABLE IF EXISTS user_badges;
DROP TABLE IF EXISTS badges;
DROP TABLE IF EXISTS transaction_streaks;
