package subscriptions

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
)

type Scheduler struct {
	taskScheduler *tasks.TaskScheduler
	subscription  *Service
	store         *db.Store
	logger        *logging.Logger
	checkInterval time.Duration // How often to check for due deposits
}

func NewScheduler(
	taskScheduler *tasks.TaskScheduler,
	subscriptions *Service,
	store *db.Store,
	logger *logging.Logger,
	checkInterval time.Duration,
) *Scheduler {
	if checkInterval == 0 {
		checkInterval = 1 * time.Minute // Default: check every minute
	}

	return &Scheduler{
		taskScheduler: taskScheduler,
		subscription:  subscriptions,
		store:         store,
		logger:        logger,
		checkInterval: checkInterval,
	}
}

const (
	// Task IDs
	TaskRenewalReminders3Days   = "subscription_renewal_reminders_3d"
	TaskRenewalReminders1Day    = "subscription_renewal_reminders_1d"
	TaskRenewalRemindersSameDay = "subscription_renewal_reminders_0d"
	TaskAutoTopUp               = "subscription_auto_topup"
	TaskPendingReminders        = "subscription_pending_reminders"
	TaskMonthlySpendReset       = "subscription_monthly_spend_reset"
	TaskDailySpendReset         = "subscription_daily_spend_reset"
)

func (s *Scheduler) Start() error {
	s.logger.Info("Starting subscription scheduler...")

	// Register all tasks
	if err := s.registerTasks(); err != nil {
		return fmt.Errorf("failed to register tasks: %w", err)
	}

	// Schedule recurring tasks
	if err := s.scheduleRecurringTasks(); err != nil {
		return fmt.Errorf("failed to schedule tasks: %w", err)
	}

	// Schedule one-time tasks (spend resets at specific times)
	if err := s.scheduleDailySpendReset(); err != nil {
		return fmt.Errorf("failed to schedule daily spend reset: %w", err)
	}

	s.logger.Info("Subscription scheduler started successfully")
	return nil
}

// registerTasks registers all subscription tasks with the task scheduler
func (s *Scheduler) registerTasks() error {
	// Renewal Reminders - 3 days before
	_, err := s.taskScheduler.AddTask(
		TaskRenewalReminders3Days,
		"Subscription Renewal Reminders (3 days)",
		s.processRenewalReminders3Days,
		6*time.Hour, // Run every 6 hours
	)
	if err != nil {
		return fmt.Errorf("failed to add 3-day reminder task: %w", err)
	}

	// Renewal Reminders - 1 day before
	_, err = s.taskScheduler.AddTask(
		TaskRenewalReminders1Day,
		"Subscription Renewal Reminders (1 day)",
		s.processRenewalReminders1Day,
		6*time.Hour,
	)
	if err != nil {
		return fmt.Errorf("failed to add 1-day reminder task: %w", err)
	}

	// Auto Top-up
	_, err = s.taskScheduler.AddTask(
		TaskAutoTopUp,
		"Subscription Auto Top-up",
		s.processAutoTopUp,
		12*time.Hour, // Run every 12 hours
	)
	if err != nil {
		return fmt.Errorf("failed to add auto top-up task: %w", err)
	}

	// Pending Reminders
	_, err = s.taskScheduler.AddTask(
		TaskPendingReminders,
		"Send Pending Subscription Reminders",
		s.processPendingReminders,
		5*time.Minute, // Run every 5 minutes
	)
	if err != nil {
		return fmt.Errorf("failed to add pending reminders task: %w", err)
	}

	// Monthly Spend Reset (will be scheduled at month boundaries)
	_, err = s.taskScheduler.AddTask(
		TaskMonthlySpendReset,
		"Reset Monthly Subscription Spending",
		s.processMonthlySpendReset,
		0, // Not recurring, will be scheduled at specific times
	)
	if err != nil {
		return fmt.Errorf("failed to add monthly spend reset task: %w", err)
	}

	// Daily Spend Reset
	_, err = s.taskScheduler.AddTask(
		TaskDailySpendReset,
		"Reset Daily Subscription Spending",
		s.processDailySpendReset,
		24*time.Hour, // Run every 24 hours
	)
	if err != nil {
		return fmt.Errorf("failed to add daily spend reset task: %w", err)
	}

	s.logger.Info("All subscription tasks registered successfully")

	return nil
}

// scheduleRecurringTasks schedules all recurring tasks to start after 5 seconds
func (s *Scheduler) scheduleRecurringTasks() error {
	tasks := []string{
		TaskRenewalReminders3Days,
		TaskRenewalReminders1Day,
		TaskRenewalRemindersSameDay,
		TaskAutoTopUp,
		TaskPendingReminders,
		TaskDailySpendReset,
	}

	for _, taskID := range tasks {
		if err := s.taskScheduler.ScheduleTask(taskID, 5*time.Second); err != nil {
			return fmt.Errorf("failed to schedule task %s: %w", taskID, err)
		}
		s.logger.Info(fmt.Sprintf("Scheduled recurring task: %s", taskID))
	}

	return nil
}

