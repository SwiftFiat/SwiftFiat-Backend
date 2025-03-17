package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	"github.com/shopspring/decimal"
)

/// This file is for tracking daily transactions for KYC users

// isSameDay checks if two times are on the same calendar day
func isSameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func (r *RedisService) TrackDailyTransactions(ctx context.Context, userID string, amount decimal.Decimal, currency string) error {
	// Create a key for tracking daily transactions
	key := fmt.Sprintf("daily_transactions:%s", userID)

	// Get current transaction data if it exists
	var transaction models.KYCTransaction
	err := r.client.HGetAll(ctx, key).Scan(&transaction)
	if err != nil {
		return fmt.Errorf("failed to get daily transactions: %w", err)
	}

	// If no transaction exists for today, create new one
	if transaction.CreatedAt.IsZero() || !isSameDay(transaction.CreatedAt, time.Now()) {
		transaction = models.KYCTransaction{
			UserID:      userID,
			TotalAmount: amount,
			Currency:    currency,
			CreatedAt:   time.Now(),
		}
	} else {
		// Add to existing total
		transaction.TotalAmount = transaction.TotalAmount.Add(amount)
		transaction.Currency = currency // Update currency in case it changed
	}

	// Store transaction data
	err = r.client.HSet(ctx, key, map[string]interface{}{
		"user_id":      transaction.UserID,
		"total_amount": transaction.TotalAmount.String(),
		"currency":     transaction.Currency,
		"created_at":   transaction.CreatedAt.Format(time.RFC3339),
	}).Err()
	if err != nil {
		return fmt.Errorf("failed to store daily transaction: %w", err)
	}

	// Set expiration for end of day
	midnight := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)
	err = r.client.ExpireAt(ctx, key, midnight).Err()
	if err != nil {
		return fmt.Errorf("failed to set expiration: %w", err)
	}

	return nil
}

func (r *RedisService) GetDailyTransactions(ctx context.Context, userID string, currency string) (models.KYCTransaction, error) {
	key := fmt.Sprintf("daily_transactions:%s", userID)

	// Get all fields
	fields, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return models.KYCTransaction{}, fmt.Errorf("failed to get daily transactions: %w", err)
	}

	// If no data exists
	if len(fields) == 0 {
		return models.KYCTransaction{
			UserID:      userID,
			TotalAmount: decimal.Zero,
			Currency:    currency,
			CreatedAt:   time.Now(),
		}, nil
	}

	// Parse the stored data
	createdAt, err := time.Parse(time.RFC3339, fields["created_at"])
	if err != nil {
		return models.KYCTransaction{}, fmt.Errorf("failed to parse created_at: %w", err)
	}

	amount, err := decimal.NewFromString(fields["total_amount"])
	if err != nil {
		return models.KYCTransaction{}, fmt.Errorf("failed to parse total_amount: %w", err)
	}

	transaction := models.KYCTransaction{
		UserID:      fields["user_id"],
		TotalAmount: amount,
		Currency:    fields["currency"],
		CreatedAt:   createdAt,
	}

	// If transaction is not from today, return empty transaction
	if !isSameDay(transaction.CreatedAt, time.Now()) {
		return models.KYCTransaction{
			UserID:      userID,
			TotalAmount: decimal.Zero,
			Currency:    currency,
			CreatedAt:   time.Now(),
		}, nil
	}

	return transaction, nil
}
