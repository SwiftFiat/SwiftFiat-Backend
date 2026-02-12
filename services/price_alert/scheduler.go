package pricealert

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
)

// AlertScheduler manages the periodic checking of price alerts
type AlertScheduler struct {
	taskScheduler     *tasks.TaskScheduler
	store             *db.Store
	alertService      *PriceAlertService
	logger            *logging.Logger
	checkInterval     time.Duration
	batchSize         int
	concurrentWorkers int
	metrics           *AlertMetrics
	mu                sync.RWMutex
}

// AlertMetrics tracks performance metrics
type AlertMetrics struct {
	TotalChecks       int64
	SuccessfulChecks  int64
	FailedChecks      int64
	AlertsTriggered   int64
	AverageCheckTime  time.Duration
	LastCheckTime     time.Time
	ActiveAlertCount  int
	mu                sync.RWMutex
}

func NewAlertScheduler(
	taskScheduler *tasks.TaskScheduler,
	store *db.Store,
	alertService *PriceAlertService,
	logger *logging.Logger,
	checkInterval time.Duration,
) *AlertScheduler {
	if checkInterval == 0 {
		checkInterval = 30 * time.Second // Default: check every 30 seconds
	}

	return &AlertScheduler{
		taskScheduler:     taskScheduler,
		store:             store,
		alertService:      alertService,
		logger:            logger,
		checkInterval:     checkInterval,
		batchSize:         100,         // Process 100 alerts per batch
		concurrentWorkers: 5,           // Use 5 concurrent workers
		metrics:           &AlertMetrics{},
	}
}

// Start begins the alert checking scheduler
func (s *AlertScheduler) Start() error {
	s.logger.Info("Starting price alert scheduler...")

	// Register main alert checking task
	_, err := s.taskScheduler.AddTask(
		"price-alert-checker",
		"Check Price Alerts",
		s.checkAlerts,
		s.checkInterval,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register price alert task: %v", err))
		return err
	}

	// Register metrics collection task (every 5 minutes)
	_, err = s.taskScheduler.AddTask(
		"price-alert-metrics",
		"Collect Price Alert Metrics",
		s.collectMetrics,
		5*time.Minute,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register metrics task: %v", err))
		return err
	}

	// Register cleanup task for expired alerts (daily)
	_, err = s.taskScheduler.AddTask(
		"price-alert-cleanup",
		"Cleanup Expired Alerts",
		s.cleanupExpiredAlerts,
		24*time.Hour,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register cleanup task: %v", err))
		return err
	}

	// Start all tasks with initial delay
	s.taskScheduler.ScheduleTask("price-alert-checker", 5*time.Second)
	s.taskScheduler.ScheduleTask("price-alert-metrics", 1*time.Minute)
	s.taskScheduler.ScheduleTask("price-alert-cleanup", 10*time.Second)

	s.logger.Info(fmt.Sprintf("Price alert scheduler started. Check interval: %s", s.checkInterval))
	return nil
}

// Stop halts the scheduler
func (s *AlertScheduler) Stop() error {
	s.logger.Info("Stopping price alert scheduler...")
	
	s.taskScheduler.StopTask("price-alert-checker")
	s.taskScheduler.StopTask("price-alert-metrics")
	s.taskScheduler.StopTask("price-alert-cleanup")
	
	s.logger.Info("Price alert scheduler stopped")
	return nil
}

// checkAlerts is the main task function for checking alerts
func (s *AlertScheduler) checkAlerts(ctx context.Context) error {
	startTime := time.Now()
	
	s.metrics.mu.Lock()
	s.metrics.TotalChecks++
	s.metrics.LastCheckTime = startTime
	s.metrics.mu.Unlock()

	s.logger.Debug("Starting price alert check cycle...")

	// Get count of active alerts
	count, err := s.store.GetActiveAlertCount(ctx)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get active alert count: %v", err))
		s.recordFailedCheck()
		return err
	}

	s.metrics.mu.Lock()
	s.metrics.ActiveAlertCount = int(count)
	s.metrics.mu.Unlock()

	if count == 0 {
		s.logger.Debug("No active alerts to check")
		s.recordSuccessfulCheck(time.Since(startTime))
		return nil
	}

	s.logger.Info(fmt.Sprintf("Checking %d active price alerts", count))

	// Use the alertService to check all alerts
	// The service already implements batching and optimization
	if err := s.alertService.CheckAlerts(ctx); err != nil {
		s.logger.Error(fmt.Sprintf("Error checking alerts: %v", err))
		s.recordFailedCheck()
		return err
	}

	duration := time.Since(startTime)
	s.recordSuccessfulCheck(duration)
	
	s.logger.Info(fmt.Sprintf("Alert check cycle completed in %v", duration))
	return nil
}

// collectMetrics gathers and logs performance metrics
func (s *AlertScheduler) collectMetrics(ctx context.Context) error {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	s.logger.Info(fmt.Sprintf(
		"Alert Metrics - Total: %d, Success: %d, Failed: %d, Triggered: %d, Active: %d, Avg Time: %v",
		s.metrics.TotalChecks,
		s.metrics.SuccessfulChecks,
		s.metrics.FailedChecks,
		s.metrics.AlertsTriggered,
		s.metrics.ActiveAlertCount,
		s.metrics.AverageCheckTime,
	))

	// Could send metrics to monitoring service here
	return nil
}