// scheduleDailySpendReset schedules daily spend reset at midnight UTC
func (s *Scheduler) scheduleDailySpendReset() error {
	now := time.Now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	duration := nextMidnight.Sub(now)

	if err := s.taskScheduler.RunAt(TaskDailySpendReset, nextMidnight); err != nil {
		return fmt.Errorf("failed to schedule daily spend reset: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Scheduled daily spend reset at %s (in %s)",
		nextMidnight.Format(time.RFC3339), duration))

	return nil
}

// scheduleNextMonthlyReset schedules the monthly reset for the next month
func (s *Scheduler) scheduleNextMonthlyReset() error {
	now := time.Now().UTC()

	// Calculate first day of next month at 00:00:00 UTC
	firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)

	if err := s.taskScheduler.RunAt(TaskMonthlySpendReset, firstOfNextMonth); err != nil {
		return fmt.Errorf("failed to schedule monthly spend reset: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Scheduled monthly spend reset at %s",
		firstOfNextMonth.Format(time.RFC3339)))

	return nil
}

// processRenewalReminders3Days processes renewal reminders for subscriptions due in 3 days
func (s *Scheduler) processRenewalReminders3Days(ctx context.Context) error {
	s.logger.Info("Processing 3-day renewal reminders...")

	if err := s.subscription.ProcessRenewalReminders(ctx, "3", 100); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to process 3-day reminders: %v", err))
		return err
	}

	s.logger.Info("3-day renewal reminders processed successfully")
	return nil
}

// processRenewalReminders1Day processes renewal reminders for subscriptions due in 1 day
func (s *Scheduler) processRenewalReminders1Day(ctx context.Context) error {
	s.logger.Info("Processing 1-day renewal reminders...")

	if err := s.subscription.ProcessRenewalReminders(ctx, "1", 100); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to process 1-day reminders: %v", err))
		return err
	}

	s.logger.Info("1-day renewal reminders processed successfully")
	return nil
}

// processRenewalRemindersSameDay processes renewal reminders for subscriptions due today
func (s *Scheduler) processRenewalRemindersSameDay(ctx context.Context) error {
	s.logger.Info("Processing same-day renewal reminders...")

	if err := s.subscription.ProcessRenewalReminders(ctx, "0", 100); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to process same-day reminders: %v", err))
		return err
	}

	s.logger.Info("Same-day renewal reminders processed successfully")
	return nil
}

// processAutoTopUp checks and performs auto top-up for cards with upcoming renewals
func (s *Scheduler) processAutoTopUp(ctx context.Context) error {
	s.logger.Info("Processing auto top-up checks...")

	if err := s.subscription.CheckAndAutoTopUp(ctx); err != nil {
		s.logger.Error(fmt.Sprintf("Auto top-up check failed: %v", err))
		return err
	}

	s.logger.Info("Auto top-up processing completed successfully")
	return nil
}

// processPendingReminders sends all pending reminder notifications
func (s *Scheduler) processPendingReminders(ctx context.Context) error {
	reminders, err := s.store.GetPendingReminders(ctx, 50)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get pending reminders: %v", err))
		return err
	}

	if len(reminders) == 0 {
		// No pending reminders, nothing to do
		return nil
	}

	s.logger.Info(fmt.Sprintf("Sending %d pending reminders", len(reminders)))

	successCount := 0
	failureCount := 0

	for _, reminder := range reminders {
		if err := s.sendReminder(ctx, reminder); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to send reminder %s: %v", reminder.ID, err))
			failureCount++
			continue
		}

		// Update reminder status to sent
		_, err := s.store.UpdateReminderStatus(ctx, db.UpdateReminderStatusParams{
			ID:     reminder.ID,
			Status: "sent",
		})
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to update reminder status: %v", err))
		} else {
			successCount++
		}
	}

	s.logger.Info(fmt.Sprintf("Reminder processing completed: %d sent, %d failed",
		successCount, failureCount))

	return nil
}

// sendReminder sends a reminder notification through configured channels
func (s *Scheduler) sendReminder(ctx context.Context, reminder db.GetPendingRemindersRow) error {
	// TODO: Integrate with actual notification service
	// For now, just log the reminder
	s.logger.Info(fmt.Sprintf("Sending reminder to user %d: %s - %s",
		reminder.UserID, reminder.Title, reminder.Message))

	// Example integration points:
	// 1. Push Notifications (FCM/APNS)
	// 2. Email Service (SendGrid/AWS SES)
	// 3. SMS Service (Twilio)

	// For channels in reminder.Channels:
	// - "push" -> Send push notification
	// - "email" -> Send email
	// - "sms" -> Send SMS

	return nil
}

