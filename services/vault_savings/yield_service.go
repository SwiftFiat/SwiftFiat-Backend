package vaultsavings

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ============================================================================
// YIELD SERVICE
// ============================================================================

// YieldService handles all yield calculation and crediting operations
type YieldService struct {
	store        *db.Store
	logger       *logging.Logger
	emailService *service.Plunk
	pushService  *service.PushNotificationService
}

// NewYieldService creates a new yield service instance
func NewYieldService(
	store *db.Store,
	logger *logging.Logger,
	emailService *service.Plunk,
	pushService *service.PushNotificationService,
) *YieldService {
	return &YieldService{
		store:        store,
		logger:       logger,
		emailService: emailService,
		pushService:  pushService,
	}
}

type VaultYieldResponse struct {
	ID                     uuid.UUID `json:"id"`
	UserID                 int64     `json:"user_id"`
	VaultID                uuid.UUID `json:"vault_id"`
	YieldAmount            string    `json:"yield_amount"`
	YieldRate              string    `json:"yield_rate"`
	CalculationPeriodStart time.Time `json:"calculation_period_start"`
	CalculationPeriodEnd   time.Time `json:"calculation_period_end"`
	VaultBalanceSnapshot   string    `json:"vault_balance_snapshot"`
	Status                 string    `json:"status"`
	CreditedAt             time.Time `json:"credited_at"`
	CreatedAt              time.Time `json:"created_at"`
}

func MapVaultYieldToResponse(v *db.VaultYield) *VaultYieldResponse {
	return &VaultYieldResponse{
		ID:                     v.ID,
		UserID:                 v.UserID,
		VaultID:                v.VaultID,
		YieldAmount:            v.YieldAmount,
		YieldRate:              v.YieldRate,
		CalculationPeriodStart: v.CalculationPeriodStart,
		CalculationPeriodEnd:   v.CalculationPeriodEnd,
		VaultBalanceSnapshot:   v.VaultBalanceSnapshot,
		Status:                 v.Status.String,
		CreditedAt:             v.CreditedAt.Time,
		CreatedAt:              v.CreatedAt,
	}
}

// ============================================================================
// YIELD CALCULATION MODELS
// ============================================================================

// YieldCalculationRequest contains parameters for calculating yield
type YieldCalculationRequest struct {
	VaultID       uuid.UUID
	Balance       string // Current balance
	APY           string // Annual percentage yield
	PeriodStart   time.Time
	PeriodEnd     time.Time
	CompoundDaily bool // Whether to use daily compounding
}

// YieldCalculationResult contains the calculated yield details
type YieldCalculationResult struct {
	YieldAmount     string // Total yield earned
	DailyRate       string // Daily interest rate used
	DaysInPeriod    int    // Number of days in calculation period
	StartBalance    string // Balance at start of period
	EndBalance      string // Balance including yield
	EffectiveAPY    string // Actual APY achieved (may differ due to compounding)
	CalculatedAt    time.Time
	CompoundingUsed bool
	Reference       string
}

// ============================================================================
// CALCULATE YIELD
// ============================================================================

