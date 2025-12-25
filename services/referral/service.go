package referral

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/shopspring/decimal"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

type Service struct {
	repo    *Repo
	logger  *logging.Logger
	notifyr *service.Notification
	//notifier  NotificationService // assuming you have a notification service
}

func NewReferralService(repo *Repo, logger *logging.Logger, notifyr *service.Notification) *Service {
	return &Service{
		repo:    repo,
		logger:  logger,
		notifyr: notifyr,
		//notifier: notifier,
	}
}

func (s *Service) TrackReferral(ctx context.Context, referralCode string, refereeID int64, referralAmount decimal.Decimal) (*Referral, error) {
	// Get the record of the referrer
	referralRecord, err := s.repo.queries.GetReferralByReferralKey(ctx, referralCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Error(fmt.Errorf("invalid referral code: %s", referralCode))
			return nil, errors.New("invalid referral code")
		}
		return nil, err
	}
	referrerID := int64(referralRecord.UserID)

	// Step 2: Check if referee was already referred
	existing, err := s.repo.queries.GetReferralByRefereeID(ctx, int32(refereeID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != (db.UserReferral{}) {
		s.logger.Error(fmt.Errorf("referrer with id %d already exists", refereeID))
		return nil, errors.New("user already referred")
	}

	// Step 4: Create the referral
	referral, err := s.repo.CreateReferral(ctx, referrerID, refereeID, referralAmount, string(ReferralStatusPending))
	if err != nil {
		s.logger.Error(err)
		return nil, err
	}

	// Check if referree kyc is 1
	kyc, err := s.repo.queries.GetKYCByUserID(ctx, int32(refereeID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Info(fmt.Sprintf("Referred user %d has not completed KYC yet", refereeID))
			s.notifyr.Create(ctx, int32(referrerID), "Referral", fmt.Sprintf("You have earned a referral bonus of %s, pending KYC approval of the referred user.", referralAmount.String()))
			return referral, nil
		}
		s.logger.Error(err)
		return nil, err
	}

	if kyc.Status == "active" && kyc.Tier >= 1 {
		params := db.UpdateReferralEarningsParams{
			UserID:      int32(referrerID),
			TotalEarned: referralAmount.String(),
		}

		_, err = s.repo.queries.UpdateReferralEarnings(ctx, params)
		if err != nil {
			s.logger.Error(err)
			return nil, err
		}
		err = s.repo.queries.UpdateReferralStatus(ctx, db.UpdateReferralStatusParams{
			Status:    string(ReferralStatusActive),
			RefereeID: int32(refereeID),
		})
		if err != nil {
			s.logger.Error(err)
		}

		s.notifyr.Create(ctx, int32(referrerID), "Referral", fmt.Sprintf("You have recieved a referral bonus of %s for referring a new user", referralAmount.String()))
		// TODO: Notify the referrer about the earnings
	} else {
		s.notifyr.Create(ctx, int32(referrerID), "Referral", fmt.Sprintf("You have earned a referral bonus of %s pending KYC verification of the referred user", referralAmount.String()))
		// TODO: Send email notification to the referrer saying they have earned a referral bonus pending KYC approval of the referee
	}

	return referral, nil
}

func (s *Service) GetUserReferrals(ctx context.Context, userID int64) ([]Referral, error) {
	return s.repo.GetUserReferrals(ctx, userID)
}

func (s *Service) GetAllReferrals(ctx context.Context) ([]db.UserReferral, error) {
	return s.repo.GetAllReferrals(ctx)
}

func (s *Service) GetReferralEarnings(ctx context.Context, userID int64) (*db.ReferralEarning, error) {
	return s.repo.GetReferralEarnings(ctx, userID)
}

func (s *Service) RequestWithdrawal(ctx context.Context, userID int64, amount decimal.Decimal) (*db.WithdrawalRequest, error) {
	wr, err := s.repo.CreateWithdrawalRequest(ctx, userID, amount)
	if err != nil {
		if errors.Is(err, ErrInsufficientBalance) {
			return nil, fmt.Errorf("insufficient balance: %w", err)
		}
		if errors.Is(err, ErrWithdrawalThreshold) {
			return nil, fmt.Errorf("amount below withdrawal threshold: %w", err)
		}
		s.logger.Error(fmt.Errorf("requestwithdrawal service error: %v", err))
		return nil, err
	}
	// Todo: email Notify user and admin
	return wr, nil
}

// UpdateWithdrawalRequest Admin feature
func (s *Service) UpdateWithdrawalRequest(ctx context.Context, withdrawalRequestID int64, status WithdrawalRequestStatus) (db.WithdrawalRequest, error) {
	wr, err := s.repo.GetWithdrawalRequest(ctx, withdrawalRequestID)
	if err != nil {
		return db.WithdrawalRequest{}, err
	}
	if wr.Status == string(WithdrawalStatusApproved) {
		return db.WithdrawalRequest{}, errors.New("withdrawal request is already approved")
	}
	switch status {
	case WithdrawalStatusApproved:
		return s.repo.UpdateWithdrawalRequest(ctx, withdrawalRequestID, status)
	case WithdrawalStatusRejected:
		return s.repo.UpdateWithdrawalRequest(ctx, withdrawalRequestID, status)
	default:
		return db.WithdrawalRequest{}, errors.New("invalid withdrawal status")
	}
}

func (s *Service) Withdraw(ctx context.Context, requestID int64, userID int32, amount decimal.Decimal) error {
	wallet, err := s.repo.queries.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: int64(userID),
		Currency:   "NGN",
	})

	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}

	walletBalance, err := decimal.NewFromString(wallet.Balance.String)
	if err != nil {
		return fmt.Errorf("failed to convert wallet balance to decimal: %w", err)
	}

	err = s.repo.queries.ExecTx(ctx, func(q *db.Queries) error {
		// Get the withdrawal request
		wr, err := q.GetWithdrawalRequest(ctx, requestID)
		if err != nil {
			return err
		}

		if wr.Status != string(WithdrawalStatusApproved) {
			return errors.New("withdrawal request is not approved")
		}

		// Get referral earnings
		earnings, err := q.GetReferralEarnings(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get referral earnings: %w", err)
		}

		availableBalance, err := decimal.NewFromString(earnings.AvailableBalance)
		if err != nil {
			return err
		}

		if availableBalance.LessThan(amount) {
			return errors.New("insufficient available balance")
		}

		// Deduct the amount from available balance
		updateParams := db.UpdateAvailableBalanceAfterWithdrawalParams{
			UserID:           userID,
			AvailableBalance: amount.String(),
		}
		if _, err := q.UpdateAvailableBalanceAfterWithdrawal(ctx, updateParams); err != nil {
			return err
		}

		newWalletBalance := walletBalance.Add(amount)

		// Update wallet balance
		updateWalletParams := db.UpdateWalletBalanceParams{
			Amount: sql.NullString{String: newWalletBalance.String(), Valid: true},
			ID:     wallet.ID,
		}

		_, err = q.UpdateWalletBalance(ctx, updateWalletParams)
		if err != nil {

			return fmt.Errorf("failed to update wallet balance: %w", err)
		}

		return nil
	})

	if err != nil {
		s.logger.Error(fmt.Errorf("withdrawal failed: %v", err))
		return err
	}

	// Notify user (if applicable)
	// ...

	return nil
}

func (s *Service) ListWithdrawalRequests(ctx context.Context, userID int64) ([]db.WithdrawalRequest, error) {
	return s.repo.ListWithdrawalRequests(ctx)
}

func (s *Service) GetWithdrawalRequest(ctx context.Context, requestID int64) (db.WithdrawalRequest, error) {
	return s.repo.GetWithdrawalRequest(ctx, requestID)
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
