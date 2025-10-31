package vaultsavings

import (
	"context"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

// ============================================================================
// VAULT SCHEDULER
// ============================================================================

// VaultScheduler manages recurring vault deposits using the TaskScheduler.
// It periodically checks for vaults with recurring rules that are due for execution
// and processes them automatically.
//
// Features:
//   - Processes recurring deposits based on JSONB rules
//   - Handles retry logic for failed deposits
//   - Respects low balance pausing
//   - Sends notifications on success/failure
//   - Graceful shutdown support
type VaultScheduler struct {
	taskScheduler *tasks.TaskScheduler
	vaultService  *VaultService
	store         *db.Store
	logger        *logging.Logger
	checkInterval time.Duration // How often to check for due deposits
}

// NewVaultScheduler creates a new vault scheduler instance.
//
// Parameters:
//   - taskScheduler: SwiftFiat's existing task scheduler
//   - vaultService: Vault service for processing deposits
//   - store: Database store for querying vaults
//   - logger: Logger instance
//   - checkInterval: How frequently to check for due deposits (default: 1 minute)
//
// Example:
//
//	scheduler := NewVaultScheduler(
//	    taskScheduler,
//	    vaultService,
//	    store,
//	    logger,
//	    1 * time.Minute,
//	)
func NewVaultScheduler(
	taskScheduler *tasks.TaskScheduler,
	vaultService *VaultService,
	store *db.Store,
	logger *logging.Logger,
	checkInterval time.Duration,
) *VaultScheduler {
	if checkInterval == 0 {
		checkInterval = 1 * time.Minute // Default: check every minute
	}

	return &VaultScheduler{
		taskScheduler: taskScheduler,
		vaultService:  vaultService,
		store:         store,
		logger:        logger,
		checkInterval: checkInterval,
	}
}

// ============================================================================
// START & STOP
// ============================================================================

// Start begins the vault recurring deposits scheduler.
// This should be called once during application startup.
//
// The scheduler runs continuously in the background, checking for vaults
// with due recurring deposits at the configured interval.
//
// Returns an error if the task cannot be registered.
//
// Example:
//
//	if err := scheduler.Start(); err != nil {
//	    log.Fatal(err)
//	}
func (vs *VaultScheduler) Start() error {
	vs.logger.Info("Starting vault recurring deposits scheduler...")

	// Register the recurring check task
	_, err := vs.taskScheduler.AddTask(
		"vault-recurring-deposits",
		"Process Vault Recurring Deposits",
		vs.processRecurringDeposits,
		vs.checkInterval, // Run at configured interval
	)
	if err != nil {
		return fmt.Errorf("failed to add vault scheduler task: %w", err)
	}

	// Start the task with initial delay of 10 seconds
	// (gives time for server to fully initialize)
	if err := vs.taskScheduler.ScheduleTask("vault-recurring-deposits", 10*time.Second); err != nil {
		return fmt.Errorf("failed to schedule vault task: %w", err)
	}

	vs.logger.Info(fmt.Sprintf("Vault scheduler started. Checking for recurring deposits every %s", vs.checkInterval))
	return nil
}

// Stop gracefully stops the vault scheduler.
// Should be called during application shutdown.
//
// Example:
//
//	scheduler.Stop()
func (vs *VaultScheduler) Stop() error {
	vs.logger.Info("Stopping vault recurring deposits scheduler...")

	if err := vs.taskScheduler.RemoveTask("vault-recurring-deposits"); err != nil {
		// Task might already be stopped, log but don't fail
		vs.logger.Warn(fmt.Sprintf("Failed to remove vault scheduler task: %v", err))
	}

	vs.logger.Info("Vault scheduler stopped")
	return nil
}

// ============================================================================
// PROCESSING LOGIC
// ============================================================================

// processRecurringDeposits is the main processing function that runs periodically.
// It queries all vaults with active recurring rules, checks if they're due for
// execution, and processes the deposits.
//
// Steps:
//  1. Query all vaults with recurring rules enabled
//  2. Filter to only those due for execution (NextExecutionAt <= now)
//  3. For each vault, attempt the recurring deposit
//  4. Handle success/failure and update the rule
//  5. Send notifications if configured
func (vs *VaultScheduler) processRecurringDeposits(ctx context.Context) error {
	vs.logger.Info("Checking for recurring deposits to process...")

	// Get all vaults with active recurring rules that are due
	vaults, err := vs.store.GetVaultsWithDueRecurringDeposits(ctx, time.Now())
	if err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to fetch vaults with due deposits: %v", err))
		return fmt.Errorf("failed to fetch vaults: %w", err)
	}

	if len(vaults) == 0 {
		vs.logger.Info("No recurring deposits due at this time")
		return nil
	}

	vs.logger.Info(fmt.Sprintf("Found %d vaults with due recurring deposits", len(vaults)))

	// Process each vault
	successCount := 0
	failureCount := 0

	for _, vault := range vaults {
		if err := vs.processVaultDeposit(ctx, &vault); err != nil {
			vs.logger.Error(fmt.Sprintf("Failed to process recurring deposit for vault %s: %v", vault.ID, err))
			failureCount++
		} else {
			successCount++
		}
	}

	vs.logger.Info(fmt.Sprintf("Recurring deposits processed: %d succeeded, %d failed", successCount, failureCount))
	return nil
}

