package subscriptions

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bridgecards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	store      *db.Store
	logger     *logging.Logger
	bridgeCard *bridgecards.BridgeCardProvider
}

func NewService(store *db.Store, logger *logging.Logger, bridgeCard *bridgecards.BridgeCardProvider) *Service {
	return &Service{
		store:      store,
		logger:     logger,
		bridgeCard: bridgeCard,
	}
}

// SystemSettings holds cached system settings
type SystemSettings struct {
	DefaultRenewalIntervalDays    int
	AllowDailyBilling             bool
	AllowYearlyBilling            bool
	DefaultAutoTopupBufferPercent decimal.Decimal
	MaxAutoTopupAmount            int64
	MinAutoTopupAmount            int64
	AutoTopupEnabledByDefault     bool
	DefaultReminderDaysBefore     int
	EnableSameDayReminders        bool
	EnableMultiReminderSchedule   bool
	MaxCustomSubscriptionsPerUser int
	MinSubscriptionAmount         int64
	MaxSubscriptionAmount         int64
}

// CreateCustomSubscriptionRequest defines parameters for creating custom subscription
type CreateCustomSubscriptionRequest struct {
	CardID                 uuid.UUID
	MerchantName           string
	DisplayName            string
	Category               string
	Amount                 int64
	Currency               string
	BillingCycle           string // 'daily', 'monthly', 'yearly'
	FirstChargeDate        time.Time
	ReminderEnabled        bool
	CustomReminderTiming   *int // Optional override
	AutoTopupEnabled       bool
	AutoTopupBufferPercent *decimal.Decimal // Optional override
	Notes                  string
}

// UpdateCustomSubscriptionRequest defines parameters for updating custom subscription
type UpdateCustomSubscriptionRequest struct {
	DisplayName            *string
	Amount                 *int64
	BillingCycle           *string
	ReminderEnabled        *bool
	CustomReminderTiming   *int
	AutoTopupBufferPercent *decimal.Decimal
	Notes                  *string
}

// LoadSystemSettings loads and caches system settings
func (s *Service) LoadSystemSettings(ctx context.Context) (*SystemSettings, error) {
	settings := &SystemSettings{}

	// Load renewal settings
	if val, err := s.getSettingInt(ctx, "default_renewal_interval_days"); err == nil {
		settings.DefaultRenewalIntervalDays = val
	} else {
		settings.DefaultRenewalIntervalDays = 30
	}

	settings.AllowDailyBilling, _ = s.getSettingBool(ctx, "allow_daily_billing")
	settings.AllowYearlyBilling, _ = s.getSettingBool(ctx, "allow_yearly_billing")

	// Load auto topup settings
	if val, err := s.getSettingDecimal(ctx, "default_auto_topup_buffer_percent"); err == nil {
		settings.DefaultAutoTopupBufferPercent = val
	} else {
		settings.DefaultAutoTopupBufferPercent = decimal.NewFromFloat(10.0)
	}

	// All monetary settings are stored in dollars (integer units)
	settings.MaxAutoTopupAmount, _ = s.getSettingInt64(ctx, "max_auto_topup_amount")
	settings.MinAutoTopupAmount, _ = s.getSettingInt64(ctx, "min_auto_topup_amount")
	settings.AutoTopupEnabledByDefault, _ = s.getSettingBool(ctx, "auto_topup_enabled_by_default")

	// Load reminder settings
	if val, err := s.getSettingInt(ctx, "default_reminder_days_before"); err == nil {
		settings.DefaultReminderDaysBefore = val
	} else {
		settings.DefaultReminderDaysBefore = 3
	}

	settings.EnableSameDayReminders, _ = s.getSettingBool(ctx, "enable_same_day_reminders")
	settings.EnableMultiReminderSchedule, _ = s.getSettingBool(ctx, "enable_multi_reminder_schedule")

	// Load limits
	if val, err := s.getSettingInt(ctx, "max_custom_subscriptions_per_user"); err == nil {
		settings.MaxCustomSubscriptionsPerUser = val
	} else {
		settings.MaxCustomSubscriptionsPerUser = 50
	}

	settings.MinSubscriptionAmount, _ = s.getSettingInt64(ctx, "min_subscription_amount")
	settings.MaxSubscriptionAmount, _ = s.getSettingInt64(ctx, "max_subscription_amount")

	s.logger.Infof("Loaded system settings: %+v", settings)
	return settings, nil
}

