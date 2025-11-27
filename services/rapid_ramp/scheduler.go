package rapidramp

import (
	"context"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
)

type RapidRampScheduler struct {
	taskScheduler *tasks.TaskScheduler
	qrcodeService *QRCodeService
	store         *db.Store
	logger        *logging.Logger
	checkInterval time.Duration
}

func NewRapidRampScheduler(
	taskScheduler *tasks.TaskScheduler,
	qrcodeService *QRCodeService,
	store *db.Store,
	logger *logging.Logger,
	checkInterval time.Duration,
) *RapidRampScheduler {
	if checkInterval == 0 {
		checkInterval = 1 * time.Minute // Default: check every minute
	}
	return &RapidRampScheduler{
		taskScheduler: taskScheduler,
		qrcodeService: qrcodeService,
		store:         store,
		logger:        logger,
		checkInterval: checkInterval,
	}
}

func (s *RapidRampScheduler) Start() error {
	s.logger.Info("Starting rapid ramp scheduler...")

	// _, err := s.taskScheduler.AddTask(
	// 	"qr-process-confirmations",
	// 	"Process Pending QR Confirmations",
	// 	s.processPendingConfirmations,
	// 	s.checkInterval,
	// )
	// if err != nil {
	// 	s.logger.Error(fmt.Sprintf("Failed to register confirmations task: %v", err))
	// 	return err
	// }

	// Register tasks for processing conversions
	_, err := s.taskScheduler.AddTask(
		"qr-process-conversions",
		"Process Pending QR Conversions",
		s.processReadyForConversion,
		s.checkInterval,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register conversions task: %v", err))
		return err
	}

	// Register task for processing payouts
	_, err = s.taskScheduler.AddTask(
		"qr-process-payouts",
		"Process QR Payouts",
		s.processReadyForPayout,
		s.checkInterval,
	)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to register payouts task: %v", err))
		return err
	}

	// Start all tasks with 10 second initial delay
	s.taskScheduler.ScheduleTask("qr-process-confirmations", 10*time.Second)
	s.taskScheduler.ScheduleTask("qr-process-conversions", 10*time.Second)
	s.taskScheduler.ScheduleTask("qr-process-payouts", 10*time.Second)

	s.logger.Info(fmt.Sprintf("Rapid ramp scheduler started. Checking every %s", s.checkInterval))
	return nil
}

func (s *RapidRampScheduler) Stop() error {
	s.logger.Info("Stopping rapid ramp scheduler...")
	s.taskScheduler.StopTask("qr-process-confirmations")
	s.taskScheduler.StopTask("qr-process-conversions")
	s.taskScheduler.StopTask("qr-process-payouts")
	s.logger.Info("Rapid ramp scheduler stopped")
	return nil
}

// func (s *RapidRampScheduler) processPendingConfirmations(ctx context.Context) error {
// 	return s.qrcodeService.ProcessPendingConfirmations(ctx)
// }

func (s *RapidRampScheduler) processReadyForConversion(ctx context.Context) error {
	return s.qrcodeService.ProcessReadyForConversion(ctx, 50)
}

func (s *RapidRampScheduler) processReadyForPayout(ctx context.Context) error {
	return s.qrcodeService.ProcessReadyForPayout(ctx, 50)
}