// processVaultDeposit handles the deposit for a single vault.
// It checks the recurring rule, validates the wallet balance,
// performs the deposit, and updates the rule accordingly.
func (vs *VaultScheduler) processVaultDeposit(ctx context.Context, vault *db.VaultSaving) error {
	// Parse the recurring rule from JSONB
	var rule RecurringRule
	if err := rule.Scan(vault.RecurringRule); err != nil {
		return fmt.Errorf("failed to parse recurring rule: %w", err)
	}

	vs.logger.Info(fmt.Sprintf("Processing recurring deposit for vault '%s' (ID: %s)", vault.VaultName, vault.ID))

	// Double-check the rule is active (safety check)
	if !rule.IsActive() {
		vs.logger.Warn(fmt.Sprintf("Vault %s has inactive rule, skipping", vault.ID))
		return nil
	}

	// Parse the deposit amount
	amount, err := decimal.NewFromString(rule.Amount)
	if err != nil {
		return fmt.Errorf("invalid recurring amount: %w", err)
	}

	// Get the source wallet for this currency
	wallet, err := vs.store.GetWalletByCurrency(ctx, db.GetWalletByCurrencyParams{
		CustomerID: vault.UserID,
		Currency:   vault.Currency,
	})
	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}

	// Parse wallet balance
	walletBalance, err := decimal.NewFromString(wallet.Balance.String)
	if err != nil {
		return fmt.Errorf("invalid wallet balance: %w", err)
	}

	// Check if wallet has sufficient balance
	if walletBalance.LessThan(amount) {
		return vs.handleInsufficientBalance(ctx, vault, &rule, walletBalance, amount)
	}

	// Perform the deposit
	depositReq := DepositRequest{
		UserID:       vault.UserID,
		VaultID:      vault.ID,
		FromWalletID: wallet.ID,
		Amount:       rule.Amount,
		Currency:     vault.Currency,
		Description:  fmt.Sprintf("Automated %s recurring deposit", rule.Interval),
		Reference:    "", // update
	}

	tx, err := vs.vaultService.Deposit(ctx, depositReq)
	if err != nil {
		return vs.handleDepositFailure(ctx, vault, &rule, err)
	}

	// Deposit succeeded - update the rule
	return vs.handleDepositSuccess(ctx, vault, &rule, tx)
}

// ============================================================================
// SUCCESS & FAILURE HANDLERS
// ============================================================================

