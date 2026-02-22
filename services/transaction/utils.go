package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CheckFirstConversionAndDisburseReferralBonus checks if the user has completed their first conversion, updates their record accordingly,
// and credits their referrer with the Onre time referral bonus if applicable.
//
//	It returns the referrer's ID and the referral bonus amount if successful,
//
// or nil if the user has no referrer or has already completed a conversion.
func CheckFirstConersionAndDisburseReferralBonus(ctx context.Context, store *db.Store, dbTx *sql.Tx, userID int64, cID uuid.UUID) (*int64, *decimal.Decimal, error) {
	if err := store.UpdateUserHasCompletedFirstConversion(ctx, userID); err != nil {
		return nil, nil, fmt.Errorf("failed to update user has completed first conversion: %w", err)
	}

	if err := store.UpdateUserFirstConversionID(ctx, db.UpdateUserFirstConversionIDParams{
		ID:                userID,
		FirstConversionID: uuid.NullUUID{UUID: cID, Valid: true},
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to update user first conversion ID: %w", err)
	}

	if err := store.UpdateUserFirstConversionAt(ctx, db.UpdateUserFirstConversionAtParams{
		ID: userID,
		FirstConversionAt: sql.NullTime{
			Time:  time.Now(),
			Valid: true,
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to update user first conversion at: %w", err)
	}

	// Check if the user has a referrer
	referral, err := store.WithTx(dbTx).GetReferralByRefereeID(ctx, int32(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No referrer found, nothing to do
			return nil, nil, fmt.Errorf("get referral by referee ID: %w", err)
		}
		return nil, nil, nil
	}

	// Get the referrer's ID and referral bonus amount
	referrerID := int64(referral.ReferrerID)
	referralBonus, err := decimal.NewFromString(referral.EarnedAmount)
	if err != nil {
		return nil, nil, fmt.Errorf("convert referral bonus to decimal: %w", err)
	}

	amountUsd, err := utils.ConvertToUSD(ctx, referralBonus, "NGN")
	if err != nil {
		return nil, nil, fmt.Errorf("convert referral bonus to USD: %w", err)
	}

	// Create a transaction for the referral bonus
	txx, err := store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          referrerID,
		Amount:          referralBonus.String(),
		AmountUsd:       amountUsd.String(),
		Type:            string(Referral),
		Description:     sql.NullString{String: "Referral bonus for referring a user", Valid: true},
		TransactionFlow: "inplatform",
		Currency:        "NGN",
		IdempotencyKey:  uuid.New().String(),
		TFrom:           "Platform",
		TTo:             "Referral",
		Direction:       "credit",
		Status:          "pending",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to creating tx record for referral bonus: %w", err)
	}

	// create tx records for referral bonus
	refTx, err := store.WithTx(dbTx).CreateReferralTransaction(ctx, db.CreateReferralTransactionParams{
		UserID:          int32(referrerID),
		Amount:          referralBonus.String(),
		TransactionID:   uuid.NullUUID{UUID: txx.ID, Valid: true},
		TransactionType: "credit",
		Status:          "pending",
		Reference:       utils.NewTxRef("r_"),
	})
	if err != nil {
		return nil, nil, err
	}

	// Update the referrer's earnings
	params := db.UpdateReferralEarningsParams{
		UserID:      int32(referrerID),
		TotalEarned: referralBonus.String(),
	}

	if _, err := store.WithTx(dbTx).UpdateReferralEarnings(ctx, params); err != nil {
		return nil, nil, fmt.Errorf("update referral earnings: %w", err)
	}

	_, err = store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     txx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update tx record: %w", err)
	}

	err = store.WithTx(dbTx).UpdateReferralTransactionStatus(ctx, db.UpdateReferralTransactionStatusParams{
		ID:     refTx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update referral tx record: %w", err)
	}

	err = store.WithTx(dbTx).UpdateReferralStatus(ctx, db.UpdateReferralStatusParams{
		ID:     referral.ID,
		Status: "active",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("update referral status: %w", err)
	}

	return &referrerID, &referralBonus, nil
}

// CreditReferrerForConversion checks if the user has a referrer and credits them with a % of conversion amount if they do.
// It returns the referrer's ID and the referral bonus amount if successful,
// or nil if the user has no referrer.
func CreditReferrerForConversion(ctx context.Context, store *db.Store, dbTx *sql.Tx, userID int64, conversionAmount decimal.Decimal) (*int64, *decimal.Decimal, error) {
	// Validate input
    if conversionAmount.IsZero() || conversionAmount.IsNegative() {
        return nil, nil, fmt.Errorf("invalid conversion amount: %v", conversionAmount)
    }

	// Check if the user has a referrer
	referral, err := store.WithTx(dbTx).GetReferralByRefereeID(ctx, int32(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No referrer found, nothing to do
			return nil, nil, fmt.Errorf("get referral by referee ID: %w", err)
		}
		return nil, nil, fmt.Errorf("get referral by referee ID: %w", err)
	}

	// Get the referrer's ID and referral bonus amount
	referrerID := int64(referral.ReferrerID)
	referralConfig, err := store.WithTx(dbTx).GetReferralConfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get referral config: %w", err)
	}

	// Calculate referral bonus as a percentage of conversion amount
	pecentageEarned, err := utils.ToFloat(referralConfig.ReferralPercentageEarnedPerConversion)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert referral percentage to float: %w", err)
	}
	referralBonus := conversionAmount.Mul(decimal.NewFromFloat(pecentageEarned)).Div(decimal.NewFromFloat(100))

	// Validate the calculated bonus
    if referralBonus.IsZero() {
        return nil, nil, fmt.Errorf("calculated referral bonus is zero")
    }

	rate, err := utils.GetNGNUSDRate(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get NGN to USD rate: %w", err)
	}

	amount := referralBonus.Mul(rate)

	// Create a transaction for the referral bonus
	txx, err := store.WithTx(dbTx).CreateTransaction(ctx, db.CreateTransactionParams{
		UserID:          referrerID,
		Amount:          referralBonus.String(),
		AmountUsd:       referralBonus.String(),
		Type:            string(Referral),
		Description:     sql.NullString{String: "Referral bonus for referring a user who completed a conversion", Valid: true},
		TransactionFlow: "inplatform",
		Currency:        "USD",
		IdempotencyKey:  uuid.New().String(),
		TFrom:           "Platform",
		TTo:             "Referral",
		Direction:       "credit",
		Status:          "pending",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to creating tx record for referral bonus: %w", err)
	}

	// create tx records for referral bonus
	refTx, err := store.WithTx(dbTx).CreateReferralTransaction(ctx, db.CreateReferralTransactionParams{
		UserID:          int32(referrerID),
		Amount:          referralBonus.String(),
		TransactionID:   uuid.NullUUID{UUID: txx.ID, Valid: true},
		TransactionType: "credit",
		Status:          "pending",
		Reference:       utils.NewTxRef("r_"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create referral transaction: %w", err)
	}

	// Update the referrer's earnings
	params := db.UpdateReferralEarningsParams{
		UserID:      int32(referrerID),
		TotalEarned: amount.String(),
	}

	if _, err := store.WithTx(dbTx).UpdateReferralEarnings(ctx, params); err != nil {
		return nil, nil, fmt.Errorf("update referral earnings failed: %w", err)
	}

	_, err = store.WithTx(dbTx).UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
		ID:     txx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update tx record: %w", err)
	}

	err = store.WithTx(dbTx).UpdateReferralTransactionStatus(ctx, db.UpdateReferralTransactionStatusParams{
		ID:     refTx.ID,
		Status: string(Success),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update referral tx record: %w", err)
	}

	return &referrerID, &referralBonus, nil
}
