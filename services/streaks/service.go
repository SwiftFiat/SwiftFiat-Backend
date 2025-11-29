package streaks

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
)

type StreakService struct {
	store  *db.Store
	logger *logging.Logger
}

func NewStreakService(store *db.Store, logger *logging.Logger) *StreakService {
	return &StreakService{
		store:  store,
		logger: logger,
	}
}

// GetUserStreakDashboard retrieves comprehensive streak dashboard for a user
func (s *StreakService) GetUserStreakDashboard(ctx context.Context, userID int64) (*StreakDashboard, error) {
	s.logger.Info(fmt.Sprintf("fetching streak dashboard for user: %d", userID))

	// Get or create streak record
	streak, err := s.store.GetOrCreateUserStreak(ctx, userID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("error fetching user streak: %v", err))
		return nil, fmt.Errorf("failed to fetch streak data: %w", err)
	}

	// Get all badges with lock status
	badgesRows, err := s.store.GetUserBadgesWithLockStatus(ctx, db.GetUserBadgesWithLockStatusParams{
		UserID:             userID,
		RequiredStreakDays: streak.CurrentStreak,
	})
	if err != nil {
		s.logger.Error(fmt.Sprintf("error fetching badges: %v", err))
		return nil, fmt.Errorf("failed to fetch badges: %w", err)
	}

	// Transform badges
	badges := make([]BadgeWithStatus, 0, len(badgesRows))
	for _, b := range badgesRows {
		progress := float64(0)
		if b.RequiredStreakDays > 0 {
			if b.IsUnlocked {
				progress = 100.0
			} else {
				progress = (float64(streak.CurrentStreak) / float64(b.RequiredStreakDays)) * 100
				if progress > 100 {
					progress = 100
				}
			}
		}

		badge := BadgeWithStatus{
			BadgeID:            b.BadgeID,
			Name:               b.Name,
			Description:        b.Description,
			RequiredStreakDays: b.RequiredStreakDays,
			TierLevel:          b.TierLevel,
			IconURL:            &b.IconUrl.String,
			IsUnlocked:         b.IsUnlocked,
			EarnedAt:           &b.EarnedAt.Time,
			StreakAtUnlock:     &b.StreakAtUnlock.Int32,
			DaysRemaining:      b.DaysRemaining,
			Progress:           progress,
		}
		badges = append(badges, badge)
	}

	// Get next milestone
	var nextMilestone *NextMilestone
	nextBadge, err := s.store.GetNextMilestone(ctx, db.GetNextMilestoneParams{
		RequiredStreakDays: int32(streak.CurrentStreak),
		UserID:             userID,
	})
	if err == nil {
		daysRemaining := nextBadge.RequiredStreakDays - streak.CurrentStreak
		progress := (float64(streak.CurrentStreak) / float64(nextBadge.RequiredStreakDays)) * 100
		nextMilestone = &NextMilestone{
			BadgeID:            nextBadge.ID,
			Name:               nextBadge.Name,
			Description:        nextBadge.Description,
			RequiredStreakDays: nextBadge.RequiredStreakDays,
			DaysRemaining:      daysRemaining,
			Progress:           progress,
		}
	}

	// Get recent badge achievements
	recentBadges, err := s.GetRecentUserAchievements(ctx, userID, 5)
	if err != nil {
		s.logger.Warn(fmt.Sprintf("failed to fetch recent achievements: %v", err))
		recentBadges = []RecentBadge{}
	}

	// Calculate streak health
	health := s.calculateStreakHealth(streak)

	// Calculate days until reset
	daysUntilReset := s.calculateDaysUntilReset(streak.LastTransactionDate)

	// Check if active today
	isActive := false
	if streak.LastTransactionDate.Valid {
		isActive = streak.LastTransactionDate.Time.Format("2006-01-02") == time.Now().Format("2006-01-02")
	}

	dashboard := &StreakDashboard{
		CurrentStreak:      streak.CurrentStreak,
		BestStreak:         streak.BestStreak,
		TotalDays:          streak.TotalTransactionDays,
		LastTransactionAt:  &streak.LastTransactionDate.Time,
		StreakStartedAt:    &streak.StreakStartedAt.Time,
		IsActive:           isActive,
		DaysUntilReset:     int32(daysUntilReset),
		Badges:             badges,
		NextMilestone:      nextMilestone,
		StreakHealth:       health,
		RecentAchievements: recentBadges,
	}

	s.logger.Info(fmt.Sprintf("streak dashboard built successfully for user: %d", userID))
	return dashboard, nil
}