// CalculateYield calculates yield for a vault based on balance and APY
// Uses the formula: Yield = Principal × (1 + APY/365)^days - Principal
// For daily compounding, uses: Yield = Principal × ((1 + APY/365)^days - 1)
func (ys *YieldService) CalculateYield(ctx context.Context, req YieldCalculationRequest) (*YieldCalculationResult, error) {
	// Parse decimal values
	balance, err := decimal.NewFromString(req.Balance)
	if err != nil {
		return nil, fmt.Errorf("invalid balance: %w", err)
	}

	apy, err := decimal.NewFromString(req.APY)
	if err != nil {
		return nil, fmt.Errorf("invalid APY: %w", err)
	}

	// Validate inputs
	if balance.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("balance must be positive")
	}

	if apy.LessThan(decimal.Zero) {
		return nil, fmt.Errorf("APY cannot be negative")
	}

	// Calculate days in period
	days := int(req.PeriodEnd.Sub(req.PeriodStart).Hours() / 24)
	if days <= 0 {
		return nil, fmt.Errorf("invalid period: end must be after start")
	}

	// Convert APY from percentage to decimal (e.g., 5.0% -> 0.05)
	apyDecimal := apy.Div(decimal.NewFromInt(100))

	var yieldAmount decimal.Decimal
	var effectiveAPY decimal.Decimal

	if req.CompoundDaily {
		// Daily compounding formula: A = P(1 + r/n)^(nt)
		// Where: P = principal, r = annual rate, n = 365, t = days/365
		dailyRate := apyDecimal.Div(decimal.NewFromInt(365))

		// Calculate (1 + daily_rate)^days
		onePlusDailyRate := decimal.NewFromInt(1).Add(dailyRate)
		compoundFactor := ys.power(onePlusDailyRate, days)

		// Final amount = balance * compound_factor
		finalAmount := balance.Mul(compoundFactor)
		yieldAmount = finalAmount.Sub(balance)

		// Calculate effective APY achieved
		yearlyFactor := ys.power(onePlusDailyRate, 365)
		effectiveAPY = yearlyFactor.Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(100))
	} else {
		// Simple interest: Yield = Principal × APY × (days / 365)
		daysFraction := decimal.NewFromInt(int64(days)).Div(decimal.NewFromInt(365))
		yieldAmount = balance.Mul(apyDecimal).Mul(daysFraction)
		effectiveAPY = apy // Same as input for simple interest
	}

	// Calculate final balance
	finalBalance := balance.Add(yieldAmount)

	// Calculate daily rate for informational purposes
	dailyRate := apyDecimal.Div(decimal.NewFromInt(365))

	return &YieldCalculationResult{
		YieldAmount:     yieldAmount.StringFixed(8),
		DailyRate:       dailyRate.StringFixed(10),
		DaysInPeriod:    days,
		StartBalance:    balance.StringFixed(8),
		EndBalance:      finalBalance.StringFixed(8),
		EffectiveAPY:    effectiveAPY.StringFixed(6),
		CalculatedAt:    time.Now(),
		CompoundingUsed: req.CompoundDaily,
	}, nil
}

// power calculates decimal^exponent using approximation
// For financial calculations, we use a series approximation
func (ys *YieldService) power(base decimal.Decimal, exponent int) decimal.Decimal {
	// Convert to float64 for calculation (acceptable for financial precision up to 8 decimals)
	baseFloat, _ := base.Float64()
	result := math.Pow(baseFloat, float64(exponent))
	return decimal.NewFromFloat(result)
}

// ============================================================================
// PROCESS VAULT YIELD
// ============================================================================

// ProcessVaultYield calculates and credits yield for a single vault
func (ys *YieldService) ProcessVaultYield(ctx context.Context, vaultID uuid.UUID) error {
	ys.logger.Info(fmt.Sprintf("Processing yield for vault %s", vaultID))

	// Get vault details
	vault, err := ys.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		return fmt.Errorf("failed to get vault: %w", err)
	}

	// Skip if vault has no balance
	balance, err := decimal.NewFromString(vault.CurrentBalance.String)
	if err != nil || balance.LessThanOrEqual(decimal.Zero) {
		ys.logger.Info(fmt.Sprintf("Vault %s has no balance, skipping yield", vaultID))
		return nil
	}

	// Get active yield config for this currency
	yieldConfig, err := ys.store.GetActiveYieldConfigByCurrency(ctx, vault.Currency)
	if err != nil {
		ys.logger.Warn(fmt.Sprintf("No active yield config for currency %s", vault.Currency))
		return nil // Not an error, just no yield available
	}

	// Check if balance meets minimum requirement
	minBalance, _ := decimal.NewFromString(yieldConfig.MinBalanceForYield)
	if balance.LessThan(minBalance) {
		ys.logger.Info(fmt.Sprintf("Vault %s balance below minimum for yield", vaultID))
		return nil
	}

	// Determine calculation period
	var periodStart time.Time
	if vault.LastYieldCalculation.Valid {
		periodStart = vault.LastYieldCalculation.Time
	} else {
		// First time - calculate from vault creation
		periodStart = vault.CreatedAt
	}

	periodEnd := time.Now()

	// Calculate yield
	calcReq := YieldCalculationRequest{
		VaultID:       vaultID,
		Balance:       vault.CurrentBalance.String,
		APY:           yieldConfig.ApyRate,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		CompoundDaily: yieldConfig.CompoundFrequency.Valid && yieldConfig.CompoundFrequency.String == "daily",
	}

	result, err := ys.CalculateYield(ctx, calcReq)
	if err != nil {
		return fmt.Errorf("failed to calculate yield: %w", err)
	}

	// Check if yield is significant enough to credit (> 0.0001)
	yieldAmount, _ := decimal.NewFromString(result.YieldAmount)
	minYield := decimal.NewFromFloat(0.0001)
	if yieldAmount.LessThan(minYield) {
		ys.logger.Info(fmt.Sprintf("Yield amount too small for vault %s: %s", vaultID, result.YieldAmount))
		return nil
	}

	// Credit yield in a transaction
	return ys.creditYieldToVault(ctx, &vault, result, &yieldConfig)
}