// handleDepositSuccess updates the recurring rule after a successful deposit.
// Actions:
//   - Mark rule as executed (increments counter, updates timestamps)
//   - Calculate next execution time
//   - Reset retry counter
//   - Send success notification if enabled
//   - Save updated rule to database
func (vs *VaultScheduler) handleDepositSuccess(
	ctx context.Context,
	vault *db.VaultSaving,
	rule *RecurringRule,
	tx *db.VaultTransaction,
) error {
	vs.logger.Info(fmt.Sprintf("Recurring deposit successful for vault %s: %s %s",
		vault.ID, rule.Amount, vault.Currency))

	// Update the rule
	rule.MarkExecuted()

	// Save the updated rule
	ruleValue, err := rule.Value()
	if err != nil {
		return fmt.Errorf("failed to serialize rule: %w", err)
	}

	// Convert to pqtype.NullRawMessage
	bytes, ok := ruleValue.([]byte)
	if !ok {
		return fmt.Errorf("unexpected rule value type: %T", ruleValue)
	}
	recurringRule := pqtype.NullRawMessage{RawMessage: bytes, Valid: true}

	if err := vs.store.UpdateVaultRecurringRule(ctx, db.UpdateVaultRecurringRuleParams{
		ID:            vault.ID,
		RecurringRule: recurringRule,
	}); err != nil {
		return fmt.Errorf("failed to update recurring rule: %w", err)
	}

	// Send success notification if enabled
	if rule.NotifyOnSuccess {
		go vs.sendSuccessNotification(vault, rule, tx)
	}

	vs.logger.Info(fmt.Sprintf("Next recurring deposit for vault %s scheduled at %s",
		vault.ID, rule.NextExecutionAt.Format(time.RFC3339)))

	return nil
}

// handleDepositFailure handles a failed deposit attempt.
// Actions:
//   - Check if retries are enabled and available
//   - If retries available: increment retry counter, schedule retry
//   - If retries exhausted: mark as failed, send failure notification
//   - Update rule in database
func (vs *VaultScheduler) handleDepositFailure(
	ctx context.Context,
	vault *db.VaultSaving,
	rule *RecurringRule,
	depositErr error,
) error {
	vs.logger.Error(fmt.Sprintf("Recurring deposit failed for vault %s: %v",
		vault.ID, depositErr))

	// Check if we should retry
	if rule.RetryOnFailure && rule.RetryCount < rule.MaxRetries {
		rule.RetryCount++
		vs.logger.Info(fmt.Sprintf("Will retry deposit for vault %s (attempt %d/%d)",
			vault.ID, rule.RetryCount, rule.MaxRetries))

		// Save updated retry count
		ruleValue, err := rule.Value()
		if err != nil {
			return fmt.Errorf("failed to serialize rule: %w", err)
		}

		// Convert to pqtype.NullRawMessage
		bytes, ok := ruleValue.([]byte)
		if !ok {
			return fmt.Errorf("unexpected rule value type: %T", ruleValue)
		}
		recurringRule := pqtype.NullRawMessage{RawMessage: bytes, Valid: true}

		if err := vs.store.UpdateVaultRecurringRule(ctx, db.UpdateVaultRecurringRuleParams{
			ID:            vault.ID,
			RecurringRule: recurringRule,
		}); err != nil {
			return fmt.Errorf("failed to update retry count: %w", err)
		}

		// Schedule a retry in 1 hour
		retryTaskID := fmt.Sprintf("vault-retry-%s", vault.ID.String())
		_, err = vs.taskScheduler.AddTask(
			retryTaskID,
			fmt.Sprintf("Retry deposit for vault %s", vault.VaultName),
			func(retryCtx context.Context) error {
				return vs.processVaultDeposit(retryCtx, vault)
			},
			0, // One-time task
		)
		if err == nil {
			vs.taskScheduler.RunAfterAndRemove(retryTaskID, 1*time.Hour)
		}

		return nil
	}

	// Retries exhausted or disabled - mark as failed
	vs.logger.Error(fmt.Sprintf("Recurring deposit failed permanently for vault %s after %d retries",
		vault.ID, rule.RetryCount))

	// Reset for next cycle
	rule.RetryCount = 0
	rule.NextExecutionAt = rule.CalculateNextExecution()

	// Save updated rule
	ruleValue, err := rule.Value()
	if err != nil {
		return fmt.Errorf("failed to serialize rule: %w", err)
	}

	// Convert to pqtype.NullRawMessage
	bytes, ok := ruleValue.([]byte)
	if !ok {
		return fmt.Errorf("unexpected rule value type: %T", ruleValue)
	}
	recurringRule := pqtype.NullRawMessage{RawMessage: bytes, Valid: true}

	if err := vs.store.UpdateVaultRecurringRule(ctx, db.UpdateVaultRecurringRuleParams{
		ID:            vault.ID,
		RecurringRule: recurringRule,
	}); err != nil {
		return fmt.Errorf("failed to update rule: %w", err)
	}

	// Send failure notification if enabled
	if rule.NotifyOnFailure {
		go vs.sendFailureNotification(vault, rule, depositErr)
	}

	return fmt.Errorf("deposit failed after retries: %w", depositErr)
}

