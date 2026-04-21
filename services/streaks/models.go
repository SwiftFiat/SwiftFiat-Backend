package streaks

import (
	"time"

	"github.com/google/uuid"
)

// ===============================================
// RESPONSE MODELS
// ===============================================

// StreakDashboard represents the complete streak dashboard response
type StreakDashboard struct {
	CurrentStreak      int32             `json:"current_streak"`
	BestStreak         int32             `json:"best_streak"`
	TotalDays          int32             `json:"total_transaction_days"`
	LastTransactionAt  *time.Time        `json:"last_transaction_at,omitempty"`
	StreakStartedAt    *time.Time        `json:"streak_started_at,omitempty"`
	IsActive           bool              `json:"is_active"` // True if transacted today
	DaysUntilReset     int32             `json:"days_until_reset"`
	Badges             []BadgeWithStatus `json:"badges"`
	NextMilestone      *NextMilestone    `json:"next_milestone,omitempty"`
	StreakHealth       StreakHealth      `json:"streak_health"`
	RecentAchievements []RecentBadge     `json:"recent_achievements"`
}

// BadgeWithStatus represents a badge with lock/unlock status
type BadgeWithStatus struct {
	BadgeID            int64      `json:"badge_id"`
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	RequiredStreakDays int32      `json:"required_streak_days"`
	TierLevel          int32      `json:"tier_level"`
	IconURL            *string    `json:"icon_url,omitempty"`
	IsUnlocked         bool       `json:"is_unlocked"`
	EarnedAt           *time.Time `json:"earned_at,omitempty"`
	StreakAtUnlock     *int32     `json:"streak_at_unlock,omitempty"`
	DaysRemaining      int32      `json:"days_remaining"`
	Progress           float64    `json:"progress"` // Percentage (0-100)
}

// NextMilestone represents the next badge target
type NextMilestone struct {
	BadgeID            int64   `json:"badge_id"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	RequiredStreakDays int32   `json:"required_streak_days"`
	DaysRemaining      int32   `json:"days_remaining"`
	Progress           float64 `json:"progress"` // Percentage
}

// StreakHealth provides engagement metrics
type StreakHealth struct {
	Status            string  `json:"status"` // "on_fire", "active", "at_risk", "broken"
	MotivationalText  string  `json:"motivational_text"`
	ConsecutiveDays   int32   `json:"consecutive_days"`
	RankPercentile    float64 `json:"rank_percentile,omitempty"` // Top X% of users
	DailyGoalAchieved bool    `json:"daily_goal_achieved"`
}

// RecentBadge represents a recently earned badge
type RecentBadge struct {
	BadgeName      string    `json:"badge_name"`
	TierLevel      int32     `json:"tier_level"`
	EarnedAt       time.Time `json:"earned_at"`
	StreakAtUnlock int32     `json:"streak_at_unlock"`
}

// LeaderboardEntry represents a user on the streak leaderboard
type LeaderboardEntry struct {
	UserID               uuid.UUID `json:"user_id"`
	FirstName            string    `json:"first_name"`
	LastName             string    `json:"last_name"`
	AvatarURL            *string   `json:"avatar_url,omitempty"`
	CurrentStreak        int32     `json:"current_streak"`
	BestStreak           int32     `json:"best_streak"`
	TotalTransactionDays int32     `json:"total_transaction_days"`
	BadgesEarned         int32     `json:"badges_earned"`
	Rank                 int32     `json:"rank"`
}

// StreakStatistics provides platform-wide metrics
type StreakStatistics struct {
	TotalUsersWithStreaks        int32   `json:"total_users_with_streaks"`
	AverageCurrentStreak         float64 `json:"average_current_streak"`
	HighestCurrentStreak         int32   `json:"highest_current_streak"`
	HighestBestStreak            int32   `json:"highest_best_streak"`
	TotalPlatformTransactionDays int64   `json:"total_platform_transaction_days"`
}