// ============================================================================
// CREDIT YIELD TO VAULT
// ============================================================================

// creditYieldToVault credits calculated yield to vault balance
func (ys *YieldService) creditYieldToVault(
	ctx context.Context,
	vault *db.VaultSaving,
	result *YieldCalculationResult,
	config *db.VaultYieldConfig,
) error {
	ys.logger.Info(fmt.Sprintf("Crediting yield %s %s to vault %s",
		result.YieldAmount, vault.Currency, vault.ID))

	// Parse amounts
	yieldAmount, _ := decimal.NewFromString(result.YieldAmount)
	currentBalance, _ := decimal.NewFromString(vault.CurrentBalance.String)
	newBalance := currentBalance.Add(yieldAmount)

	// Start database transaction
	tx, err := ys.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := ys.store.WithTx(tx)

	// Create yield record
	periodStart := vault.LastYieldCalculation.Time
	if !vault.LastYieldCalculation.Valid {
		periodStart = vault.CreatedAt
	}

	yieldRecord, err := qtx.CreateVaultYield(ctx, db.CreateVaultYieldParams{
		UserID:                 vault.UserID,
		VaultID:                vault.ID,
		YieldAmount:            result.YieldAmount,
		YieldRate:              config.ApyRate,
		CalculationPeriodStart: periodStart,
		CalculationPeriodEnd:   time.Now(),
		VaultBalanceSnapshot:   vault.CurrentBalance.String,
		Status:                 nullString("calculated"),
	})
	if err != nil {
		return fmt.Errorf("failed to create yield record: %w", err)
	}

	// Create vault transaction for yield credit
	transactionRef := utils.NewTxRef("yield")
	balanceBefore := vault.CurrentBalance
	balanceAfter := newBalance.StringFixed(4)

	_, err = qtx.CreateVaultTransaction(ctx, db.CreateVaultTransactionParams{
		UserID:          vault.UserID,
		VaultID:         vault.ID,
		TransactionType: string(TransactionTypeYieldCredit),
		Amount:          result.YieldAmount,
		Currency:        vault.Currency,
		BalanceBefore:   balanceBefore.String,
		BalanceAfter:    balanceAfter,
		Reference:       nullString(transactionRef),
		Description:     nullString((fmt.Sprintf("Yield earned: %s%% APY for %d days", config.ApyRate, result.DaysInPeriod))),
		Status:          nullString(string(TransactionStatusSuccessful)),
		Requires2fa:     nullBool(false),
	})
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	// Update vault balance and yield tracking
	totalYieldEarned, _ := decimal.NewFromString(vault.TotalYieldEarned.String)
	newTotalYield := totalYieldEarned.Add(yieldAmount)

	nextCalculation := time.Now()
	switch config.CompoundFrequency.String {
	case "daily":
		nextCalculation = nextCalculation.Add(24 * time.Hour)
	case "weekly":
		nextCalculation = nextCalculation.Add(7 * 24 * time.Hour)
	case "monthly":
		nextCalculation = nextCalculation.AddDate(0, 1, 0)
	default:
		nextCalculation = nextCalculation.Add(24 * time.Hour) // Default to daily
	}

	err = qtx.UpdateYieldTracking(ctx, db.UpdateYieldTrackingParams{
		ID:                   vault.ID,
		TotalYieldEarned:     nullString(newTotalYield.StringFixed(4)),
		LastYieldCalculation: nullTime(time.Now()),
		NextYieldCalculation: nullTime(nextCalculation),
	})
	if err != nil {
		return fmt.Errorf("failed to update yield tracking: %w", err)
	}

	// Update vault balance
	err = qtx.IncrementVaultBalance(ctx, db.IncrementVaultBalanceParams{
		ID:             vault.ID,
		CurrentBalance: nullString(result.YieldAmount),
	})
	if err != nil {
		return fmt.Errorf("failed to update vault balance: %w", err)
	}

	// Mark yield as credited
	err = qtx.UpdateYieldStatus(ctx, db.UpdateYieldStatusParams{
		ID:     yieldRecord.ID,
		Status: nullString("credited"),
	})
	if err != nil {
		return fmt.Errorf("failed to update yield status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	ys.logger.Info(fmt.Sprintf("Successfully credited yield %s %s to vault %s",
		result.YieldAmount, vault.Currency, vault.ID))

	// Send notifications asynchronously
	go ys.sendYieldNotification(vault, result)

	// Check if goal reached after yield credit
	go ys.checkGoalCompletion(vault.ID, newBalance.String(), vault.GoalAmount.String)

	return nil
}

