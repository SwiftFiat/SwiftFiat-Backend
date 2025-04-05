package referral

import (
	"context"
	"database/sql"
	"errors"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/shopspring/decimal"
)

type referral interface {
	TrackReferral(ctx context.Context, referrerID, refereeID int64, referralAmount float64) (*Referral, error)
	GetUserReferrals(ctx context.Context, userID int64) ([]Referral, error)
	GetReferralEarnings(ctx context.Context, userID int64) (*db.ReferralEarning, error)
	RequestWithdrawal(ctx context.Context, userID int64, req WithdrawRequest) (*db.WithdrawalRequest, error)
	// ProcessWithdrawalRequest ListWithdrawalRequests(ctx context.Context, filter models.WithdrawalFilter) ([]db.WithdrawalRequest, error)
	ProcessWithdrawalRequest(ctx context.Context, requestID int64, status WithdrawalRequestStatus, adminID int64, notes string) (*db.WithdrawalRequest, error)
}

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

func (s *Service) TrackReferral(ctx context.Context, referrerID, refereeID int64, referralAmount decimal.Decimal) (*Referral, error) {
	// Check if this referee was already referred
	existing, err := s.repo.queries.GetReferralByRefereeID(ctx, int32(refereeID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != (db.UserReferral{}) {
		return nil, errors.New("user already referred")
	}

	// Create the referral
	referral, err := s.repo.CreateReferral(ctx, referrerID, refereeID, referralAmount)
	if err != nil {
		return nil, err
	}

	params := db.UpdateReferralEarningsParams{
		UserID:      int32(referrerID),
		TotalEarned: referralAmount.String(),
	}

	// Update the referrer's earnings
	_, err = s.repo.queries.UpdateReferralEarnings(ctx, params)
	if err != nil {
		return nil, err
	}

	// Todo: Send notification to the referrer
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

	// Todo: Notify user

	return wr, nil
}
