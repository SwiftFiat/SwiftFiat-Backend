package referral

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type Service struct {
	repo    *Repo
	logger  *logging.Logger
	notifyr *service.Notification
	push    *service.PushNotificationService
	//notifier  NotificationService // assuming you have a notification service
}

func NewReferralService(repo *Repo, logger *logging.Logger, notifyr *service.Notification, push *service.PushNotificationService) *Service {
	return &Service{
		repo:    repo,
		logger:  logger,
		notifyr: notifyr,
		push:    push,
	}
}

func (s *Service) TrackReferral(ctx context.Context, referralCode string, refereeID uuid.UUID, referralAmount decimal.Decimal) (*Referral, error) {
	// Get the record of the referrer
	referralRecord, err := s.repo.queries.GetReferralByReferralKey(ctx, referralCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Error(fmt.Errorf("invalid referral code: %s", referralCode))
			return nil, errors.New("invalid referral code")
		}
		return nil, fmt.Errorf("an error occurred while fetching referral record: %w", err)
	}

	referrerID := referralRecord.UserID

	// Step 2: Check if referee was already referred
	existing, err := s.repo.queries.GetReferralByRefereeID(ctx, refereeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("an error occurred while checking existing referral: %w", err)
	}
	if existing != (db.UserReferral{}) {
		s.logger.Error(fmt.Errorf("referrer with id %d already exists", refereeID))
		return nil, errors.New("user already referred")
	}

	// Step 4: Create the referral
	referral, err := s.repo.CreateReferral(ctx, referrerID, refereeID, referralAmount, string(ReferralStatusPending))
	if err != nil {
		s.logger.Error(err)
		return nil, fmt.Errorf("an error occurred while creating referral: %w", err)
	}

	referedUser, err := s.repo.queries.GetUserByID(ctx, referral.RefereeID)
	if err != nil {
		s.logger.Errorf("failed to get user [TrackReferral]: %v", err)
		s.notifyr.CreateWithRecipients(
			ctx,
			nil,
			"SWIIFT Referral",
			"A user registered using your referral code. You'll earn a referral bonus on their first conversion",
			"system",
			[]uuid.UUID{referrerID},
		)
		return nil, err
	}

	s.push.NewReferral(ctx, refereeID, referedUser.UserTag.String)
	s.notifyr.CreateWithRecipients(
		ctx,
		nil,
		"SWIIFT Referral",
		fmt.Sprintf("user %s registered using your referral code, You'll earn a referral bonus on their first conversion", referedUser.UserTag.String),
		"system",
		[]uuid.UUID{referrerID},
	)

	return referral, nil
}

func (s *Service) GetUserReferrals(ctx context.Context, userID uuid.UUID) ([]Referral, error) {
	return s.repo.GetUserReferrals(ctx, userID)
}

func (s *Service) GetAllReferrals(ctx context.Context) ([]db.UserReferral, error) {
	return s.repo.GetAllReferrals(ctx)
}

func (s *Service) GetReferralEarnings(ctx context.Context, userID uuid.UUID) (*db.ReferralEarning, error) {
	return s.repo.GetReferralEarnings(ctx, userID)
}

func (s *Service) CreateReferralConfig(ctx context.Context, amount, threshold decimal.Decimal) (db.ReferralConfig, error) {
	return s.repo.CreateReferralConfig(ctx, amount, threshold)
}

func (s *Service) UpdateReferralConfig(ctx context.Context, id int64, amount, threshold *decimal.Decimal) (db.ReferralConfig, error) {
	return s.repo.UpdateReferralConfig(ctx, id, amount, threshold)
}

func (s *Service) GetReferralConfig(ctx context.Context) (db.ReferralConfig, error) {
	return s.repo.GetReferralConfig(ctx)
}