// Helper functions to get settings
func (s *Service) getSettingInt(ctx context.Context, key string) (int, error) {
	setting, err := s.store.GetSystemSetting(ctx, key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(setting.SettingValue)
}

func (s *Service) getSettingInt64(ctx context.Context, key string) (int64, error) {
	setting, err := s.store.GetSystemSetting(ctx, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(setting.SettingValue, 10, 64)
}

func (s *Service) getSettingBool(ctx context.Context, key string) (bool, error) {
	setting, err := s.store.GetSystemSetting(ctx, key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(setting.SettingValue)
}

func (s *Service) getSettingDecimal(ctx context.Context, key string) (decimal.Decimal, error) {
	setting, err := s.store.GetSystemSetting(ctx, key)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(setting.SettingValue)
}

func (s *Service) CreateCustomSubscription(ctx context.Context, userID int64, req CreateCustomSubscriptionRequest) (*db.UserSubscription, error) {
	// load system settings
	settings, err := s.LoadSystemSettings(ctx)
	if err != nil {
		return nil, err
	}

	// Valdate user has not exceeded imit
	count, err := s.store.GetCustomSubscriptionCount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get custom subscription count: %v", err)
	}
	if count >= int64(settings.MaxCustomSubscriptionsPerUser) {
		return nil, fmt.Errorf("user has exceeded maximum number of subscriptions: %d", settings.MaxCustomSubscriptionsPerUser)
	}

	// validate amount (all amounts are in whole dollars)
	if req.Amount < settings.MinSubscriptionAmount || req.Amount > settings.MaxSubscriptionAmount {
		return nil, fmt.Errorf("amount must be between %d and %d dollars", settings.MinSubscriptionAmount, settings.MaxSubscriptionAmount)
	}

	// Validate billing cycle
	var billingIntervalDays int32
	switch req.BillingCycle {
	case "daily":
		if !settings.AllowDailyBilling {
			return nil, fmt.Errorf("daily billing is not allowed")
		}
		billingIntervalDays = 1
	case "monthly":
		billingIntervalDays = 30
	case "yearly":
		if !settings.AllowYearlyBilling {
			return nil, fmt.Errorf("yearly billing is not allowed")
		}
		billingIntervalDays = 365
	default:
		return nil, fmt.Errorf("invalid billing cycle: %s (must be 'daily', 'monthly', or 'yearly')", req.BillingCycle)
	}

	// calculate dates
	nextChargeDate := req.FirstChargeDate.AddDate(0, 0, int(billingIntervalDays))

	// set reminder timimg
	reminderDays := settings.DefaultReminderDaysBefore
	if req.CustomReminderTiming != nil {
		reminderDays = *req.CustomReminderTiming
	}

	// set auto topup buffer
	autoTopupBufferPercent := settings.DefaultAutoTopupBufferPercent
	if req.AutoTopupBufferPercent != nil {
		autoTopupBufferPercent = *req.AutoTopupBufferPercent
	}

	subscription, err := s.store.CreateCustomSubscription(ctx, db.CreateCustomSubscriptionParams{
		UserID:                  userID,
		CardID:                  req.CardID,
		MerchantName:            req.MerchantName,
		DisplayName:             req.DisplayName,
		Category:                sql.NullString{String: req.Category, Valid: req.Category != ""},
		Amount:                  req.Amount,
		Currency:                req.Currency,
		BillingIntervalDays:     billingIntervalDays,
		FirstChargeDate:         req.FirstChargeDate,
		NextEstimatedChargeDate: nextChargeDate,
		Status:                  "active",
		ConfidenceScore:         decimal.NewFromFloat(1.0).String(),
		ReminderEnabled:         req.ReminderEnabled,
		ReminderDaysBefore:      int32(reminderDays),
		IsCustom:                true,
		CustomBillingCycle:      sql.NullString{String: req.BillingCycle, Valid: true},
		CustomAmountOverride:    true,
		AutoTopupBufferPercent:  sql.NullString{String: autoTopupBufferPercent.String(), Valid: true},
		CustomReminderTiming:    sql.NullInt32{Int32: int32(reminderDays), Valid: true},
		Notes:                   sql.NullString{String: req.Notes, Valid: req.Notes != ""},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create custom subscription: %v", err)
	}
	return &subscription, nil
}

// UpdateCustomSubscription updates a user-defined subscription
func (s *Service) UpdateCustomSubscription(ctx context.Context, userID int64, subscriptionID uuid.UUID, req *UpdateCustomSubscriptionRequest) (*db.UserSubscription, error) {
	// Get existing subscription
	existing, err := s.store.GetUserSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}

	if existing.UserID != userID {
		return nil, fmt.Errorf("subscription does not belong to user")
	}

	if !existing.IsCustom {
		return nil, fmt.Errorf("cannot update automatically detected subscription")
	}

	// Load system settings for validation
	settings, err := s.LoadSystemSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load system settings: %w", err)
	}

	// Validate amount if being updated
	if req.Amount != nil {
		if *req.Amount < settings.MinSubscriptionAmount {
			return nil, fmt.Errorf("amount must be at least $%d", settings.MinSubscriptionAmount)
		}
		if *req.Amount > settings.MaxSubscriptionAmount {
			return nil, fmt.Errorf("amount cannot exceed $%d", settings.MaxSubscriptionAmount)
		}
	}

	// Calculate new billing interval if cycle is being updated
	var newBillingInterval *int32
	if req.BillingCycle != nil {
		switch *req.BillingCycle {
		case "daily":
			if !settings.AllowDailyBilling {
				return nil, fmt.Errorf("daily billing is not allowed")
			}
			interval := int32(1)
			newBillingInterval = &interval
		case "monthly":
			interval := int32(30)
			newBillingInterval = &interval
		case "yearly":
			if !settings.AllowYearlyBilling {
				return nil, fmt.Errorf("yearly billing is not allowed")
			}
			interval := int32(365)
			newBillingInterval = &interval
		default:
			return nil, fmt.Errorf("invalid billing cycle: %s", *req.BillingCycle)
		}
	}

	// Build update params
	updateParams := db.UpdateCustomSubscriptionParams{
		ID:     subscriptionID,
		UserID: userID,
	}

	if req.DisplayName != nil {
		updateParams.DisplayName = sql.NullString{String: *req.DisplayName, Valid: true}
	}

	if req.Amount != nil {
		updateParams.Amount = sql.NullInt64{Int64: *req.Amount, Valid: true}
	}

	if newBillingInterval != nil {
		updateParams.BillingIntervalDays = sql.NullInt32{Int32: *newBillingInterval, Valid: true}
	}

	if req.BillingCycle != nil {
		updateParams.CustomBillingCycle = sql.NullString{String: *req.BillingCycle, Valid: true}
	}

	if req.ReminderEnabled != nil {
		updateParams.ReminderEnabled = sql.NullBool{Bool: *req.ReminderEnabled, Valid: true}
	}

	if req.CustomReminderTiming != nil {
		updateParams.CustomReminderTiming = sql.NullInt32{Int32: int32(*req.CustomReminderTiming), Valid: true}
	}

	if req.AutoTopupBufferPercent != nil {
		updateParams.AutoTopupBufferPercent = sql.NullString{String: req.AutoTopupBufferPercent.String(), Valid: true}
	}

	if req.Notes != nil {
		updateParams.Notes = sql.NullString{String: *req.Notes, Valid: true}
	}

	// Update subscription
	updated, err := s.store.UpdateCustomSubscription(ctx, updateParams)
	if err != nil {
		return nil, fmt.Errorf("update custom subscription: %w", err)
	}

	s.logger.Infof("Updated custom subscription %s for user %d", subscriptionID, userID)

	return &updated, nil
}

// DetectAndLogSubscription analyzes a card transaction to detect if it's a subscription
func (s *Service) DetectAndLogSubscription(ctx context.Context, transaction *db.CardTransaction) error {
	// Only process approved debit transactions
	if transaction.Status != "successful" || transaction.TransactionType != "DEBIT" {
		return nil
	}

	// Check if merchant exists in our subscription database
	merchant, err := s.store.FindSubscriptionMerchantByPattern(ctx, transaction.MerchantName.String)
	var merchantID sql.NullInt64
	var category, displayName string
	var confidenceScore decimal.Decimal

	if err == nil && merchant.ID != 0 {
		// Known subscription merchant
		merchantID = sql.NullInt64{Int64: merchant.ID, Valid: true}
		category = merchant.Category
		displayName = merchant.DisplayName
		confidenceScore = decimal.RequireFromString("0.9")

		s.logger.Infof("Detected known subscription merchant: %s", displayName)
	} else {
		// Unknown merchant - use heuristics to detect subscription pattern
		if !s.isLikelySubscription(ctx, transaction) {
			return nil
		}
		category = "other"
		displayName = transaction.MerchantName.String
		confidenceScore = decimal.RequireFromString("0.5")

		s.logger.Infof("Detected potential subscription from transaction pattern: %s", displayName)
	}

	// Check if subscription already exists
	existingSub, err := s.store.FindExistingSubscription(ctx, db.FindExistingSubscriptionParams{
		UserID: transaction.UserID,
		CardID: transaction.CardID,
		Lower:  transaction.MerchantName.String,
	})

	if err == nil && existingSub.ID != uuid.Nil {
		// Update existing subscription
		return s.updateExistingSubscription(ctx, existingSub, transaction)
	}

	return s.createNewSubscription(ctx, transaction, merchantID, category, displayName, confidenceScore)
}

// isLikelySubscription uses heuristics to detect if a transaction is likely a subscription
func (s *Service) isLikelySubscription(ctx context.Context, transaction *db.CardTransaction) bool {
	// Get previous transactions from same merchant
	previousTxs, err := s.store.GetTransactionsByMerchant(ctx, db.GetTransactionsByMerchantParams{
		CardID: transaction.CardID,
		Lower:  transaction.MerchantName.String,
	})

	if err != nil || len(previousTxs) < 2 {
		return false
	}

	// Check for recurring pattern (similar amounts at regular intervals)
	var intervals []int
	var amounts []int64

	for i := 1; i < len(previousTxs); i++ {
		daysDiff := int(previousTxs[i-1].TransactionDate.Sub(previousTxs[i].TransactionDate).Hours() / 24)
		intervals = append(intervals, daysDiff)
		amounts = append(amounts, previousTxs[i].Amount)
	}

	// Check if intervals are consistent (within 5 days variance for monthly)
	if len(intervals) >= 2 {
		avgInterval := average(intervals)
		if isNearlyEqual(avgInterval, 30, 5) || isNearlyEqual(avgInterval, 365, 30) {
			// Check if amounts are similar
			avgAmount := averageInt64(amounts)
			if isAmountConsistent(amounts, avgAmount, 0.1) {
				return true
			}
		}
	}

	return false
}

// createNewSubscription creates a new subscription record
func (s *Service) createNewSubscription(
	ctx context.Context,
	transaction *db.CardTransaction,
	merchantID sql.NullInt64,
	category, displayName string,
	confidenceScore decimal.Decimal,
) error {
	// Calculate billing interval (default 30 days for monthly)
	billingInterval := int32(30)

	// Predict next charge date
	nextChargeDate := transaction.TransactionDate.AddDate(0, 1, 0)

	_, err := s.store.CreateUserSubscription(ctx, db.CreateUserSubscriptionParams{
		UserID:                  transaction.UserID,
		CardID:                  transaction.CardID,
		MerchantID:              merchantID,
		MerchantName:            transaction.MerchantName.String,
		DisplayName:             displayName,
		Category:                sql.NullString{String: category, Valid: true},
		Amount:                  transaction.Amount,
		Currency:                transaction.Currency,
		BillingIntervalDays:     billingInterval,
		FirstChargeDate:         transaction.TransactionDate,
		LastChargeDate:          transaction.TransactionDate,
		NextEstimatedChargeDate: nextChargeDate,
		Status:                  "active",
		ConfidenceScore:         confidenceScore.String(),
		ReminderEnabled:         true,
		ReminderDaysBefore:      3,
	})

	if err != nil {
		return fmt.Errorf("create user subscription: %w", err)
	}

	s.logger.Infof("Created new subscription for user %d: %s ($%.2f every %d days)",
		transaction.UserID, displayName, float64(transaction.Amount), billingInterval)

	return nil
}

// updateExistingSubscription updates an existing subscription with new transaction
func (s *Service) updateExistingSubscription(
	ctx context.Context,
	subscription db.UserSubscription,
	transaction *db.CardTransaction,
) error {
	// Calculate new billing interval based on actual charge dates
	daysSinceLastCharge := int32(transaction.TransactionDate.Sub(subscription.LastChargeDate).Hours() / 24)

	// Use actual interval if it's reasonable (between 25-35 days for monthly or 350-380 for yearly)
	newInterval := subscription.BillingIntervalDays
	if daysSinceLastCharge >= 25 && daysSinceLastCharge <= 35 {
		newInterval = 30
	} else if daysSinceLastCharge >= 350 && daysSinceLastCharge <= 380 {
		newInterval = 365
	}

	nextChargeDate := transaction.TransactionDate.Add(time.Duration(newInterval) * 24 * time.Hour)

	_, err := s.store.UpdateSubscriptionCharge(ctx, db.UpdateSubscriptionChargeParams{
		ID:                      subscription.ID,
		LastChargeDate:          transaction.TransactionDate,
		NextEstimatedChargeDate: nextChargeDate,
		Amount:                  transaction.Amount,
	})

	if err != nil {
		return fmt.Errorf("update subscription charge: %w", err)
	}

	// Link transaction to subscription
	_, err = s.store.LinkTransactionToSubscription(ctx, db.LinkTransactionToSubscriptionParams{
		ID:             uuid.MustParse(transaction.ID.String()),
		SubscriptionID: uuid.NullUUID{UUID: subscription.ID, Valid: true},
	})

	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to link transaction to subscription: %v", err))
	}

	s.logger.Infof("Updated subscription %s with new charge. Next renewal: %s",
		subscription.DisplayName, nextChargeDate.Format("2006-01-02"))

	return nil
}

