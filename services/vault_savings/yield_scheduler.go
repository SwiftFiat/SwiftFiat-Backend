package vaultsavings

import (
	"context"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
	"github.com/google/uuid"
)

// ============================================================================
// YIELD SCHEDULER
// ============================================================================

// YieldScheduler manages automated yield calculations for all vaults
// Runs daily to calculate and credit interest/yields based on vault balances
type YieldScheduler struct {
	taskScheduler *tasks.TaskScheduler
	yieldService  *YieldService
	store         *db.Store
	logger        *logging.Logger

	// Configuration
	checkInterval time.Duration // How often to check for due yields
	batchSize     int32         // How many vaults to process per batch
}

// NewYieldScheduler creates a new yield scheduler instance
//
// Parameters:
//   - taskScheduler: SwiftFiat's existing task scheduler
//   - yieldService: Yield service for calculating yields
//   - store: Database store for querying vaults
//   - logger: Logger instance
//   - checkInterval: How frequently to check for due yields (default: 1 hour)
//
// Example:
//
//	scheduler := NewYieldScheduler(
//	    taskScheduler,
//	    yieldService,
//	    store,
//	    logger,
//	    1 * time.Hour,  // Check every hour
//	)
func NewYieldScheduler(
	taskScheduler *tasks.TaskScheduler,
	yieldService *YieldService,
	store *db.Store,
	logger *logging.Logger,
	checkInterval time.Duration,
) *YieldScheduler {
	if checkInterval == 0 {
		checkInterval = 1 * time.Hour // Default: check every hour
	}

	return &YieldScheduler{
		taskScheduler: taskScheduler,
		yieldService:  yieldService,
		store:         store,
		logger:        logger,
		checkInterval: checkInterval,
		batchSize:     1000, // Process up to 1000 vaults per run
	}
}

// ============================================================================
// START & STOP
// ============================================================================

// Start begins the yield calculation scheduler
// This should be called once during application startup
//
// The scheduler runs continuously in the background, checking for vaults
// that are due for yield calculation at the configured interval.
//
// Typical setup:
//   - Daily compounding: Check every 1-4 hours
//   - Weekly compounding: Check every 12-24 hours
//   - Monthly compounding: Check once daily
//
// Example:
//
//	if err := yieldScheduler.Start(); err != nil {
//	    log.Fatal(err)
//	}
func (ys *YieldScheduler) Start() error {
	ys.logger.Info("Starting vault yield calculation scheduler...")

	// Register the yield calculation task
	_, err := ys.taskScheduler.AddTask(
		"vault-yield-calculations",
		"Calculate and Credit Vault Yields",
		ys.processYieldCalculations,
		ys.checkInterval, // Run at configured interval
	)
	if err != nil {
		return fmt.Errorf("failed to add yield scheduler task: %w", err)
	}

	// Start the task with initial delay of 1 minute
	// (gives time for server to fully initialize)
	if err := ys.taskScheduler.ScheduleTask("vault-yield-calculations", 1*time.Minute); err != nil {
		return fmt.Errorf("failed to schedule yield task: %w", err)
	}

	ys.logger.Info(fmt.Sprintf("Yield scheduler started. Calculating yields every %s", ys.checkInterval))
	return nil
}

// Stop gracefully stops the yield scheduler
// Should be called during application shutdown
func (ys *YieldScheduler) Stop() error {
	ys.logger.Info("Stopping vault yield calculation scheduler...")

	if err := ys.taskScheduler.RemoveTask("vault-yield-calculations"); err != nil {
		// Task might already be stopped, log but don't fail
		ys.logger.Warn(fmt.Sprintf("Failed to remove yield scheduler task: %v", err))
	}

	ys.logger.Info("Yield scheduler stopped")
	return nil
}

// ============================================================================
// PROCESSING LOGIC
// ============================================================================

// processYieldCalculations is the main processing function that runs periodically
// It queries all vaults due for yield calculation and processes them
//
// Steps:
//  1. Query vaults where next_yield_calculation <= NOW()
//  2. For each vault, check if it meets yield requirements
//  3. Calculate yield based on balance and APY
//  4. Credit yield to vault balance
//  5. Update next calculation time
//  6. Send notifications
func (ys *YieldScheduler) processYieldCalculations(ctx context.Context) error {
	ys.logger.Info("Checking for vaults due for yield calculation...")

	// Process yields in batches
	successCount, failureCount, err := ys.yieldService.ProcessAllDueYields(ctx, ys.batchSize)
	if err != nil {
		ys.logger.Error(fmt.Sprintf("Failed to process yields: %v", err))
		return fmt.Errorf("failed to process yields: %w", err)
	}

	if successCount == 0 && failureCount == 0 {
		ys.logger.Info("No vaults due for yield calculation")
		return nil
	}

	ys.logger.Info(fmt.Sprintf("Yield calculations complete: %d succeeded, %d failed",
		successCount, failureCount))

	return nil
}

