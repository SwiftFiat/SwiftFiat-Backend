package streaks

import (
	"context"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/google/uuid"
)

// StreakScheduler manages automated streak maintenance tasks
type StreakScheduler struct {
	store         *db.Store
	logger        *logging.Logger
	taskScheduler *tasks.TaskScheduler
	notifService  *service.Notification
	streakService *StreakService
}

// NewStreakScheduler creates a new streak scheduler
func NewStreakScheduler(
	store *db.Store,
	logger *logging.Logger,
	taskScheduler *tasks.TaskScheduler,
	notifService *service.Notification,
	streakService *StreakService,
) *StreakScheduler {
	return &StreakScheduler{
		store:         store,
		logger:        logger,
		taskScheduler: taskScheduler,
		notifService:  notifService,
		streakService: streakService,
	}
}

// Initialize sets up all scheduled tasks
func (ss *StreakScheduler) Start() error {
	ss.logger.Info("Initializing streak scheduler...")

	// Task 1: Reset broken streaks daily at midnight
	_, err := ss.taskScheduler.AddTask(
		"streak-reset-midnight",
		"Reset Broken Streaks",
		ss.resetBrokenStreaksTask,
		24*time.Hour, // Run every 24 hours
	)
	if err != nil {
		return fmt.Errorf("failed to add streak reset task: %w", err)
	}

	// Schedule first run at next midnight
	nextMidnight := ss.getNextMidnight()
	if err := ss.taskScheduler.RunAt("streak-reset-midnight", nextMidnight); err != nil {
		return fmt.Errorf("failed to schedule streak reset: %w", err)
	}

	// Task 2: Send streak reminder notifications (users at risk)
	_, err = ss.taskScheduler.AddTask(
		"streak-reminder-evening",
		"Send Streak Reminders",
		ss.sendStreakReminders,
		24*time.Hour,
	)
	if err != nil {
		return fmt.Errorf("failed to add streak reminder task: %w", err)
	}

	// Schedule reminder at 8 PM daily
	nextReminder := ss.getNextScheduledTime(20, 0) // 8 PM
	if err := ss.taskScheduler.RunAt("streak-reminder-evening", nextReminder); err != nil {
		return fmt.Errorf("failed to schedule streak reminder: %w", err)
	}

	// Task 3: Weekly analytics report
	_, err = ss.taskScheduler.AddTask(
		"streak-weekly-analytics",
		"Generate Weekly Streak Analytics",
		ss.generateWeeklyAnalytics,
		7*24*time.Hour, // Every 7 days
	)
	if err != nil {
		return fmt.Errorf("failed to add weekly analytics task: %w", err)
	}

	ss.logger.Info("Streak scheduler initialized successfully")
	return nil
}

// resetBrokenStreaksTask resets streaks for users who missed transactions
func (ss *StreakScheduler) resetBrokenStreaksTask(ctx context.Context) error {
	ss.logger.Info("Starting daily streak reset task...")
	startTime := time.Now()

	// Get users with broken streaks before reset
	brokenStreaks, err := ss.store.GetUsersWithBrokenStreaks(ctx, db.GetUsersWithBrokenStreaksParams{
		Limit:  1000,
		Offset: 0,
	})
	if err != nil {
		ss.logger.Error(fmt.Sprintf("Failed to fetch broken streaks: %v", err))
		return fmt.Errorf("failed to fetch broken streaks: %w", err)
	}

	// Send notifications to users about broken streaks
	for _, streak := range brokenStreaks {
		if streak.CurrentStreak > 0 && streak.DaysInactive >= 1 {
			// Send "streak at risk" notification
			message := fmt.Sprintf(
				"⚠️ Your %d-day streak is at risk! Make a transaction today to keep it alive.",
				streak.CurrentStreak,
			)

			// Non-blocking notification
			go func(userID int64, msg string) {
				_, err := ss.notifService.Create(ctx, int32(userID), "Streak At Risk", msg)
				if err != nil {
					ss.logger.Error(fmt.Sprintf("Failed to send streak notification to user %d: %v", userID, err))
				}
			}(streak.UserID, message)
		}
	}

	// Execute the reset
	err = ss.streakService.ResetBrokenStreaks(ctx)
	if err != nil {
		ss.logger.Error(fmt.Sprintf("Failed to reset broken streaks: %v", err))
		return fmt.Errorf("failed to reset broken streaks: %w", err)
	}

	duration := time.Since(startTime)
	ss.logger.Info(fmt.Sprintf(
		"Streak reset completed: %d users processed in %s",
		len(brokenStreaks),
		duration,
	))

	return nil
}