// ProcessRenewalReminders checks for upcoming renewals and creates reminder notifications
func (s *Service) ProcessRenewalReminders(ctx context.Context, daysBeforeStr string, limit int32) error {
	subscriptions, err := s.store.GetSubscriptionsDueForReminder(ctx, db.GetSubscriptionsDueForReminderParams{
		Column1: sql.NullString{String: daysBeforeStr, Valid: true},
		Limit:   limit,
	})

	if err != nil {
		return fmt.Errorf("get subscriptions due for reminder: %w", err)
	}

	s.logger.Infof("Processing %d subscription reminders", len(subscriptions))

	for _, sub := range subscriptions {
		if err := s.createRenewalReminder(ctx, sub); err != nil {
			s.logger.Error(fmt.Sprintf("Failed to create reminder for subscription %s: %v",
				sub.ID, err))
			continue
		}
	}

	return nil
}

// createRenewalReminder creates a reminder notification for upcoming renewal
func (s *Service) createRenewalReminder(ctx context.Context, subscription db.GetSubscriptionsDueForReminderRow) error {
	daysUntilRenewal := int(time.Until(subscription.NextEstimatedChargeDate).Hours() / 24)

	title := fmt.Sprintf("%s Renewal Coming Up", subscription.DisplayName)
	message := fmt.Sprintf("Your %s subscription ($%.2f) will renew in %d days. Make sure your card has sufficient balance.",
		subscription.DisplayName, float64(subscription.Amount), daysUntilRenewal)

	_, err := s.store.CreateSubscriptionReminder(ctx, db.CreateSubscriptionReminderParams{
		SubscriptionID: subscription.ID,
		UserID:         subscription.UserID,
		ReminderType:   "upcoming_renewal",
		ScheduledFor:   time.Now(),
		Title:          title,
		Message:        message,
		ActionUrl:      sql.NullString{String: fmt.Sprintf("/cards/%s/fund", subscription.CardID), Valid: true},
		Channels:       []string{"push", "email"},
		Status:         "pending",
	})

	if err != nil {
		return fmt.Errorf("create subscription reminder: %w", err)
	}

	s.logger.Infof("Created renewal reminder for user %d: %s", subscription.UserID, subscription.DisplayName)
	return nil
}