// ============================================================================
// MANUAL OPERATIONS
// ============================================================================

// ProcessVaultYieldNow manually triggers yield calculation for a specific vault
// Useful for testing or manual intervention by admins
//
// # This bypasses the normal schedule check and immediately calculates yield
//
// Example:
//
//	vaultID := uuid.MustParse("...")
//	if err := yieldScheduler.ProcessVaultYieldNow(ctx, vaultID); err != nil {
//	    log.Printf("Manual yield processing failed: %v", err)
//	}
func (ys *YieldScheduler) ProcessVaultYieldNow(ctx context.Context, vaultID uuid.UUID) error {
	ys.logger.Info(fmt.Sprintf("Manually processing yield for vault %s", vaultID))
	return ys.yieldService.ProcessVaultYield(ctx, vaultID)
}

// ProcessAllYieldsNow manually triggers all due yield calculations immediately
// Useful for catching up after downtime or testing the scheduler
//
// Example:
//
//	if err := yieldScheduler.ProcessAllYieldsNow(ctx); err != nil {
//	    log.Printf("Batch yield processing failed: %v", err)
//	}
func (ys *YieldScheduler) ProcessAllYieldsNow(ctx context.Context) error {
	ys.logger.Info("Manually processing all due yield calculations")
	_, _, err := ys.yieldService.ProcessAllDueYields(ctx, ys.batchSize)
	return err
}

// ============================================================================
// SCHEDULER STATS
// ============================================================================

// YieldSchedulerStats contains statistics about yield scheduler operation
type YieldSchedulerStats struct {
	IsRunning               bool      `json:"is_running"`
	CheckInterval           string    `json:"check_interval"`
	LastCheckTime           time.Time `json:"last_check_time"`
	BatchSize               int32     `json:"batch_size"`
	VaultsDueForCalculation int       `json:"vaults_due_for_calculation"`
	TotalYieldCreditedToday string    `json:"total_yield_credited_today"`
}

// GetStats returns current scheduler statistics
func (ys *YieldScheduler) GetStats(ctx context.Context) (*YieldSchedulerStats, error) {
	task, err := ys.taskScheduler.GetTask("vault-yield-calculations")
	if err != nil {
		return &YieldSchedulerStats{
			IsRunning:     false,
			CheckInterval: ys.checkInterval.String(),
			BatchSize:     ys.batchSize,
		}, nil
	}

	// Count vaults due for calculation
	vaults, err := ys.store.GetVaultsDueForYieldCalculation(ctx, ys.batchSize)
	vaultsDue := 0
	if err == nil {
		vaultsDue = len(vaults)
	}

	// Get today's total yield (from midnight to now)
	// startOfDay := time.Now().Truncate(24 * time.Hour)
	// endOfDay := time.Now()

	// Query total yield credited today across all vaults
	// This requires a custom query or aggregation
	totalYield := "0.0000" // Placeholder - would need custom query

	return &YieldSchedulerStats{
		IsRunning:               true,
		CheckInterval:           ys.checkInterval.String(),
		LastCheckTime:           task.LastRun,
		BatchSize:               ys.batchSize,
		VaultsDueForCalculation: vaultsDue,
		TotalYieldCreditedToday: totalYield,
	}, nil
}

// ============================================================================
// CONFIGURATION UPDATES
// ============================================================================

// SetBatchSize updates the number of vaults processed per batch
func (ys *YieldScheduler) SetBatchSize(size int32) {
	ys.batchSize = size
	ys.logger.Info(fmt.Sprintf("Yield scheduler batch size updated to %d", size))
}

// SetCheckInterval updates how often yields are calculated
// Note: This only affects future scheduled runs
func (ys *YieldScheduler) SetCheckInterval(interval time.Duration) {
	ys.checkInterval = interval
	ys.logger.Info(fmt.Sprintf("Yield scheduler check interval updated to %s", interval))
}