// sendStreakReminders sends reminders to users who haven't transacted today
func (ss *StreakScheduler) sendStreakReminders(ctx context.Context) error {
	ss.logger.Info("Sending streak reminder notifications...")

	// Get users with active streaks who haven't transacted today
	brokenStreaks, err := ss.store.GetUsersWithBrokenStreaks(ctx, db.GetUsersWithBrokenStreaksParams{
		Limit:  500,
		Offset: 0,
	})
	if err != nil {
		return fmt.Errorf("failed to fetch users for reminders: %w", err)
	}

	remindersSent := 0
	for _, streak := range brokenStreaks {
		// Only remind users with active streaks (not yet reset)
		if streak.CurrentStreak > 0 && streak.DaysInactive == 0 {
			message := fmt.Sprintf(
				"🔥 Keep your %d-day streak alive! Make a transaction before midnight.",
				streak.CurrentStreak,
			)

			// Send notification asynchronously
			go func(userID int64, msg string) {
				_, err := ss.notifService.Create(ctx, int32(userID), "Daily Streak Reminder", msg)
				if err != nil {
					ss.logger.Error(fmt.Sprintf("Failed to send reminder to user %d: %v", userID, err))
				}
			}(streak.UserID, message)

			remindersSent++
		}
	}

	ss.logger.Info(fmt.Sprintf("Sent %d streak reminder notifications", remindersSent))
	return nil
}

// generateWeeklyAnalytics generates weekly streak analytics report
func (ss *StreakScheduler) generateWeeklyAnalytics(ctx context.Context) error {
	ss.logger.Info("Generating weekly streak analytics...")

	stats, err := ss.streakService.GetPlatformStatistics(ctx)
	if err != nil {
		return fmt.Errorf("failed to get platform statistics: %w", err)
	}

	ss.logger.Info(fmt.Sprintf(
		"Weekly Streak Report: Total Users=%d, Avg Streak=%.2f, Highest Streak=%d",
		stats.TotalUsersWithStreaks,
		stats.AverageCurrentStreak,
		stats.HighestCurrentStreak,
	))

	// TODO: Send report to admin dashboard or email
	return nil
}

// ===============================================
// TRANSACTION-TRIGGERED STREAK UPDATES
// ===============================================

// UpdateStreakOnTransaction updates user streak after successful transaction
// This should be called from the transaction service after commit
func (ss *StreakScheduler) UpdateStreakOnTransaction(
	ctx context.Context,
	userID int64,
	transactionID uuid.UUID,
	transactionType string,
) error {
	ss.logger.Info(fmt.Sprintf("Updating streak for user %d after %s transaction", userID, transactionType))

	// Get current streak
	streak, err := ss.store.GetOrCreateUserStreak(ctx, userID)
	if err != nil {
		ss.logger.Error(fmt.Sprintf("Failed to get streak for user %d: %v", userID, err))
		return fmt.Errorf("failed to get streak: %w", err)
	}

	previousStreak := streak.CurrentStreak

	// Note: The database trigger will automatically update the streak
	// We just need to check if any new badges were earned

	// Wait a moment for trigger to complete
	time.Sleep(100 * time.Millisecond)

	// Fetch updated streak
	updatedStreak, err := ss.store.GetUserStreak(ctx, userID)
	if err != nil {
		ss.logger.Error(fmt.Sprintf("Failed to get updated streak: %v", err))
		return fmt.Errorf("failed to get updated streak: %w", err)
	}

	// Check if streak increased (milestone reached)
	if updatedStreak.CurrentStreak > previousStreak {
		ss.handleStreakMilestone(ctx, userID, updatedStreak.CurrentStreak, transactionType)
	}

	return nil
}

