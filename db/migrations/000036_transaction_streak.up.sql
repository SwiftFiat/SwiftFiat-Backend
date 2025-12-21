/**
 * Transaction Streaks & Badges System
 * Purpose: Gamification system to track daily transaction patterns and award achievement badges
 *
 * Features:
 * - Track daily transaction streaks (current, best, total)
 * - Multiple badge tiers with unlock requirements
 * - Automatic streak calculations via triggers
 * - Historical tracking of badge achievements
 */

-- ===============================================
-- TRANSACTION STREAKS TABLE
-- ===============================================
/**
 * Table: transaction_streaks
 * Tracks daily transaction patterns for each user
 * 
 * Metrics:
 * - current_streak: Consecutive days with transactions (resets to 0 on missed day)
 * - best_streak: Longest consecutive streak ever achieved (never decreases)
 * - total_transaction_days: Total count of unique days with transactions (never decreases)
 * - last_transaction_date: Date of most recent transaction (for streak validation)
 */
 CREATE TABLE IF NOT EXISTS "transaction_streaks" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- Link to user
    "user_id" BIGINT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    
    -- Streak Metrics
    "current_streak" INT NOT NULL DEFAULT 0, -- Current consecutive days with transactions
    "best_streak" INT NOT NULL DEFAULT 0, -- Longest consecutive streak ever achieved
    "total_transaction_days" INT NOT NULL DEFAULT 0, -- Total count of unique days with transactions
    
    -- Date tracking
    "last_transaction_date" DATE, -- Date of most recent transaction (for streak validation)
    "streak_started_at" TIMESTAMPTZ, -- Date when the streak started
    
    -- Audit timestamps
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW()),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW()),
    
    -- Ensure one record per user
    CONSTRAINT "unique_user_streak" UNIQUE ("user_id")
);

-- Index
CREATE INDEX IF NOT EXISTS "idx_transaction_streaks_user_id" ON "transaction_streaks"("user_id");
CREATE INDEX IF NOT EXISTS "idx_transaction_streaks_last_transaction_date" ON "transaction_streaks"("last_transaction_date");

-- ===============================================
-- BADGES TABLE
-- ===============================================
/**
 * Table: badges
 * Defines all available achievement badges with unlock requirements
 * 
 * Badge Tiers (based on your UI):
 * - Starter (1 day)
 * - Consistent (3 days)
 * - Committed (7 days)
 * - Dedicated (14 days)
 * - Champion (30 days)
 * - Legend (60 days)
 * - Master (90 days)
 * - Elite (180 days)
 * - Titan (365 days)
 */
 CREATE TABLE IF NOT EXISTS "badges" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- Badge identification
    "name" VARCHAR(50) NOT NULL UNIQUE,
    "description" TEXT NOT NULL,
    "icon_url" TEXT, -- URL to badge icon/image
    
    -- Unlock requirements
    "required_streak_days" INT NOT NULL,
    "tier_level" INT NOT NULL, -- Ordering: 1=Starter, 2=Consistent, etc.
    
    -- Badge metadata
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "display_order" INT NOT NULL DEFAULT 0,
    
    -- Audit timestamps
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW()),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW())
);

-- Seed initial badges (based on UI mockup)
INSERT INTO "badges" ("name", "description", "required_streak_days", "tier_level", "display_order") VALUES
    ('Starter', 'Complete your first daily transaction', 1, 1, 1),
    ('Consistent', 'Maintain a 3-day transaction streak', 3, 2, 2),
    ('Committed', 'Achieve a 7-day transaction streak', 7, 3, 3),
    ('Dedicated', 'Keep going for 14 consecutive days', 14, 4, 4),
    ('Champion', 'Master the 30-day streak challenge', 30, 5, 5),
    ('Legend', 'Legendary 60-day streak achieved', 60, 6, 6),
    ('Master', 'Elite 90-day transaction streak', 90, 7, 7),
    ('Elite', 'Exceptional 180-day dedication', 180, 8, 8),
    ('Titan', 'Ultimate achievement: 365-day streak!', 365, 9, 9)
ON CONFLICT ("name") DO NOTHING;

CREATE INDEX IF NOT EXISTS "idx_badges_tier_level" ON "badges"("tier_level");
CREATE INDEX IF NOT EXISTS "idx_badges_required_days" ON "badges"("required_streak_days");

-- ===============================================
-- USER BADGES TABLE (Junction)
-- ===============================================
/**
 * Table: user_badges
 * Tracks which badges users have unlocked and when
 * 
 * Features:
 * - Historical record of badge achievements
 * - Tracks streak value when badge was earned
 * - Prevents duplicate badge awards
 */
 CREATE TABLE IF NOT EXISTS "user_badges" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- Relations
    "user_id" BIGINT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "badge_id" BIGINT NOT NULL REFERENCES "badges"("id") ON DELETE CASCADE,
    
    -- Achievement metadata
    "earned_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW()),
    "streak_at_unlock" INT NOT NULL, -- Streak value when badge was earned
    
    -- Prevent duplicate awards
    CONSTRAINT "unique_user_badge" UNIQUE ("user_id", "badge_id")
);