// GetRecentUserAchievements gets recently earned badges for a user
func (s *StreakService) GetRecentUserAchievements(ctx context.Context, userID int64, limit int) ([]RecentBadge, error) {
	badges, err := s.store.GetUserBadges(ctx, userID)
	if err != nil {
		return nil, err
	}

	recent := make([]RecentBadge, 0)
	count := 0
	for _, b := range badges {
		if count >= limit {
			break
		}
		recent = append(recent, RecentBadge{
			BadgeName:      b.BadgeName,
			TierLevel:      b.TierLevel,
			EarnedAt:       b.EarnedAt,
			StreakAtUnlock: b.StreakAtUnlock,
		})
		count++
	}

	return recent, nil
}

// calculateStreakHealth determines user's engagement level
func (s *StreakService) calculateStreakHealth(streak db.TransactionStreak) StreakHealth {
	health := StreakHealth{
		ConsecutiveDays:   streak.CurrentStreak,
		DailyGoalAchieved: false,
	}

	// Check if transacted today
	if streak.LastTransactionDate.Valid {
		today := time.Now().Format("2006-01-02")
		lastTxDate := streak.LastTransactionDate.Time.Format("2006-01-02")
		health.DailyGoalAchieved = today == lastTxDate
	}

	// Determine status and motivation
	switch {
	case streak.CurrentStreak >= 30:
		health.Status = "on_fire"
		health.MotivationalText = "🔥 You're unstoppable! Keep the momentum going!"
	case streak.CurrentStreak >= 7:
		health.Status = "active"
		health.MotivationalText = "💪 Great consistency! You're building strong habits."
	case streak.CurrentStreak >= 1:
		health.Status = "active"
		health.MotivationalText = "⭐ Nice start! Every journey begins with a single step."
	case !streak.LastTransactionDate.Valid:
		health.Status = "new"
		health.MotivationalText = "🚀 Make your first transaction to start your streak!"
	default:
		// Streak is broken (current_streak = 0 but has history)
		health.Status = "broken"
		if streak.BestStreak > 0 {
			health.MotivationalText = fmt.Sprintf("💔 You lost your %d-day streak. Start fresh today!", streak.BestStreak)
		} else {
			health.MotivationalText = "🌟 Time to rebuild! Every expert was once a beginner."
		}
	}

	// Calculate percentile (optional - requires platform stats)
	// TODO: Implement percentile ranking if needed
	// health.RankPercentile = s.calculatePercentile(ctx, streak.CurrentStreak)

	return health
}

// calculateDaysUntilReset calculates how many days until streak resets
func (s *StreakService) calculateDaysUntilReset(lastTxDate sql.NullTime) int {
	if !lastTxDate.Valid {
		return 0
	}

	today := time.Now().Truncate(24 * time.Hour)
	lastTx := lastTxDate.Time.Truncate(24 * time.Hour)
	daysSince := int(today.Sub(lastTx).Hours() / 24)

	if daysSince >= 1 {
		return 0 // Already broken or will break tonight
	}

	return 1 // Have today to make a transaction
}

// ===============================================
// LEADERBOARD & ANALYTICS
// ===============================================