// cleanupExpiredAlerts removes old expired alerts
func (s *AlertScheduler) cleanupExpiredAlerts(ctx context.Context) error {
	s.logger.Info("Running expired alert cleanup...")

	// Delete alerts that expired more than 7 days ago
	cutoffTime := time.Now().AddDate(0, 0, -7)
	
	count, err := s.store.DeleteExpiredAlerts(ctx, sql.NullTime{Time: cutoffTime, Valid: true})
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to cleanup expired alerts: %v", err))
		return err
	}

	s.logger.Info(fmt.Sprintf("Cleaned up %d expired alerts", count))
	return nil
}

// recordSuccessfulCheck updates metrics for successful check
func (s *AlertScheduler) recordSuccessfulCheck(duration time.Duration) {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	
	s.metrics.SuccessfulChecks++
	
	// Update average check time (simple moving average)
	if s.metrics.AverageCheckTime == 0 {
		s.metrics.AverageCheckTime = duration
	} else {
		s.metrics.AverageCheckTime = (s.metrics.AverageCheckTime + duration) / 2
	}
}

// recordFailedCheck updates metrics for failed check
func (s *AlertScheduler) recordFailedCheck() {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	
	s.metrics.FailedChecks++
}

// recordAlertTriggered increments triggered alert counter
func (s *AlertScheduler) recordAlertTriggered() {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	
	s.metrics.AlertsTriggered++
}

// TriggerManualCheck manually triggers alert checking
func (s *AlertScheduler) TriggerManualCheck(ctx context.Context) error {
	s.logger.Info("Manually triggering price alert check")
	return s.checkAlerts(ctx)
}

// GetStats returns current scheduler statistics
func (s *AlertScheduler) GetStats(ctx context.Context) map[string]interface{} {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	stats := map[string]interface{}{
		"check_interval":      s.checkInterval.String(),
		"batch_size":          s.batchSize,
		"concurrent_workers":  s.concurrentWorkers,
		"total_checks":        s.metrics.TotalChecks,
		"successful_checks":   s.metrics.SuccessfulChecks,
		"failed_checks":       s.metrics.FailedChecks,
		"alerts_triggered":    s.metrics.AlertsTriggered,
		"active_alert_count":  s.metrics.ActiveAlertCount,
		"average_check_time":  s.metrics.AverageCheckTime.String(),
		"last_check_time":     s.metrics.LastCheckTime,
	}

	// Add task info
	tasks := []string{"price-alert-checker", "price-alert-metrics", "price-alert-cleanup"}
	taskStats := make(map[string]interface{})

	for _, taskID := range tasks {
		task, err := s.taskScheduler.GetTask(taskID)
		if err != nil {
			taskStats[taskID] = "not found"
			continue
		}
		var nextRun interface{}
		if task.IsRecurring && !task.LastRun.IsZero() {
			nextRun = task.LastRun.Add(task.Interval)
		} else {
			nextRun = nil
		}
		
		taskStats[taskID] = map[string]interface{}{
			"last_run":    task.LastRun,
			"is_active":   task.IsRecurring,
			"next_run":    nextRun,
		}
	}
	stats["tasks"] = taskStats

	return stats
}

// UpdateCheckInterval dynamically adjusts the check interval
func (s *AlertScheduler) UpdateCheckInterval(interval time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if interval < 10*time.Second {
		return fmt.Errorf("interval must be at least 10 seconds")
	}

	s.checkInterval = interval
	s.logger.Info(fmt.Sprintf("Updated check interval to %s", interval))

	// Reschedule the task with new interval
	s.taskScheduler.StopTask("price-alert-checker")
	
	_, err := s.taskScheduler.AddTask(
		"price-alert-checker",
		"Check Price Alerts",
		s.checkAlerts,
		interval,
	)
	if err != nil {
		return err
	}

	s.taskScheduler.ScheduleTask("price-alert-checker", 5*time.Second)
	return nil
}

// GetMetrics returns current metrics snapshot
func (s *AlertScheduler) GetMetrics() *AlertMetrics {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	// Return a copy
	return &AlertMetrics{
		TotalChecks:       s.metrics.TotalChecks,
		SuccessfulChecks:  s.metrics.SuccessfulChecks,
		FailedChecks:      s.metrics.FailedChecks,
		AlertsTriggered:   s.metrics.AlertsTriggered,
		AverageCheckTime:  s.metrics.AverageCheckTime,
		LastCheckTime:     s.metrics.LastCheckTime,
		ActiveAlertCount:  s.metrics.ActiveAlertCount,
	}
}

// ResetMetrics clears all metrics (useful for testing)
func (s *AlertScheduler) ResetMetrics() {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()

	s.metrics.TotalChecks = 0
	s.metrics.SuccessfulChecks = 0
	s.metrics.FailedChecks = 0
	s.metrics.AlertsTriggered = 0
	s.metrics.AverageCheckTime = 0
	s.metrics.LastCheckTime = time.Time{}
	s.metrics.ActiveAlertCount = 0

	s.logger.Info("Alert metrics reset")
}