// ============================================================================
// ADVANCED FEATURES
// ============================================================================

// ProcessVaultsByUser processes yields for all vaults belonging to a specific user
// Useful for VIP users or special cases
func (ys *YieldScheduler) ProcessVaultsByUser(ctx context.Context, userID int64) (int, error) {
	ys.logger.Info(fmt.Sprintf("Processing yields for all vaults of user %d", userID))

	// Get all active vaults for user
	vaults, err := ys.store.GetActiveVaultGoalsByUserID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to get user vaults: %w", err)
	}

	successCount := 0
	for _, vault := range vaults {
		if err := ys.yieldService.ProcessVaultYield(ctx, vault.ID); err != nil {
			ys.logger.Error(fmt.Sprintf("Failed to process yield for vault %s: %v", vault.ID, err))
		} else {
			successCount++
		}
	}

	ys.logger.Info(fmt.Sprintf("Processed yields for %d/%d vaults of user %d", 
		successCount, len(vaults), userID))

	return successCount, nil
}

// ProcessVaultsByCurrency processes yields for all vaults of a specific currency
// Useful after updating yield configs for a currency
func (ys *YieldScheduler) ProcessVaultsByCurrency(ctx context.Context, currency string) (int, error) {
	ys.logger.Info(fmt.Sprintf("Processing yields for all %s vaults", currency))

	// Get all active vaults with balances due for calculation
	vaults, err := ys.store.GetVaultsDueForYieldCalculation(ctx, ys.batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to get vaults: %w", err)
	}

	// Filter by currency
	successCount := 0
	for _, vault := range vaults {
		if vault.Currency != currency {
			continue
		}

		if err := ys.yieldService.ProcessVaultYield(ctx, vault.ID); err != nil {
			ys.logger.Error(fmt.Sprintf("Failed to process yield for vault %s: %v", vault.ID, err))
		} else {
			successCount++
		}
	}

	ys.logger.Info(fmt.Sprintf("Processed yields for %d %s vaults", successCount, currency))
	return successCount, nil
}

// ============================================================================
// SCHEDULER HEALTH CHECK
// ============================================================================

// HealthCheck performs a health check on the yield scheduler
type YieldSchedulerHealth struct {
	IsHealthy             bool      `json:"is_healthy"`
	LastSuccessfulRun     time.Time `json:"last_successful_run"`
	TimeSinceLastRun      string    `json:"time_since_last_run"`
	VaultsPendingCalc     int       `json:"vaults_pending_calculation"`
	BacklogWarning        bool      `json:"backlog_warning"`
	YieldConfigsActive    int       `json:"yield_configs_active"`
	Issues                []string  `json:"issues,omitempty"`
}

// HealthCheck returns the health status of the yield scheduler
func (ys *YieldScheduler) HealthCheck(ctx context.Context) (*YieldSchedulerHealth, error) {
	health := &YieldSchedulerHealth{
		IsHealthy: true,
		Issues:    []string{},
	}

	// Check if scheduler is running
	task, err := ys.taskScheduler.GetTask("vault-yield-calculations")
	if err != nil {
		health.IsHealthy = false
		health.Issues = append(health.Issues, "Scheduler task not found")
		return health, nil
	}

	health.LastSuccessfulRun = task.LastRun
	health.TimeSinceLastRun = time.Since(task.LastRun).String()

	// Check if last run was too long ago (> 2x check interval)
	if time.Since(task.LastRun) > ys.checkInterval*2 {
		health.IsHealthy = false
		health.Issues = append(health.Issues, "Last run was too long ago")
	}

	// Check for pending calculations
	vaults, err := ys.store.GetVaultsDueForYieldCalculation(ctx, ys.batchSize)
	if err == nil {
		health.VaultsPendingCalc = len(vaults)
		
		// Warn if backlog is building up
		if len(vaults) > int(ys.batchSize)*2 {
			health.BacklogWarning = true
			health.Issues = append(health.Issues, 
				fmt.Sprintf("Large backlog: %d vaults pending", len(vaults)))
		}
	}

	// Check active yield configs
	configs, err := ys.store.GetAllActiveYieldConfigs(ctx)
	if err == nil {
		health.YieldConfigsActive = len(configs)
		
		if len(configs) == 0 {
			health.IsHealthy = false
			health.Issues = append(health.Issues, "No active yield configurations")
		}
	}

	return health, nil
}