// HandleFailedRenewal processes a failed subscription renewal
func (s *Service) HandleFailedRenewal(ctx context.Context, subscriptionID uuid.UUID, reason string) error {
	subscription, err := s.store.GetUserSubscription(ctx, subscriptionID)
	if err != nil {
		return fmt.Errorf("get subscription: %w", err)
	}

	// Update subscription with failure
	_, err = s.store.UpdateSubscriptionFailure(ctx, db.UpdateSubscriptionFailureParams{
		ID:                subscription.ID,
		LastFailedDate:    sql.NullTime{Time: time.Now(), Valid: true},
		LastFailureReason: sql.NullString{String: reason, Valid: true},
	})

	if err != nil {
		return fmt.Errorf("update subscription failure: %w", err)
	}

	// Create failure notification
	title := fmt.Sprintf("%s Payment Failed", subscription.DisplayName)
	message := fmt.Sprintf("Your %s subscription payment of $%.2f failed. Reason: %s. Please fund your card to avoid service interruption.",
		subscription.DisplayName, float64(subscription.Amount), reason)

	_, err = s.store.CreateSubscriptionReminder(ctx, db.CreateSubscriptionReminderParams{
		SubscriptionID: subscription.ID,
		UserID:         subscription.UserID,
		ReminderType:   "payment_failed",
		ScheduledFor:   time.Now(),
		Title:          title,
		Message:        message,
		ActionUrl:      sql.NullString{String: fmt.Sprintf("/cards/%s/fund", subscription.CardID), Valid: true},
		Channels:       []string{"push", "email"},
		Status:         "pending",
	})

	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create failure notification: %v", err))
	}

	return nil
}