// ============================================================================
// BATCH PROCESSING
// ============================================================================

// ProcessAllDueYields processes yields for all vaults that are due for calculation
func (ys *YieldService) ProcessAllDueYields(ctx context.Context, limit int32) (int, int, error) {
	ys.logger.Info("Processing all due yields...")

	// Get vaults due for yield calculation
	vaults, err := ys.store.GetVaultsDueForYieldCalculation(ctx, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch vaults: %w", err)
	}

	if len(vaults) == 0 {
		ys.logger.Info("No vaults due for yield calculation")
		return 0, 0, nil
	}

	ys.logger.Info(fmt.Sprintf("Found %d vaults due for yield calculation", len(vaults)))

	successCount := 0
	failureCount := 0

	for _, vault := range vaults {
		if err := ys.ProcessVaultYield(ctx, vault.ID); err != nil {
			ys.logger.Error(fmt.Sprintf("Failed to process yield for vault %s: %v", vault.ID, err))
			failureCount++
		} else {
			successCount++
		}
	}

	ys.logger.Info(fmt.Sprintf("Yield processing complete: %d succeeded, %d failed", successCount, failureCount))
	return successCount, failureCount, nil
}

// ============================================================================
// YIELD PROJECTION
// ============================================================================

// ProjectYield calculates estimated future yield without crediting
// Useful for showing users potential earnings
type YieldProjection struct {
	ProjectionPeriodDays int    `json:"projection_period_days"`
	CurrentBalance       string `json:"current_balance"`
	EstimatedYield       string `json:"estimated_yield"`
	ProjectedBalance     string `json:"projected_balance"`
	APY                  string `json:"apy"`
	DailyYield           string `json:"daily_yield"`
	WeeklyYield          string `json:"weekly_yield"`
	MonthlyYield         string `json:"monthly_yield"`
	YearlyYield          string `json:"yearly_yield"`
}