// handleInsufficientBalance handles the case when wallet has insufficient funds.
// Behavior depends on PauseOnLowBalance setting:
//   - If true: Skip this cycle, will try again next cycle (better UX)
//   - If false: Treat as failure, trigger retry logic
func (vs *VaultScheduler) handleInsufficientBalance(
	ctx context.Context,
	vault *db.VaultSaving,
	rule *RecurringRule,
	walletBalance, requiredAmount decimal.Decimal,
) error {
	vs.logger.Warn(fmt.Sprintf("Insufficient balance for vault %s: have %s, need %s",
		vault.ID, walletBalance.String(), requiredAmount.String()))

	if rule.PauseOnLowBalance {
		// Just skip this cycle - will try again at next NextExecutionAt
		vs.logger.Info(fmt.Sprintf("Pausing recurring deposit for vault %s due to low balance (will retry next cycle)",
			vault.ID))

		// Calculate next execution time without marking as executed
		rule.NextExecutionAt = rule.CalculateNextExecution()

		// Save updated rule
		ruleValue, err := rule.Value()
		if err != nil {
			return fmt.Errorf("failed to serialize rule: %w", err)
		}

		// Convert to pqtype.NullRawMessage
		bytes, ok := ruleValue.([]byte)
		if !ok {
			return fmt.Errorf("unexpected rule value type: %T", ruleValue)
		}
		recurringRule := pqtype.NullRawMessage{RawMessage: bytes, Valid: true}

		if err := vs.store.UpdateVaultRecurringRule(ctx, db.UpdateVaultRecurringRuleParams{
			ID:            vault.ID,
			RecurringRule: recurringRule,
		}); err != nil {
			return fmt.Errorf("failed to update rule: %w", err)
		}

		return nil // Not really an error, just skipped
	}

	// Treat as failure and go through retry logic
	return vs.handleDepositFailure(ctx, vault, rule,
		fmt.Errorf("insufficient wallet balance: have %s, need %s",
			walletBalance.String(), requiredAmount.String()))
}

// ============================================================================
// NOTIFICATIONS
// ============================================================================

// sendSuccessNotification sends email and push notifications for successful deposits.
func (vs *VaultScheduler) sendSuccessNotification(
	vault *db.VaultSaving,
	rule *RecurringRule,
	tx *db.VaultTransaction,
) {
	// Get user details
	user, err := vs.store.GetUserByID(context.Background(), vault.UserID)
	if err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to get user for notification: %v", err))
		return
	}

	// Send email notification
	if err := vs.vaultService.emailService.SendRecurringDepositSuccessEmail(
		context.Background(),
		&user,
		vault.VaultName,
		rule.Amount,
		vault.Currency,
	); err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to send success email: %v", err))
	}

	// Send push notification
	if err := vs.vaultService.pushService.SendRecurringDepositSuccessPush(
		context.Background(),
		vault.UserID,
		vault.VaultName,
		rule.Amount,
		vault.Currency,
	); err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to send success push: %v", err))
	}
}