// CheckAndAutoTopUp checks cards with upcoming renewals and auto-tops them up if enabled
func (s *Service) CheckAndAutoTopUp(ctx context.Context) error {
	// Get subscriptions renewing in the next 24 hours
	upcomingSubs, err := s.store.GetSubscriptionsDueForReminder(ctx, db.GetSubscriptionsDueForReminderParams{
		Column1: sql.NullString{String: "1", Valid: true},
		Limit:   100,
	})

	if err != nil {
		return fmt.Errorf("get upcoming subscriptions: %w", err)
	}

	s.logger.Infof("Checking %d subscriptions for auto top-up", len(upcomingSubs))

	for _, sub := range upcomingSubs {
		if err := s.checkAndTopUpCard(ctx, sub); err != nil {
			s.logger.Error(fmt.Sprintf("Auto top-up failed for subscription %s: %v", sub.ID, err))
		}
	}

	return nil
}

// checkAndTopUpCard checks if card needs top-up and performs it if enabled.
// It uses live BridgeCard balances and triggers funding via the BridgeCard provider.
func (s *Service) checkAndTopUpCard(ctx context.Context, subscription db.GetSubscriptionsDueForReminderRow) error {
	// Get card details (contains auto-topup config and BridgeCard ID)
	card, err := s.store.GetVirtualCard(ctx, subscription.CardID)
	if err != nil {
		return fmt.Errorf("get card: %w", err)
	}

	// get user wallet
	wallet, err := s.store.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: subscription.UserID,
		Currency:   subscription.Currency,
	})
	if err != nil {
		return fmt.Errorf("failed to get user usd wallet: %v", err)
	}

	// Check if auto top-up is enabled at card level
	if !card.AutoTopupEnabled {
		return nil
	}

	// Ensure auto top-up configuration is complete
	if !card.AutoTopupThreshold.Valid || !card.AutoTopupAmount.Valid {
		return fmt.Errorf("auto top-up configuration incomplete")
	}

	if s.bridgeCard == nil {
		return fmt.Errorf("bridge card provider is not configured")
	}

	// Get current card balance from BridgeCard (book balance is the most conservative)
	balanceResp, err := s.bridgeCard.GetCardBalance(ctx, card.BridgecardCardID)
	if err != nil {
		return fmt.Errorf("get card balance from bridgecard: %w", err)
	}

	currentBalanceCents, err := strconv.Atoi(balanceResp.Data.SettledBookBalance)
	if err != nil {
		// Fallback to generic balance field if book balance cannot be parsed
		currentBalanceCents, err = strconv.Atoi(balanceResp.Data.Balance)
		if err != nil {
			return fmt.Errorf("parse card balance: %w", err)
		}
	}

	thresholdCents := card.AutoTopupThreshold.Int64

	// Subscription amount is stored in whole dollars; convert to cents
	subscriptionAmountCents := subscription.Amount * 100

	// Project balance after upcoming subscription charge
	projectedBalance := int64(currentBalanceCents) - subscriptionAmountCents
	if projectedBalance >= thresholdCents {
		// No top-up needed
		return nil
	}

	// Determine top-up amount in cents from card configuration
	autoTopupAmountCents := card.AutoTopupAmount.Int64
	if autoTopupAmountCents <= 0 {
		return fmt.Errorf("invalid auto top-up amount: %d", autoTopupAmountCents)
	}

	// Convert cents to dollar string for logging only (funding call uses cents)
	autoTopupAmountDollars, err := utils.CentsStringToDollarString(strconv.FormatInt(autoTopupAmountCents, 10))
	if err != nil {
		return fmt.Errorf("convert auto top-up amount to dollars: %w", err)
	}

	s.logger.Infof(
		"Auto top-up triggered for card %s (subscription: %s, current_balance_cents=%d, projected_balance_cents=%d, threshold_cents=%d, topup_cents=%d [$%s])",
		card.ID, subscription.DisplayName, currentBalanceCents, projectedBalance, thresholdCents, autoTopupAmountCents, autoTopupAmountDollars,
	)

	walletbalance, err := utils.ToDecimal(wallet.Balance.String)
	if err != nil {
		return fmt.Errorf("wallet balance conversion error: %v", err)
	}

	topupAmount, err := utils.ToDecimal(autoTopupAmountDollars)
	if err != nil {
		return fmt.Errorf("topup amount conversion error: %v", err)
	}

	if walletbalance.LessThan(topupAmount) {
		return fmt.Errorf("insufficient funds to make auto topup")
	}

	// Trigger asynchronous card funding via BridgeCard.
	// NOTE: This funds the issuing wallet-backed card directly; wallet and ledger
	// debits should be handled separately if required by business rules.
	_, err = s.bridgeCard.FundCard(ctx, bridgecards.FundCardRequest{
		CardID:               card.BridgecardCardID,
		Amount:               strconv.FormatInt(autoTopupAmountCents, 10), // cents
		TransactionReference: utils.NewTxRef("auto_topup"),
		Currency:             "USD",
	})
	if err != nil {
		return fmt.Errorf("bridgecard auto top-up failed: %w", err)
	}

	// // start db transaction
	// tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	// if err != nil {
	// 	return fmt.Errorf("failed to start transaction: %w", err)
	// }
	// defer tx.Rollback()

	// qtx := s.store.WithTx(tx)

	// newWalletbalance := walletbalance.Sub(topupAmount)
	// _, err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
	// 	Amount: sql.NullString{String: newWalletbalance.String(), Valid: true},
	// 	ID:     wallet.ID,
	// })
	// if err != nil {
	// 	return fmt.Errorf("wallet update error: %v", err)
	// }

	// _, err = qtx.CreateCardFunding(ctx, db.CreateCardFundingParams{
	// 	CardID:         card.ID,
	// 	UserID:         subscription.UserID,
	// 	SourceWalletID: wallet.ID,
	// 	Currency:       subscription.Currency,
	// 	FundingType:    "auto_topup",
	// 	InitiatedBy:    "system",
	// 	Status:         "successful",
	// })

	// maintx, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
	// 	UserID:      sql.NullInt64{Int64: subscription.UserID, Valid: true},
	// 	Type:        string(transaction.Card),
	// 	Description: sql.NullString{String: "card auto_top_up", Valid: true},
	// 	Status:      string(transaction.Success),
	// })

	// _, err = qtx.CreateCardTransaction(ctx, db.CreateCardTransactionParams{
	// 	CardID: subscription.CardID,
	// 	UserID: card.UserID,
	// 	BridgecardTransactionID: fundResponse.Data.TransactionReference,
	// 	TransactionType: "CREDIT",

	// })

	// // commit transaction
	// if err := tx.Commit(); err != nil {
	// 	return fmt.Errorf("failed to commit transaction: %w", err)
	// }

	return nil
}