// GetYieldProjection returns yield projections for a vault
func (ys *YieldService) GetYieldProjection(ctx context.Context, vaultID uuid.UUID, days int) (*YieldProjection, error) {
	// Get vault
	vault, err := ys.store.GetVaultGoalByID(ctx, vaultID)
	if err != nil {
		return nil, fmt.Errorf("failed to get vault: %w", err)
	}

	// Get yield config
	config, err := ys.store.GetActiveYieldConfigByCurrency(ctx, vault.Currency)
	if err != nil {
		return nil, fmt.Errorf("no active yield config: %w", err)
	}

	balance, _ := decimal.NewFromString(vault.CurrentBalance.String)
	apy, _ := decimal.NewFromString(config.ApyRate)

	// Calculate for requested period
	periodResult, _ := ys.CalculateYield(ctx, YieldCalculationRequest{
		VaultID:       vaultID,
		Balance:       vault.CurrentBalance.String,
		APY:           config.ApyRate,
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().AddDate(0, 0, days),
		CompoundDaily: config.CompoundFrequency.String == "daily",
	})

	// Calculate daily yield
	dailyResult, _ := ys.CalculateYield(ctx, YieldCalculationRequest{
		VaultID:       vaultID,
		Balance:       vault.CurrentBalance.String,
		APY:           config.ApyRate,
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().AddDate(0, 0, 1),
		CompoundDaily: false,
	})

	// Calculate weekly yield
	weeklyResult, _ := ys.CalculateYield(ctx, YieldCalculationRequest{
		VaultID:       vaultID,
		Balance:       vault.CurrentBalance.String,
		APY:           config.ApyRate,
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().AddDate(0, 0, 7),
		CompoundDaily: config.CompoundFrequency.String == "daily",
	})

	// Calculate monthly yield
	monthlyResult, _ := ys.CalculateYield(ctx, YieldCalculationRequest{
		VaultID:       vaultID,
		Balance:       vault.CurrentBalance.String,
		APY:           config.ApyRate,
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().AddDate(0, 1, 0),
		CompoundDaily: config.CompoundFrequency.String == "daily",
	})

	// Calculate yearly yield
	yearlyResult, _ := ys.CalculateYield(ctx, YieldCalculationRequest{
		VaultID:       vaultID,
		Balance:       vault.CurrentBalance.String,
		APY:           config.ApyRate,
		PeriodStart:   time.Now(),
		PeriodEnd:     time.Now().AddDate(1, 0, 0),
		CompoundDaily: config.CompoundFrequency.String == "daily",
	})

	estimatedYield, _ := decimal.NewFromString(periodResult.YieldAmount)
	projectedBalance := balance.Add(estimatedYield)

	return &YieldProjection{
		ProjectionPeriodDays: days,
		CurrentBalance:       balance.StringFixed(4),
		EstimatedYield:       estimatedYield.StringFixed(4),
		ProjectedBalance:     projectedBalance.StringFixed(4),
		APY:                  apy.StringFixed(2),
		DailyYield:           dailyResult.YieldAmount,
		WeeklyYield:          weeklyResult.YieldAmount,
		MonthlyYield:         monthlyResult.YieldAmount,
		YearlyYield:          yearlyResult.YieldAmount,
	}, nil
}

// ============================================================================
// YIELD CONFIG MANAGEMENT
// ============================================================================