// processMonthlySpendReset resets monthly spending counters
func (s *Scheduler) processMonthlySpendReset(ctx context.Context) error {
	now := time.Now().UTC()
	currentMonth := now.Format("2006-01")

	s.logger.Info(fmt.Sprintf("Resetting monthly spending counters for month: %s", currentMonth))

	if err := s.store.ResetMonthlySpending(ctx, sql.NullString{String: currentMonth, Valid: true}); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to reset monthly spending: %v", err))
		return err
	}

	s.logger.Info("Monthly spending counters reset successfully")

	// Schedule next month's reset
	if err := s.scheduleNextMonthlyReset(); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to schedule next monthly reset: %v", err))
	}

	return nil
}

// processDailySpendReset resets daily spending counters
func (s *Scheduler) processDailySpendReset(ctx context.Context) error {
	now := time.Now().UTC()
	currentDay := now.Format("2006-01-02")

	s.logger.Info(fmt.Sprintf("Resetting daily spending counters for day: %s", currentDay))

	if err := s.store.ResetDailySpending(ctx, sql.NullString{String: currentDay, Valid: true}); err != nil {
		s.logger.Error(fmt.Sprintf("Failed to reset daily spending: %v", err))
		return err
	}

	s.logger.Info("Daily spending counters reset successfully")

	// Check if it's the first of the month, trigger monthly reset too
	if now.Day() == 1 {
		s.logger.Info("First day of month detected, triggering monthly spend reset")
		if err := s.processMonthlySpendReset(ctx); err != nil {
			s.logger.Error(fmt.Sprintf("Monthly spend reset failed: %v", err))
		}
	}

	return nil
}

// Stop stops all scheduled subscription tasks
func (s *Scheduler) Stop() error {
	s.logger.Info("Stopping subscription scheduler...")

	tasks := []string{
		TaskRenewalReminders3Days,
		TaskRenewalReminders1Day,
		TaskRenewalRemindersSameDay,
		TaskAutoTopUp,
		TaskPendingReminders,
		TaskMonthlySpendReset,
		TaskDailySpendReset,
	}

	for _, taskID := range tasks {
		if err := s.taskScheduler.StopTask(taskID); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to stop task %s: %v", taskID, err))
		}
	}

	s.logger.Info("Subscription scheduler stopped")
	return nil
}

// GetTaskStatus returns the status of all subscription tasks
func (s *Scheduler) GetTaskStatus() map[string]interface{} {
	status := make(map[string]interface{})

	tasks := []string{
		TaskRenewalReminders3Days,
		TaskRenewalReminders1Day,
		TaskRenewalRemindersSameDay,
		TaskAutoTopUp,
		TaskPendingReminders,
		TaskMonthlySpendReset,
		TaskDailySpendReset,
	}

	for _, taskID := range tasks {
		task, err := s.taskScheduler.GetTask(taskID)
		if err != nil {
			status[taskID] = map[string]interface{}{
				"status": "error",
				"error":  err.Error(),
			}
			continue
		}

		status[taskID] = map[string]interface{}{
			"name":      task.Name,
			"last_run":  task.LastRun,
			"recurring": task.IsRecurring,
			"interval":  task.Interval.String(),
			"status":    "running",
		}
	}

	return status
}

// HealthCheck returns health status of the subscription scheduler
func (s *Scheduler) HealthCheck() map[string]any {
	return map[string]any{
		"status":    "running",
		"timestamp": time.Now().UTC(),
		"tasks":     s.GetTaskStatus(),
	}
}

// RunTaskNow manually triggers a specific task to run immediately
func (s *Scheduler) RunTaskNow(taskID string) error {
	s.logger.Info(fmt.Sprintf("Manually triggering task: %s", taskID))
	return s.taskScheduler.RunTask(taskID)
}

// RunRenewalRemindersNow manually triggers renewal reminder processing
func (s *Scheduler) RunRenewalRemindersNow() error {
	ctx := context.Background()

	s.logger.Info("Manually triggering all renewal reminders...")

	// Run all three reminder intervals
	if err := s.processRenewalReminders3Days(ctx); err != nil {
		s.logger.Error(fmt.Sprintf("3-day reminders failed: %v", err))
	}

	if err := s.processRenewalReminders1Day(ctx); err != nil {
		s.logger.Error(fmt.Sprintf("1-day reminders failed: %v", err))
	}

	if err := s.processRenewalRemindersSameDay(ctx); err != nil {
		s.logger.Error(fmt.Sprintf("Same-day reminders failed: %v", err))
	}

	s.logger.Info("Manual renewal reminders processing completed")
	return nil
}

// RunAutoTopUpNow manually triggers auto top-up processing
func (s *Scheduler) RunAutoTopUpNow() error {
	s.logger.Info("Manually triggering auto top-up...")
	return s.processAutoTopUp(context.Background())
}

// RunPendingRemindersNow manually triggers pending reminder sending
func (s *Scheduler) RunPendingRemindersNow() error {
	s.logger.Info("Manually triggering pending reminders...")
	return s.processPendingReminders(context.Background())
}