// GetStreakLeaderboard retrieves top users by current streak
func (s *StreakService) GetStreakLeaderboard(ctx context.Context, limit, offset int32) ([]LeaderboardEntry, error) {
	s.logger.Info("fetching streak leaderboard")

	rows, err := s.store.GetTopStreakLeaderboard(ctx, db.GetTopStreakLeaderboardParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
	}

	entries := make([]LeaderboardEntry, 0, len(rows))
	for i, row := range rows {
		entry := LeaderboardEntry{
			UserID:               row.UserID,
			FirstName:            row.FirstName.String,
			LastName:             row.LastName.String,
			AvatarURL:            &row.AvatarUrl.String,
			CurrentStreak:        row.CurrentStreak,
			BestStreak:           row.BestStreak,
			TotalTransactionDays: row.TotalTransactionDays,
			BadgesEarned:         row.BadgesEarned,
			Rank:                 offset + int32(i) + 1,
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetPlatformStatistics retrieves platform-wide streak statistics
func (s *StreakService) GetPlatformStatistics(ctx context.Context) (*StreakStatistics, error) {
	s.logger.Info("fetching platform streak statistics")

	stats, err := s.store.GetStreakStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch statistics: %w", err)
	}

	value, err := strconv.ParseFloat(stats.AvgCurrentStreak, 64)
	if err != nil {
		return nil, err
	}

	return &StreakStatistics{
		TotalUsersWithStreaks:        stats.TotalUsersWithStreaks,
		AverageCurrentStreak:         value,
		HighestCurrentStreak:         stats.HighestCurrentStreak,
		HighestBestStreak:            stats.HighestBestStreak,
		TotalPlatformTransactionDays: stats.TotalPlatformTransactionDays,
	}, nil
}

// ===============================================
// BADGE OPERATIONS
// ===============================================

// GetAllBadges retrieves all available badges
func (s *StreakService) GetAllBadges(ctx context.Context) ([]db.Badge, error) {
	return s.store.GetAllBadges(ctx)
}

// GetUserBadgesWithProgress retrieves user's badge progress
func (s *StreakService) GetUserBadgesWithProgress(ctx context.Context, userID int64) ([]BadgeWithStatus, error) {
	streak, err := s.store.GetUserStreak(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch streak: %w", err)
	}

	badgesRows, err := s.store.GetUserBadgesWithLockStatus(ctx, db.GetUserBadgesWithLockStatusParams{
		UserID:               userID,
		RequiredStreakDays:   int32(streak.CurrentStreak),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch badges: %w", err)
	}

	badges := make([]BadgeWithStatus, 0, len(badgesRows))
	for _, b := range badgesRows {
		progress := float64(0)
		if b.RequiredStreakDays > 0 {
			if b.IsUnlocked {
				progress = 100.0
			} else {
				progress = (float64(streak.CurrentStreak) / float64(b.RequiredStreakDays)) * 100
				if progress > 100 {
					progress = 100
				}
			}
		}
		badge := BadgeWithStatus{
			BadgeID:            b.BadgeID,
			Name:               b.Name,
			Description:        b.Description,
			RequiredStreakDays: b.RequiredStreakDays,
			TierLevel:          b.TierLevel,
			IconURL:            &b.IconUrl.String,
			IsUnlocked:         b.IsUnlocked,
			EarnedAt:           &b.EarnedAt.Time,
			StreakAtUnlock:     &b.StreakAtUnlock.Int32,
			DaysRemaining:      b.DaysRemaining,
			Progress:           progress,
		}
		badges = append(badges, badge)
	}

	return badges, nil
}

// ===============================================
// ADMIN/MAINTENANCE OPERATIONS
// ===============================================

// RecalculateUserStreak recalculates streak from transaction history
// Useful for fixing data inconsistencies
func (s *StreakService) RecalculateUserStreak(ctx context.Context, userID int64) (*db.TransactionStreak, error) {
	s.logger.Info(fmt.Sprintf("recalculating streak for user: %d", userID))

	streak, err := s.store.RecalculateUserStreak(ctx, sql.NullInt64{Int64: userID, Valid: true})
	if err != nil {
		s.logger.Error(fmt.Sprintf("error recalculating streak: %v", err))
		return nil, fmt.Errorf("failed to recalculate streak: %w", err)
	}

	s.logger.Info(fmt.Sprintf("streak recalculated successfully for user: %d", userID))
	return &streak, nil
}

// ResetBrokenStreaks manually resets streaks for users who missed transactions
// This is typically called by a cron job
func (s *StreakService) ResetBrokenStreaks(ctx context.Context) error {
	s.logger.Info("resetting broken streaks")

	err := s.store.BulkResetBrokenStreaks(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("error resetting broken streaks: %v", err))
		return fmt.Errorf("failed to reset broken streaks: %w", err)
	}

	s.logger.Info("broken streaks reset successfully")
	return nil
}

// GetSystemHealthCheck provides health metrics for monitoring
func (s *StreakService) GetSystemHealthCheck(ctx context.Context) (db.GetSystemHealthCheckRow, error) {
	return s.store.GetSystemHealthCheck(ctx)
}

// ===============================================
// NOTIFICATION HELPERS
// ===============================================

// NotifyStreakMilestone sends notification when user reaches milestone
func (s *StreakService) NotifyStreakMilestone(ctx context.Context, userID int64, streak int, badgeName string) error {
	s.logger.Info(fmt.Sprintf("notifying user %d of milestone: %d days, badge: %s", userID, streak, badgeName))
	// TODO: Integrate with notification service
	// Example: s.notificationService.SendStreakNotification(userID, streak, badgeName)
	return nil
}

// NotifyStreakAtRisk sends notification when user's streak is at risk
func (s *StreakService) NotifyStreakAtRisk(ctx context.Context, userID int64, streak int) error {
	s.logger.Info(fmt.Sprintf("notifying user %d of at-risk streak: %d days", userID, streak))
	// TODO: Integrate with notification service
	return nil
}