package smartconversion

import (
	"context"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
)

type Scheduler struct {
	taskScheduler          *tasks.TaskScheduler
	store                  *db.Store
	smartConversionService *ConversionService
	logger                 *logging.Logger
	checkInterval          time.Duration
}

func NewScheduler(
	taskScheduler *tasks.TaskScheduler,
	store *db.Store,
	logger *logging.Logger,
	smartConversionService *ConversionService,
	checkInterval time.Duration,
) *Scheduler {
	if checkInterval == 0 {
		checkInterval = 1 * time.Minute // Default: check every minute
	}
	return &Scheduler{
		taskScheduler:          taskScheduler,
		store:                  store,
		smartConversionService: smartConversionService,
		logger:                 logger,
		checkInterval:          checkInterval,
	}
}

func (s *Scheduler) Start() error {
	s.logger.Info("Starting smart conversion scheduler...")

	// Register task for processing scheduled conversions
	_, err := s.taskScheduler.AddTask(
		"scheduled-smart-conversion-process",
		"Process Scheduled Smart Conversions",
		s.processScheduledSmartConversions,
		s.checkInterval,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register scheduled smart conversion task: %v", err))
		return err
	}

	// Register task for processing rate-based conversions
	_, err = s.taskScheduler.AddTask(
		"rate-based-smart-conversion-process",
		"Process Rate-Based Smart Conversions",
		s.processRateBasedSmartConversions,
		s.checkInterval,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register rate-based smart conversion task: %v", err))
		return err
	}

	// Start all tasks with 10 second initial delay
	s.taskScheduler.ScheduleTask("scheduled-smart-conversion-process", 10*time.Second)
	s.logger.Info(fmt.Sprintf("Smart conversion scheduler started. Checking every %s", s.checkInterval))
	return nil
}

func (s *Scheduler) Stop() error {
	s.logger.Info("Stopping smart conversion scheduler...")
	s.taskScheduler.StopTask("scheduled-smart-conversion-process")
	s.logger.Info("Smart conversion scheduler stopped")
	return nil
}

func (s *Scheduler) processScheduledSmartConversions(ctx context.Context) error {
	return s.smartConversionService.ExecuteScheduledConversions(ctx)
}

func (s *Scheduler) processRateBasedSmartConversions(ctx context.Context) error {
	return s.smartConversionService.CheckAndExecuteRateBasedRules(ctx)
}