// GetUserSubscriptionSummary returns subscription statistics for a user
func (s *Service) GetUserSubscriptionSummary(ctx context.Context, userID int64) (*SubscriptionSummary, error) {
	summary, err := s.store.GetUserSubscriptionSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get subscription summary: %w", err)
	}

	categoryBreakdown, err := s.store.GetSubscriptionsByCategory(ctx, userID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get category breakdown: %v", err))
	}

	return &SubscriptionSummary{
		ActiveCount: int(summary.ActiveCount),
		FailedCount: int(summary.FailedCount),
		// SQL already returns total in dollars; keep as string but treat as dollars
		TotalMonthlySpend: summary.TotalMonthlySpend,
		NextChargeDate:    summary.NextChargeDate,
		CategoryBreakdown: categoryBreakdown,
	}, nil
}

// CalculateAutoTopupAmount calculates the auto topup amount based on subscription and buffer
func (s *Service) CalculateAutoTopupAmount(ctx context.Context, subscription *db.UserSubscription) (int64, error) {
	settings, err := s.LoadSystemSettings(ctx)
	if err != nil {
		return 0, fmt.Errorf("load system settings: %w", err)
	}

	// Get buffer percent
	bufferPercent := settings.DefaultAutoTopupBufferPercent
	if subscription.AutoTopupBufferPercent.Valid {
		bufferPercent, _ = decimal.NewFromString(subscription.AutoTopupBufferPercent.String)
	}

	// Calculate amount with buffer
	baseAmount := decimal.NewFromInt(subscription.Amount)
	bufferMultiplier := decimal.NewFromInt(100).Add(bufferPercent).Div(decimal.NewFromInt(100))
	topupAmount := baseAmount.Mul(bufferMultiplier)

	// Enforce limits
	topupAmountInt := topupAmount.IntPart()
	if topupAmountInt < settings.MinAutoTopupAmount {
		topupAmountInt = settings.MinAutoTopupAmount
	}
	if topupAmountInt > settings.MaxAutoTopupAmount {
		topupAmountInt = settings.MaxAutoTopupAmount
	}

	return topupAmountInt, nil
}