// sendFailureNotification sends email and push notifications for failed deposits.
func (vs *VaultScheduler) sendFailureNotification(
	vault *db.VaultSaving,
	rule *RecurringRule,
	depositErr error,
) {
	// Get user details
	user, err := vs.store.GetUserByID(context.Background(), vault.UserID)
	if err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to get user for notification: %v", err))
		return
	}

	// Send email notification
	if err := vs.vaultService.emailService.SendRecurringDepositFailedEmail(
		context.Background(),
		&user,
		vault.VaultName,
		rule.Amount,
		vault.Currency,
		depositErr.Error(),
	); err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to send failure email: %v", err))
	}

	// Send push notification
	if err := vs.vaultService.pushService.SendRecurringDepositFailedPush(
		context.Background(),
		vault.UserID,
		vault.VaultName,
		depositErr.Error(),
	); err != nil {
		vs.logger.Error(fmt.Sprintf("Failed to send failure push: %v", err))
	}
}

// ============================================================================
// MANUAL OPERATIONS (for testing/debugging)
// ============================================================================

// ProcessVaultNow manually triggers a recurring deposit for a specific vault.
// Useful for testing or manual intervention by admins.
//
// This bypasses the normal schedule check and immediately attempts the deposit.
//
// Example:
//
//	vaultID := uuid.MustParse("...")
//	if err := scheduler.ProcessVaultNow(ctx, vaultID); err != nil {
//	    log.Printf("Manual processing failed: %v", err)
//	}
func (vs *VaultScheduler) ProcessVaultNow(ctx context.Context, vaultID uuid.UUID) error {
	vs.logger.Info(fmt.Sprintf("Manually processing recurring deposit for vault %s", vaultID))

	// Get the vault
	vault, err := vs.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		return fmt.Errorf("failed to get vault: %w", err)
	}

	// Process the deposit
	return vs.processVaultDeposit(ctx, &vault)
}

// ProcessAllDueNow manually triggers all due recurring deposits immediately.
// Useful for catching up after downtime or testing the scheduler.
//
// Example:
//
//	if err := scheduler.ProcessAllDueNow(ctx); err != nil {
//	    log.Printf("Batch processing failed: %v", err)
//	}
func (vs *VaultScheduler) ProcessAllDueNow(ctx context.Context) error {
	vs.logger.Info("Manually processing all due recurring deposits")
	return vs.processRecurringDeposits(ctx)
}

// GetSchedulerStats returns statistics about the scheduler's operation.
// Useful for monitoring and debugging.
type SchedulerStats struct {
	IsRunning            bool      `json:"is_running"`
	CheckInterval        string    `json:"check_interval"`
	LastCheckTime        time.Time `json:"last_check_time"`
	TotalVaultsChecked   int       `json:"total_vaults_checked"`
	ActiveRecurringRules int       `json:"active_recurring_rules"`
}

func (vs *VaultScheduler) GetStats(ctx context.Context) (*SchedulerStats, error) {
	task, err := vs.taskScheduler.GetTask("vault-recurring-deposits")
	if err != nil {
		return &SchedulerStats{
			IsRunning:     false,
			CheckInterval: vs.checkInterval.String(),
		}, nil
	}

	// Count active recurring rules
	vaults, err := vs.store.GetVaultsWithActiveRecurringRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count active rules: %w", err)
	}

	return &SchedulerStats{
		IsRunning:            true,
		CheckInterval:        vs.checkInterval.String(),
		LastCheckTime:        task.LastRun,
		ActiveRecurringRules: len(vaults),
	}, nil
}
