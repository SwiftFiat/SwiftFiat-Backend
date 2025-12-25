package referral

import (
	"context"
	"database/sql"
	"errors"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/shopspring/decimal"
)

type Referral struct {
	ID           int64           `json:"id"`
	ReferrerID   int64           `json:"referrer_id"`
	RefereeID    int64           `json:"referee_id"`
	EarnedAmount decimal.Decimal `json:"earned_amount"`
	CreatedAt    time.Time       `json:"created_at"`
	Status       ReferralStatus  `json:"status"`
}

type WithdrawalRequestStatus string
type ReferralStatus string

const (
	WithdrawalStatusPending   WithdrawalRequestStatus = "pending"
	WithdrawalStatusApproved  WithdrawalRequestStatus = "approved"
	WithdrawalStatusRejected  WithdrawalRequestStatus = "rejected"
	WithdrawalStatusCompleted WithdrawalRequestStatus = "completed"
	ReferralStatusPending     ReferralStatus          = "pending"
	ReferralStatusActive      ReferralStatus          = "active"
)

var (
	ErrInsufficientBalance = errors.New("insufficient available balance")
	ErrWithdrawalThreshold = errors.New("amount below withdrawal threshold")
)

type Repository interface {

	/*
		Parameters:
		referrerID: ID of the user who made the referral
		refereeID: ID of the new user who was referred
		amount: The referral bonus amount earned

		Behavior:
		Creates a record linking the two users
		Tracks the referral bonus amount
		Returns the created referral record
		Used when a new user signs up via a referral link

		Example: When UserA refers UserB with ₦500 bonus
	*/
	// CreateReferral Records a new referral relationship between users.
	CreateReferral(ctx context.Context, referrerID, refereeID int64, amount decimal.Decimal) (*Referral, error)

	/*
		Parameters:
		userID: ID of the referring user to look up

		Behavior:
		Returns a list of all successful referrals the user has made
		Shows who they referred and when
		Used to display referral history in user dashboard

		Example: Get all users referred by UserA
	*/
	// GetUserReferrals Retrieves all referrals made by a specific user
	GetUserReferrals(ctx context.Context, userID int64) ([]Referral, error)

	/*
		Parameters:
		userID: ID of the user to check

		Behavior:
		Returns an object containing:
		total_earned: Lifetime referral earnings
		available_balance: Withdrawable amount
		withdrawn_balance: Already withdrawn amount

		Example: Check UserA has earned ₦1500 total, with ₦1000 available
	*/
	// GetReferralEarnings Gets a user's total referral earnings and balances
	GetReferralEarnings(ctx context.Context, userID int64) (*db.ReferralEarning, error)

	/*
		Parameters:
		userID: ID of the withdrawing user
		req: Contains amount, payment method, and details

		Behavior:
		Creates a withdrawal request in "pending" status
		Deducts amount from available_balance
		Validates sufficient balance exists
		Used when user wants to cash out earnings

		Example: UserA requests ₦1000 withdrawal to their Wallet
	*/
	// CreateWithdrawalRequest Initiates a withdrawal of referral earnings
	CreateWithdrawalRequest(ctx context.Context, userID int64, amount decimal.Decimal) (db.WithdrawalRequest, error)

	/*
		Parameters:
		requestID: ID of the withdrawal request
		status: New status (approved/rejected)
		notes: Admin comments

		Behavior:
		Changes request status and updates timestamps
		If rejected, returns funds to available_balance
		Sends notifications to user
		Used by admins to process withdrawals

		Example: Admin approves UserA's ₦1000 withdrawal
	*/
	// UpdateWithdrawalRequestStatus Updates the status of a withdrawal request (admin function)
	UpdateWithdrawalRequestStatus(ctx context.Context, requestID int64, status WithdrawalRequestStatus, notes string) (*db.WithdrawalRequest, error)

	/*
		Parameters:
		filter: Can specify userID, status, date range etc.

		Behavior:
		Returns paginated list of withdrawals
		Can filter by user or status
		Used for both user history views and admin dashboards

		Example:
		User views their past withdrawals
		Admin sees all pending requests
	*/
	// ListWithdrawalRequests Retrieves withdrawal requests with filtering
	//ListWithdrawalRequests(ctx context.Context, filter models.WithdrawalFilter) ([]db.WithdrawalRequest, error)
}

type Repo struct {
	queries *db.Store
}

func NewReferralRepository(queries *db.Store) *Repo {
	return &Repo{queries}
}

func (r *Repo) CreateReferral(ctx context.Context, referrerID, refereeID int64, amount decimal.Decimal, status string) (*Referral, error) {
	params := db.CreateReferralParams{
		ReferrerID:   int32(referrerID),
		RefereeID:    int32(refereeID),
		EarnedAmount: amount.String(),
		Status:       string(ReferralStatusPending),
	}

	referral, err := r.queries.CreateReferral(ctx, params)
	if err != nil {
		return nil, err
	}

	return &Referral{
		ReferrerID:   int64(referral.ReferrerID),
		RefereeID:    int64(referral.RefereeID),
		EarnedAmount: amount,
		Status:       ReferralStatus(referral.Status),
	}, nil
}