// ValidateBillingCycle validates if a billing cycle is allowed
func (s *Service) ValidateBillingCycle(ctx context.Context, cycle string) error {
	settings, err := s.LoadSystemSettings(ctx)
	if err != nil {
		return fmt.Errorf("load system settings: %w", err)
	}

	switch cycle {
	case "daily":
		if !settings.AllowDailyBilling {
			return fmt.Errorf("daily billing is not enabled")
		}
	case "monthly":
		return nil
	case "yearly":
		if !settings.AllowYearlyBilling {
			return fmt.Errorf("yearly billing is not enabled")
		}
	default:
		return fmt.Errorf("invalid billing cycle: %s", cycle)
	}

	return nil
}

// Helper functions
func average(numbers []int) int {
	if len(numbers) == 0 {
		return 0
	}
	sum := 0
	for _, n := range numbers {
		sum += n
	}
	return sum / len(numbers)
}

func averageInt64(numbers []int64) int64 {
	if len(numbers) == 0 {
		return 0
	}
	var sum int64
	for _, n := range numbers {
		sum += n
	}
	return sum / int64(len(numbers))
}

func isNearlyEqual(value, target, variance int) bool {
	diff := value - target
	if diff < 0 {
		diff = -diff
	}
	return diff <= variance
}

func isAmountConsistent(amounts []int64, avg int64, allowedVariance float64) bool {
	for _, amount := range amounts {
		diff := float64(amount-avg) / float64(avg)
		if diff < 0 {
			diff = -diff
		}
		if diff > allowedVariance {
			return false
		}
	}
	return true
}