func (s *Service) Withdraw(ctx context.Context, amount decimal.Decimal, userID uuid.UUID, key string) (*db.Transaction, error) {
	var transx *db.Transaction

	kyc, err := s.repo.queries.GetKYCByUserID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Err_KYC_NOT_FOUND")
		}
		return nil, fmt.Errorf("failed to fetch KYC: %w", err)
	}

	if kyc.Tier == "tier_1" {
		go s.push.SendPushNotification(ctx, userID, "Verification required.", "This feature requires Tier 2 verification. Complete identity verification to continue")
		return nil, fmt.Errorf("Err_KYC_NEED_TIER_2")
	}

	s.logger.Infof("Processing referral withdrawal for user %d, amount: %s", userID, amount.String())
	err = s.repo.queries.ExecTx(ctx, func(q *db.Queries) (error) {
		t, err := q.GetTransactionByIdempotencyKey(ctx, key)
		if err == nil {
			transx = &t
			s.logger.Infof("Duplicate withdrawal request detected for user %d with amount %s. Ignoring.", userID, amount.String())
			return nil // Idempotent: ignore duplicate request
		}
		// Lock the user's wallet for update
		wallet, err := q.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
			CustomerID: userID,
			Currency:   string(transaction.NGN),
		})

		if err != nil {
			return fmt.Errorf("failed to get wallet: %w", err)
		}
		// Get referral earnings
		earnings, err := q.GetReferralEarnings(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get referral earnings: %w", err)
		}
		s.logger.Infof("User %d has available referral balance: %s", userID, earnings.AvailableBalance)

		availableBalance, err := decimal.NewFromString(earnings.AvailableBalance)
		if err != nil {
			return err
		}

		if amount.LessThanOrEqual(decimal.Zero) {
			return errors.New("withdrawal amount must be greater than zero")
		}

		if availableBalance.LessThan(amount) {
			return errors.New("insufficient available balance")
		}

		amountUsd, err := utils.ConvertToUSD(ctx, amount, "NGN")
		if err != nil {
			return err
		}

		tx, err := q.CreateTransaction(ctx, db.CreateTransactionParams{
			UserID:          userID,
			Type:            string(transaction.Referral),
			Currency:        string(transaction.NGN),
			Description:     sql.NullString{String: "Referral Bonus Withdrawal"},
			TransactionFlow: string(transaction.InPlatform),
			Amount:          amount.String(),
			AmountUsd:       amountUsd.String(),
			Status:          string(transaction.Pending),
			IdempotencyKey:  key,
			TFrom:           string(transaction.Referral),
			TTo:             string(transaction.Wallet),
			Direction:       string(transaction.Credit),
		})
		if err != nil {
			return fmt.Errorf("failed to create transaction: %w", err)
		}
		// s.logger.Infof("Created transaction %d for referral withdrawal of user %d", tx.ID, userID)

		// reference := utils.NewTxRef(fmt.Sprintf("rw_%s", uuid.New().String()))
		refTx, err := q.CreateReferralTransaction(ctx, db.CreateReferralTransactionParams{
			UserID:          userID,
			TransactionID:   uuid.NullUUID{UUID: tx.ID, Valid: true},
			TransactionType: string(transaction.Debit),
			Status:          string(transaction.Pending),
			Reference:       key,
			Amount:          tx.Amount,
		})
		if err != nil {
			return fmt.Errorf("failed to create referral transaction: %w", err)
		}

		// Deduct the amount from available balance
		updateParams := db.UpdateAvailableBalanceAfterWithdrawalParams{
			UserID:           userID,
			AvailableBalance: amount.String(),
		}
		if _, err := q.UpdateAvailableBalanceAfterWithdrawal(ctx, updateParams); err != nil {
			return err
		}
		// s.logger.Infof("Deducted %s from user %d's available referral balance", amount.String(), userID)

		// credit wallet
		_, err = q.IncrementWalletBalance(ctx, db.IncrementWalletBalanceParams{
			Balance: sql.NullString{String: amount.String(), Valid: true},
			ID:      wallet.ID,
		})
		if err != nil {
			return fmt.Errorf("failed to credit wallet: %w", err)
		}
		// s.logger.Infof("Credited %s to user %d's wallet", amount.String(), userID)

		// Update transaction status to success
		_, err = q.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     tx.ID,
			Status: string(transaction.Success),
		})
		if err != nil {
			return err
		}

		err = q.UpdateReferralTransactionStatus(ctx, db.UpdateReferralTransactionStatusParams{
			ID:     refTx.ID,
			Status: string(transaction.Success),
		})
		if err != nil {
			return fmt.Errorf("failed to update referral transaction status: %w", err)
		}

		transx = &tx

		return nil
	})
	// TODO: modify db tx so i can fail txs
	if err != nil {
		return nil, fmt.Errorf("withdrawal transaction failed: %w", err)
	}

	ctxBG := context.Background()
	go func() {
		s.notifyr.CreateWithRecipients(
			ctxBG,
			nil,
			"Referral Withdrawal",
			fmt.Sprintf("Your referral withdrawal of %s NGN has been processed successfully.", amount.String()),
			"system",
			[]uuid.UUID{userID},
		)
		s.push.CreditAlert(ctx, userID, amount.InexactFloat64(), "NGN")
	}()

	return transx, nil
}