func (r *Repo) GetUserReferrals(ctx context.Context, userID int64) ([]Referral, error) {
	dbReferrals, err := r.queries.GetUserReferrals(ctx, int32(userID))
	if err != nil {
		return nil, err
	}

	referrals := make([]Referral, len(dbReferrals))
	for i, ref := range dbReferrals {
		amount, err := decimal.NewFromString(ref.EarnedAmount)
		if err != nil {
			return nil, err
		}
		referrals[i] = Referral{
			ID:           int64(ref.ID),
			ReferrerID:   int64(ref.ReferrerID),
			RefereeID:    int64(ref.RefereeID),
			EarnedAmount: amount,
			Status:       ReferralStatusPending,
			CreatedAt:    ref.CreatedAt,
		}
	}

	return referrals, nil
}

func (r *Repo) GetReferralEarnings(ctx context.Context, userID int64) (*db.ReferralEarning, error) {
	earnings, err := r.queries.GetReferralEarnings(ctx, int32(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Create earnings record if it doesn't exist
			earnings, err = r.queries.CreateReferralEarnings(ctx, int32(userID))
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &db.ReferralEarning{
		ID:               earnings.ID,
		UserID:           earnings.UserID,
		TotalEarned:      earnings.TotalEarned,
		AvailableBalance: earnings.AvailableBalance,
		WithdrawnBalance: earnings.WithdrawnBalance,
		UpdatedAt:        earnings.UpdatedAt,
	}, nil
}

func (r *Repo) CreateWithdrawalRequest(ctx context.Context, userID int64, amount decimal.Decimal) (*db.WithdrawalRequest, error) {
	// Check if user has enough balance
	earnings, err := r.queries.GetReferralEarnings(ctx, int32(userID))
	if err != nil {
		return nil, err
	}

	availableBalance, err := decimal.NewFromString(earnings.AvailableBalance)
	if err != nil {
		return nil, err
	}

	if availableBalance.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}

	// Check if withdrawal amount is less than minimum withdrawal threshold
	referralConfig, err := r.queries.GetReferralConfig(ctx)
	if err != nil {
		return nil, err
	}
	minimumWithdrawalThreshold, err := decimal.NewFromString(referralConfig.MinimumWithdrawalThreshold)
	if err != nil {
		return nil, err
	}

	if amount.LessThan(minimumWithdrawalThreshold) {
		return nil, ErrWithdrawalThreshold
	}

	// Create withdrawal request
	params := db.CreateWithdrawalRequestParams{
		UserID: int32(userID),
		Amount: amount.String(),
	}

	wr, err := r.queries.CreateWithdrawalRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	return &wr, nil
}

func (r *Repo) UpdateWithdrawalRequest(ctx context.Context, requestID int64, status WithdrawalRequestStatus) (db.WithdrawalRequest, error) {
	return r.queries.UpdateWithdrawalRequest(ctx, db.UpdateWithdrawalRequestParams{
		ID:     requestID,
		Status: string(status),
	})
}

func (r *Repo) ListWithdrawalRequests(ctx context.Context) ([]db.WithdrawalRequest, error) {
	return r.queries.ListWithdrawalRequests(ctx)
}

func (r *Repo) GetWithdrawalRequest(ctx context.Context, requestID int64) (db.WithdrawalRequest, error) {
	return r.queries.GetWithdrawalRequest(ctx, requestID)
}

func (r *Repo) CreateReferralConfig(ctx context.Context, minimumWithdrawalThreshold decimal.Decimal, referralAmount decimal.Decimal) (db.ReferralConfig, error) {
	return r.queries.CreateReferralConfig(ctx, db.CreateReferralConfigParams{
		MinimumWithdrawalThreshold: minimumWithdrawalThreshold.String(),
		ReferralAmount:             referralAmount.String(),
	})
}

func (r *Repo) UpdateReferralConfig(ctx context.Context, id int64, minThreshold, refAmount *decimal.Decimal) (db.ReferralConfig, error) {
	params := db.UpdateReferralConfigParams{ID: id}

	if minThreshold != nil {
		params.MinimumWithdrawalThreshold = sql.NullString{
			String: minThreshold.String(),
			Valid:  true,
		}
	}

	if refAmount != nil {
		params.ReferralAmount = sql.NullString{
			String: refAmount.String(),
			Valid:  true,
		}
	}

	return r.queries.UpdateReferralConfig(ctx, params)
}

func (r *Repo) GetReferralConfig(ctx context.Context) (db.ReferralConfig, error) {
	return r.queries.GetReferralConfig(ctx)
}

func (r *Repo) GetAllReferrals(ctx context.Context) ([]db.UserReferral, error) {
	return r.queries.GetAllReferrals(ctx)
}
