-- ============================================================================
-- ROLLBACK: REWARD POINTS SYSTEM
-- ============================================================================
-- This migration rolls back all reward points system changes
-- ============================================================================

-- Drop functions
DROP FUNCTION IF EXISTS redeem_reward_points(INTEGER, DECIMAL, BIGINT, DECIMAL, VARCHAR, VARCHAR);
DROP FUNCTION IF EXISTS calculate_and_award_reward_points(INTEGER, BIGINT, DECIMAL, VARCHAR);

-- Drop tables (in reverse order due to foreign key dependencies)
DROP TABLE IF EXISTS reward_redemptions CASCADE;
DROP TABLE IF EXISTS reward_transactions CASCADE;
DROP TABLE IF EXISTS reward_configurations CASCADE;

-- Remove columns from users table
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_total_reward_redeemed_check,
    DROP CONSTRAINT IF EXISTS users_total_reward_earned_check,
    DROP CONSTRAINT IF EXISTS users_reward_balance_check,
    DROP COLUMN IF EXISTS total_reward_redeemed,
    DROP COLUMN IF EXISTS total_reward_earned,
    DROP COLUMN IF EXISTS reward_balance;