// SubscriptionSummary holds subscription statistics
type SubscriptionSummary struct {
	ActiveCount       int                                `json:"active_count"`
	FailedCount       int                                `json:"failed_count"`
	TotalMonthlySpend string                             `json:"total_monthly_spend"`
	NextChargeDate    time.Time                          `json:"next_charge_date"`
	CategoryBreakdown []db.GetSubscriptionsByCategoryRow `json:"category_breakdown"`
}

// SubscriptionStats holds detailed subscription statistics
type SubscriptionStats struct {
	TotalSubscriptions    int64  `json:"total_subscriptions"`
	ActiveSubscriptions   int64  `json:"active_subscriptions"`
	InactiveSubscriptions int64  `json:"inactive_subscriptions"`
	MonthlySpendCents     int64  `json:"monthly_spend_dollars"`
	MonthlySpend          string `json:"monthly_spend"`
}

// AutoTopupSuccessRate holds auto topup success metrics
type AutoTopupSuccessRate struct {
	TotalAutoTopups      int64   `json:"total_auto_topups"`
	SuccessfulAutoTopups int64   `json:"successful_auto_topups"`
	SuccessRatePercent   float64 `json:"success_rate_percent"`
}

// GetSubscriptionStats returns detailed subscription statistics for a user
func (s *Service) GetSubscriptionStats(ctx context.Context, userID int64) (*SubscriptionStats, error) {
	stats, err := s.store.GetSubscriptionStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get subscription stats: %w", err)
	}

	return &SubscriptionStats{
		TotalSubscriptions:    stats.TotalSubscriptions,
		ActiveSubscriptions:   stats.ActiveSubscriptions,
		InactiveSubscriptions: stats.InactiveSubscriptions,
		// MonthlySpendCents and MonthlySpend are now in dollars
		MonthlySpendCents: stats.MonthlySpend,
		MonthlySpend:      fmt.Sprintf("%.2f", float64(stats.MonthlySpend)),
	}, nil
}

// GetAutoTopupSuccessRate returns auto topup success rate metrics for a user
func (s *Service) GetAutoTopupSuccessRate(ctx context.Context, userID int64) (*AutoTopupSuccessRate, error) {
	rate, err := s.store.GetAutoTopupSuccessRate(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get auto topup success rate: %w", err)
	}

	return &AutoTopupSuccessRate{
		TotalAutoTopups:      rate.TotalAutoTopups,
		SuccessfulAutoTopups: rate.SuccessfulAutoTopups,
		SuccessRatePercent:   float64(rate.SuccessRatePercentage),
	}, nil
}