CREATE INDEX IF NOT EXISTS "idx_user_badges_user_id" ON "user_badges"("user_id");
CREATE INDEX IF NOT EXISTS "idx_user_badges_badge_id" ON "user_badges"("badge_id");
CREATE INDEX IF NOT EXISTS "idx_user_badges_earned_at" ON "user_badges"("earned_at");

-- ===============================================
-- TRANSACTION STREAK HISTORY
-- ===============================================
/**
 * Table: transaction_streak_history
 * Logs all streak changes for analytics and auditing
 * 
 * Useful for:
 * - Debugging streak calculations
 * - Analytics on user engagement patterns
 * - Historical trend analysis
 */
CREATE TABLE IF NOT EXISTS "transaction_streak_history" (
    "id" BIGSERIAL PRIMARY KEY,
    
    -- Relations
    "user_id" BIGINT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
    "transaction_id" UUID, -- Optional: link to specific transaction
    
    -- Snapshot of streak state
    "previous_streak" INT NOT NULL,
    "new_streak" INT NOT NULL,
    "transaction_date" DATE NOT NULL,
    "event_type" VARCHAR(50) NOT NULL, -- 'streak_continued', 'streak_broken', 'streak_started'
    
    -- Additional context
    "metadata" JSONB, -- Flexible field for additional data
    
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW())
);

CREATE INDEX IF NOT EXISTS "idx_streak_history_user_id" ON "transaction_streak_history"("user_id");
CREATE INDEX IF NOT EXISTS "idx_streak_history_transaction_date" ON "transaction_streak_history"("transaction_date");
CREATE INDEX IF NOT EXISTS "idx_streak_history_event_type" ON "transaction_streak_history"("event_type");

/*
**
 * Function: update_transaction_streak
 * 
 * Automatically updates user's streak when a transaction is completed
 * 
 * Logic:
 * 1. Get user's current streak data
 * 2. Check if transaction is on a new day
 * 3. If yesterday: increment current_streak
 * 4. If today: no change (same day transactions don't count twice)
 * 5. If gap > 1 day: reset current_streak to 1, keep best_streak
 * 6. Always increment total_transaction_days if new day
 * 7. Update best_streak if current exceeds it
 * 8. Log to history
 */
-- Drop and recreate with correct field names
DROP TRIGGER IF EXISTS transaction_streak_update ON transactions;
DROP FUNCTION IF EXISTS update_transaction_streak();

CREATE OR REPLACE FUNCTION update_transaction_streak()
RETURNS TRIGGER AS $$
DECLARE 
    v_streak_record RECORD;
    v_today DATE := CURRENT_DATE;
    v_yesterday DATE := CURRENT_DATE - INTERVAL '1 day';
    v_new_streak INT;
    v_event_type VARCHAR(50);
    v_days_since_last INT;
BEGIN
    -- Only process successful transactions
    IF NEW.status != 'successful' THEN
        RETURN NEW;
    END IF;
    
    -- Skip if user_id is null
    IF NEW.user_id IS NULL THEN
        RETURN NEW;
    END IF;
    
    -- Get or create streak record for user
    SELECT * INTO v_streak_record
    FROM transaction_streaks
    WHERE user_id = NEW.user_id  -- ✅ FIXED
    FOR UPDATE;
    
    -- Initialize if doesn't exist
    IF NOT FOUND THEN
        INSERT INTO transaction_streaks (
            user_id,
            current_streak,
            best_streak,
            total_transaction_days,
            last_transaction_date,
            streak_started_at
        ) VALUES (
            NEW.user_id,  -- ✅ FIXED
            1,
            1,
            1,
            v_today,
            NOW()
        );
        
        -- Log first transaction
        INSERT INTO transaction_streak_history (
            user_id,
            transaction_id,
            previous_streak,
            new_streak,
            transaction_date,
            event_type
        ) VALUES (
            NEW.user_id,  -- ✅ FIXED
            NEW.id,
            0,
            1,
            v_today,
            'streak_started'
        );
        
        RETURN NEW;
    END IF;
    
    -- Same day transaction - no streak change
    IF v_streak_record.last_transaction_date = v_today THEN
        RETURN NEW;
    END IF;
    
    -- Calculate days since last transaction
    v_days_since_last := v_today - v_streak_record.last_transaction_date;
    
    -- Determine new streak value and event type
    IF v_streak_record.last_transaction_date = v_yesterday THEN
        v_new_streak := v_streak_record.current_streak + 1;
        v_event_type := 'streak_continued';
    ELSIF v_days_since_last > 1 THEN
        v_new_streak := 1;
        v_event_type := 'streak_broken';
    ELSE
        v_new_streak := v_streak_record.current_streak;
        v_event_type := 'no_change';
    END IF;
    
    -- Update streak record
    UPDATE transaction_streaks
    SET 
        current_streak = v_new_streak,
        best_streak = GREATEST(v_new_streak, v_streak_record.best_streak),
        total_transaction_days = v_streak_record.total_transaction_days + 1,
        last_transaction_date = v_today,
        updated_at = NOW(),
        streak_started_at = CASE 
            WHEN v_event_type = 'streak_broken' THEN NOW()
            ELSE v_streak_record.streak_started_at
        END
    WHERE user_id = NEW.user_id;  -- ✅ FIXED
    
    -- Log streak change
    INSERT INTO transaction_streak_history (
        user_id,
        transaction_id,
        previous_streak,
        new_streak,
        transaction_date,
        event_type,
        metadata
    ) VALUES (
        NEW.user_id,  -- ✅ FIXED
        NEW.id,
        v_streak_record.current_streak,
        v_new_streak,
        v_today,
        v_event_type,
        jsonb_build_object(
            'days_since_last', v_days_since_last,
            'total_days', v_streak_record.total_transaction_days + 1
        )
    );
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to transactions table
-- Adjust table name based on your schema
DROP TRIGGER IF EXISTS transaction_streak_update ON transactions;
CREATE TRIGGER transaction_streak_update
    AFTER INSERT OR UPDATE OF status ON transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_transaction_streak();