// handleStreakMilestone checks for badge unlocks and sends notifications
func (ss *StreakScheduler) handleStreakMilestone(
	ctx context.Context,
	userID int64,
	currentStreak int32,
	transactionType string,
) {
	ss.logger.Info(fmt.Sprintf("User %d reached streak %d", userID, currentStreak))

	// Check if user unlocked any new badges
	badges, err := ss.store.GetUserBadgesWithLockStatus(ctx, db.GetUserBadgesWithLockStatusParams{
		UserID:             userID,
		RequiredStreakDays: currentStreak,
	})
	if err != nil {
		ss.logger.Error(fmt.Sprintf("Failed to check badges: %v", err))
		return
	}

	// Find newly unlocked badges (just earned)
	for _, badge := range badges {
		if badge.IsUnlocked && badge.RequiredStreakDays == currentStreak {
			// This badge was just unlocked!
			message := fmt.Sprintf(
				"🎉 Congratulations! You've unlocked the '%s' badge with a %d-day streak!",
				badge.Name,
				currentStreak,
			)

			// Send celebration notification
			go func(uid int64, msg string, badgeName string) {
				_, err := ss.notifService.Create(ctx, int32(uid), "Badge Unlocked! 🏆", msg)
				if err != nil {
					ss.logger.Error(fmt.Sprintf("Failed to send badge notification: %v", err))
				}
			}(userID, message, badge.Name)

			ss.logger.Info(fmt.Sprintf("User %d unlocked badge: %s", userID, badge.Name))
		}
	}

	// Send milestone notification for significant streaks
	if currentStreak%7 == 0 || currentStreak == 1 || currentStreak == 3 {
		var message string
		switch currentStreak {
		case 1:
			message = "🎯 Great start! You've begun your transaction streak. Keep it going!"
		case 3:
			message = "🔥 3-day streak! You're building great financial habits."
		case 7:
			message = "⭐ Amazing! 7-day streak achieved. You're on fire!"
		default:
			message = fmt.Sprintf("💪 Incredible! %d-day streak! You're a streak champion!", currentStreak)
		}

		go func(uid int64, msg string) {
			_, err := ss.notifService.Create(ctx, int32(uid), "Streak Milestone", msg)
			if err != nil {
				ss.logger.Error(fmt.Sprintf("Failed to send milestone notification: %v", err))
			}
		}(userID, message)
	}
}

// ===============================================
// HELPER METHODS
// ===============================================

// getNextMidnight calculates the next midnight time
func (ss *StreakScheduler) getNextMidnight() time.Time {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 5, 0, 0, now.Location())
	return midnight
}

// getNextScheduledTime calculates next occurrence of hour:minute
func (ss *StreakScheduler) getNextScheduledTime(hour, minute int) time.Time {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}

// ScheduleStreakRecalculation schedules a recalculation for a specific user
func (ss *StreakScheduler) ScheduleStreakRecalculation(userID int64, delay time.Duration) error {
	taskID := fmt.Sprintf("recalculate-streak-%d", userID)

	recalcTask := func(ctx context.Context) error {
		ss.logger.Info(fmt.Sprintf("Recalculating streak for user %d", userID))
		_, err := ss.streakService.RecalculateUserStreak(ctx, userID)
		return err
	}

	// Add task
	_, err := ss.taskScheduler.AddTask(taskID, fmt.Sprintf("Recalculate Streak User %d", userID), recalcTask, 0)
	if err != nil {
		return err
	}

	// Schedule to run after delay and auto-remove
	return ss.taskScheduler.RunAfterAndRemove(taskID, delay)
}

// Stop gracefully stops the scheduler
func (ss *StreakScheduler) Stop() error {
	ss.logger.Info("Stopping streak scheduler...")

	// Stop all streak-related tasks
	taskIDs := []string{
		"streak-reset-midnight",
		"streak-reminder-evening",
		"streak-weekly-analytics",
	}

	for _, id := range taskIDs {
		if err := ss.taskScheduler.StopTask(id); err != nil {
			ss.logger.Error(fmt.Sprintf("Failed to stop task %s: %v", id, err))
		}
	}

	ss.logger.Info("Streak scheduler stopped")
	return nil
}
