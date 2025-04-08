package referral

import (
	"context"
	"database/sql"
	"errors"
	"github.com/shopspring/decimal"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

// type referral interface {
// 	TrackReferral(ctx context.Context, referrerID, refereeID int64, referralAmount float64) (*Referral, error)
// 	GetUserReferrals(ctx context.Context, userID int64) ([]Referral, error)
// 	GetReferralEarnings(ctx context.Context, userID int64) (*db.ReferralEarning, error)
// 	RequestWithdrawal(ctx context.Context, userID int64, req WithdrawRequest) (*db.WithdrawalRequest, error)
// 	// ProcessWithdrawalRequest ListWithdrawalRequests(ctx context.Context, filter models.WithdrawalFilter) ([]db.WithdrawalRequest, error)
// 	ProcessWithdrawalRequest(ctx context.Context, requestID int64, status WithdrawalRequestStatus, adminID int64, notes string) (*db.WithdrawalRequest, error)
// }

type Service struct {
	repo *Repo
	//notifier  NotificationService // assuming you have a notification service
}

func NewReferralService(repo *Repo) *Service {
	return &Service{
		repo: repo,
		//notifier: notifier,
	}
}

//func (s *Service) TrackReferral(ctx context.Context, referrerID, refereeID int64, referralAmount decimal.Decimal) (*Referral, error) {
//	// Check if this referee was already referred
//	existing, err := s.repo.queries.GetReferralByRefereeID(ctx, int32(refereeID))
//	if err != nil && !errors.Is(err, sql.ErrNoRows) {
//		return nil, err
//	}
//	if existing != (db.UserReferral{}) {
//		return nil, errors.New("user already referred")
//	}
//
//	// Create the referral
//	referral, err := s.repo.CreateReferral(ctx, referrerID, refereeID, referralAmount)
//	if err != nil {
//		return nil, err
//	}
//
//	params := db.UpdateReferralEarningsParams{
//		UserID:      int32(referrerID),
//		TotalEarned: referralAmount.String(),
//	}
//
//	// Update the referrer's earnings
//	_, err = s.repo.queries.UpdateReferralEarnings(ctx, params)
//	if err != nil {
//		return nil, err
//	}
//
//	// Todo: Send notification to the referrer
//	return referral, nil
//}

func (s *Service) TrackReferral(ctx context.Context, referralCode string, refereeID int64, referralAmount decimal.Decimal) (*Referral, error) {
	referralRecord, err := s.repo.queries.GetReferralByReferralKey(ctx, referralCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
		return nil, errors.New("user already referred")
	}

	// Step 3: Prevent self-referral
	if referrerID == refereeID {
		return nil, errors.New("cannot refer yourself")
	}

	// Step 4: Create the referral
	referral, err := s.repo.CreateReferral(ctx, referrerID, refereeID, referralAmount)
	if err != nil {
		return nil, err
	}

	// Step 5: Update the referrer's earnings
	params := db.UpdateReferralEarningsParams{
		UserID:      int32(referrerID),
		TotalEarned: referralAmount.String(),
	}
	_, err = s.repo.queries.UpdateReferralEarnings(ctx, params)
	if err != nil {
		return nil, err
	}

	// TODO: Send notification to the referrer
	return referral, nil
}

func (s *Service) GetUserReferrals(ctx context.Context, userID int64) ([]Referral, error) {
	return s.repo.GetUserReferrals(ctx, userID)
}

func (s *Service) GetReferralEarnings(ctx context.Context, userID int64) (*db.ReferralEarning, error) {
	return s.repo.GetReferralEarnings(ctx, userID)
}

func (s *Service) RequestWithdrawal(ctx context.Context, userID int64, req WithdrawRequest) (*db.WithdrawalRequest, error) {
	wr, err := s.repo.CreateWithdrawalRequest(ctx, userID, req)
	if err != nil {
		return nil, err
	}
	// Todo: Notify user and admin
	return wr, nil
}

func (s *Service) ProcessWithdrawalRequest(ctx context.Context, requestID int64, status WithdrawalRequestStatus, adminID int64, notes string) (*db.WithdrawalRequest, error) {
	wr, err := s.repo.UpdateWithdrawalRequestStatus(ctx, requestID, status, notes)
	if err != nil {
		return nil, err
	}

	// If status is not "completed"
	if status != WithdrawalStatusCompleted {
		return wr, nil
	}

	// Retrieve the withdrawal request details
	withdrawalRequest, err := s.repo.queries.GetWithdrawalRequest(ctx, requestID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("withdrawal request not found")
		}
		return nil, err
	}

	// Retrieve the user's wallet
	wallet, err := s.repo.queries.GetWalletByCustomerID(ctx, int64(withdrawalRequest.UserID))
	if err != nil {
		return nil, errors.New("user wallet not found")
	}

	// Ensure the user has at least one wallet
	if len(wallet) == 0 {
		return nil, errors.New("user does not have a wallet")
	}

	// Update the wallet balance
	_, err = s.repo.queries.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		Amount: sql.NullString{
			String: withdrawalRequest.Amount,
			Valid:  true,
		},
		ID: wallet[0].ID, // Assuming the first wallet is the default wallet
	})
	if err != nil {
		return nil, err
	}

	// Todo: Notify user

	return wr, nil
}