/**
 * Function: check_and_award_badges
 * 
 * Checks if user qualifies for any new badges and awards them
 * Called after streak updates
 */
CREATE OR REPLACE FUNCTION check_and_award_badges()
RETURNS TRIGGER AS $$
DECLARE
    v_badge RECORD;
BEGIN
    -- Loop through all badges that user qualifies for but hasn't earned
    FOR v_badge IN
        SELECT b.*
        FROM badges b
        WHERE b.required_streak_days <= NEW.current_streak
        AND b.is_active = TRUE
        AND NOT EXISTS (
            SELECT 1
            FROM user_badges ub
            WHERE ub.user_id = NEW.user_id
            AND ub.badge_id = b.id
        )
        ORDER BY b.tier_level ASC
    LOOP
        -- Award badge
        INSERT INTO user_badges (
            user_id,
            badge_id,
            streak_at_unlock
        ) VALUES (
            NEW.user_id,
            v_badge.id,
            NEW.current_streak
        );
        
        -- Could trigger notification here
        -- PERFORM notify_badge_earned(NEW.user_id, v_badge.id);
    END LOOP;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to transaction_streaks
DROP TRIGGER IF EXISTS badge_award_check ON transaction_streaks;
CREATE TRIGGER badge_award_check
    AFTER INSERT OR UPDATE OF current_streak ON transaction_streaks
    FOR EACH ROW
    EXECUTE FUNCTION check_and_award_badges();

-- ===============================================
-- HELPER FUNCTION: Reset Broken Streaks (Cron Job)
-- ===============================================
/**
 * Function: reset_broken_streaks
 * 
 * Run this daily via cron to reset streaks for users who missed a day
 * Should run after midnight (e.g., 00:05 AM)
 */
CREATE OR REPLACE FUNCTION reset_broken_streaks()
RETURNS TABLE(users_affected INT) AS $$
DECLARE
    v_affected_count INT := 0;
BEGIN
    -- Reset streaks for users whose last transaction was 2+ days ago
    WITH streak_resets AS (
        UPDATE transaction_streaks
        SET 
            current_streak = 0,
            updated_at = NOW()
        WHERE last_transaction_date < CURRENT_DATE - INTERVAL '1 day'
        AND current_streak > 0
        RETURNING user_id, current_streak
    )
    SELECT COUNT(*) INTO v_affected_count FROM streak_resets;
    
    -- Log resets
    INSERT INTO transaction_streak_history (
        user_id,
        previous_streak,
        new_streak,
        transaction_date,
        event_type
    )
    SELECT 
        ts.user_id,
        ts.current_streak,
        0,
        CURRENT_DATE,
        'streak_reset_midnight'
    FROM transaction_streaks ts
    WHERE ts.last_transaction_date < CURRENT_DATE - INTERVAL '1 day'
    AND ts.current_streak > 0;
    
    RETURN QUERY SELECT v_affected_count;
END;
$$ LANGUAGE plpgsql;

-- ===============================================
-- COMMENTS
-- ===============================================
COMMENT ON TABLE transaction_streaks IS 'Tracks daily transaction streak metrics per user';
COMMENT ON TABLE badges IS 'Defines achievement badges with unlock requirements';
COMMENT ON TABLE user_badges IS 'Junction table tracking which badges users have earned';
COMMENT ON TABLE transaction_streak_history IS 'Audit log of all streak changes';
COMMENT ON FUNCTION update_transaction_streak() IS 'Trigger function to maintain streak counts on transactions';
COMMENT ON FUNCTION check_and_award_badges() IS 'Trigger function to auto-award badges when streaks reach milestones';
COMMENT ON FUNCTION reset_broken_streaks() IS 'Cron job function to reset streaks for inactive users';