type VaultYieldConfigResponse struct {
	ID                 uuid.UUID `json:"id"`
	Currency           string    `json:"currency"`
	ApyRate            string    `json:"apy_rate"`
	MinBalanceForYield string    `json:"min_balance_for_yield"`
	CompoundFrequency  string    `json:"compound_frequency"`
	IsActive           bool      `json:"is_active"`
	EffectiveFrom      time.Time `json:"effective_from"`
	EffectiveUntil     time.Time `json:"effective_until"`
	Notes              string    `json:"notes"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func MapVaultYieldConfigToResponse(v *db.VaultYieldConfig) *VaultYieldConfigResponse {
	return &VaultYieldConfigResponse{
		ID:                 v.ID,
		Currency:           v.Currency,
		ApyRate:            v.ApyRate,
		MinBalanceForYield: v.MinBalanceForYield,
		CompoundFrequency:  v.CompoundFrequency.String,
		IsActive:           v.IsActive,
		EffectiveFrom:      v.EffectiveFrom,
		EffectiveUntil:     v.EffectiveUntil.Time,
		Notes:              v.Notes.String,
		CreatedAt:          v.CreatedAt,
		UpdatedAt:          v.UpdatedAt,
	}
}

type CreateYieldConfigParams struct {
	Currency           string     `json:"currency" example:"USD"`
	ApyRate            string     `json:"apy_rate" example:"10"`
	MinBalanceForYield string     `json:"min_balance_for_yield" example:"100"`
	CompoundFrequency  *string    `json:"compound_frequency" example:"daily"`
	IsActive           bool       `json:"is_active" example:"true"`
	EffectiveFrom      time.Time  `json:"effective_from" example:"2025-12-19T13:24:54Z"`
	EffectiveUntil     *time.Time `json:"effective_until" example:"2025-12-19T13:24:54Z"`
	Notes              *string    `json:"notes"`
}

// CreateYieldConfig creates a new yield configuration
func (ys *YieldService) CreateYieldConfig(ctx context.Context, params CreateYieldConfigParams) (*db.VaultYieldConfig, error) {
	ys.logger.Info(fmt.Sprintf("Creating yield config for %s: %s%% APY", params.Currency, params.ApyRate))

	args := db.CreateYieldConfigParams{
		Currency:           params.Currency,
		ApyRate:            params.ApyRate,
		MinBalanceForYield: params.MinBalanceForYield,
		// CompoundFrequency:  sql.NullString{String: *params.CompoundFrequency, Valid: true},
		IsActive:           params.IsActive,
		EffectiveFrom:      params.EffectiveFrom,
		// EffectiveUntil:     sql.NullTime{Time: *params.EffectiveUntil, Valid: true},
		// Notes:              sql.NullString{String: *params.Notes, Valid: true},
	}
	config, err := ys.store.CreateYieldConfig(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to create yield config: %w", err)
	}

	return &config, nil
}

type UpdateYieldConfigParams struct {
	ID                 uuid.UUID  `json:"id"`
	Currency           string     `json:"currency"`
	ApyRate            string     `json:"apy_rate"`
	MinBalanceForYield string     `json:"min_balance_for_yield"`
	CompoundFrequency  *string    `json:"compound_frequency"`
	IsActive           bool       `json:"is_active"`
	EffectiveFrom      time.Time  `json:"effective_from"`
	EffectiveUntil     *time.Time `json:"effective_until"`
	Notes              *string    `json:"notes"`
}

// UpdateYieldConfig updates an existing yield configuration
func (ys *YieldService) UpdateYieldConfig(ctx context.Context, configID uuid.UUID, params UpdateYieldConfigParams) error {
	ys.logger.Info(fmt.Sprintf("Updating yield config %s", configID))

	args := db.UpdateYieldConfigParams{
		ID:                 configID,
		ApyRate:            sql.NullString{String: params.ApyRate, Valid: true},
		MinBalanceForYield: sql.NullString{String: params.MinBalanceForYield, Valid: true},
		CompoundFrequency:  sql.NullString{String: *params.CompoundFrequency, Valid: params.CompoundFrequency != nil},
		IsActive:           sql.NullBool{Bool: params.IsActive, Valid: true},
		EffectiveUntil:     sql.NullTime{Time: *params.EffectiveUntil, Valid: params.EffectiveUntil != nil},
		Notes:              sql.NullString{String: *params.Notes, Valid: params.Notes != nil},
	}
	if err := ys.store.UpdateYieldConfig(ctx, args); err != nil {
		return fmt.Errorf("failed to update yield config: %w", err)
	}

	return nil
}

// ============================================================================
// NOTIFICATIONS
// ============================================================================

// sendYieldNotification sends email and push notifications about yield earned
func (ys *YieldService) sendYieldNotification(vault *db.VaultSaving, result *YieldCalculationResult) {
	ctx := context.Background()

	// Get user details
	user, err := ys.store.GetUserByID(ctx, vault.UserID)
	if err != nil {
		ys.logger.Error(fmt.Sprintf("Failed to get user for notification: %v", err))
		return
	}

	// Send email
	if err := ys.emailService.SendYieldCredited(
		ctx,
		&user,
		vault.VaultName,
		result.YieldAmount,
		vault.Currency,
		result.EndBalance,
		result.Reference,
	); err != nil {
		ys.logger.Error(fmt.Sprintf("Failed to send yield email: %v", err))
	}

	// Send push notification
	if err := ys.pushService.SendYieldCredited(
		ctx,
		vault.UserID,
		vault.VaultName,
		result.YieldAmount,
		vault.Currency,
	); err != nil {
		ys.logger.Error(fmt.Sprintf("Failed to send yield push: %v", err))
	}
}

// checkGoalCompletion checks if vault goal is reached after yield credit
func (ys *YieldService) checkGoalCompletion(vaultID uuid.UUID, newBalance, goalAmount string) {
	balance, _ := decimal.NewFromString(newBalance)
	goal, _ := decimal.NewFromString(goalAmount)

	if balance.GreaterThanOrEqual(goal) && goal.GreaterThan(decimal.Zero) {
		ys.logger.Info(fmt.Sprintf("Vault %s reached goal after yield credit!", vaultID))
		// TODO: Trigger goal completion notification
	}
}
