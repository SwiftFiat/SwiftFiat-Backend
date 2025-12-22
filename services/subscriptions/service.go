package subscriptions

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	store  *db.Store
	logger *logging.Logger
}

func NewService(store *db.Store, logger *logging.Logger) *Service {
	return &Service{
		store:  store,
		logger: logger,
	}
}

// DetectAndLogSubscription analyzes a card transaction to detect if it's a subscription
func (s *Service) DetectAndLogSubscription(ctx context.Context, transaction *db.CardTransaction) error {
	// Only process approved debit transactions
	if transaction.Status != "successful" || transaction.TransactionType != "debit" {
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
		AmountCents:             transaction.Amount,
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
		transaction.UserID, displayName, float64(transaction.Amount)/100, billingInterval)

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
		AmountCents:             transaction.Amount,
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
		subscription.DisplayName, float64(subscription.AmountCents)/100, daysUntilRenewal)

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
		subscription.DisplayName, float64(subscription.AmountCents)/100, reason)

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

// checkAndTopUpCard checks if card needs top-up and performs it if enabled
func (s *Service) checkAndTopUpCard(ctx context.Context, subscription db.GetSubscriptionsDueForReminderRow) error {
	// Get card details
	card, err := s.store.GetVirtualCard(ctx, subscription.CardID)
	if err != nil {
		return fmt.Errorf("get card: %w", err)
	}

	// Check if auto top-up is enabled
	if !card.AutoTopupEnabled {
		return nil
	}

	// TODO: Get current card balance from BridgeCard API
	// For now, we'll assume we need to check against threshold
	
	if !card.AutoTopupThresholdCents.Valid || !card.AutoTopupAmountCents.Valid {
		return fmt.Errorf("auto top-up configuration incomplete")
	}

	// If balance would be below threshold after subscription charge, top up
	s.logger.Infof("Auto top-up triggered for card %s (subscription: %s)", 
		card.ID, subscription.DisplayName)

	// TODO: Implement actual top-up logic using wallet service
	// This would call FundCard with the configured auto_topup_amount

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
		ActiveCount:         int(summary.ActiveCount),
		FailedCount:         int(summary.FailedCount),
		TotalMonthlySpend:   summary.TotalMonthlySpendCents,
		NextChargeDate:      summary.NextChargeDate,
		CategoryBreakdown:   categoryBreakdown,
	}, nil
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
	ActiveCount       int                                  `json:"active_count"`
	FailedCount       int                                  `json:"failed_count"`
	TotalMonthlySpend string                              `json:"total_monthly_spend"`
	NextChargeDate    time.Time                            `json:"next_charge_date"`
	CategoryBreakdown []db.GetSubscriptionsByCategoryRow   `json:"category_breakdown"`
}