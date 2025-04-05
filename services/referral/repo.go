package referral

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/shopspring/decimal"
	"time"
)

type Referral struct {
	ID           int64           `json:"id"`
	ReferrerID   int64           `json:"referrer_id"`
	RefereeID    int64           `json:"referee_id"`
	EarnedAmount decimal.Decimal `json:"earned_amount"`
	CreatedAt    time.Time       `json:"created_at"`
}

type WithdrawRequest struct {
	Amount         decimal.Decimal        `json:"amount" binding:"required,gt=0"`
	PaymentMethod  string                 `json:"payment_method" binding:"required"`
	PaymentDetails map[string]interface{} `json:"payment_details" binding:"required"`
}

type WithdrawalRequestStatus string

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
	CreateWithdrawalRequest(ctx context.Context, userID int64, req WithdrawRequest) (db.WithdrawalRequest, error)

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
	return &Repo{queries: queries}
}

func (r *Repo) CreateReferral(ctx context.Context, referrerID, refereeID int64, amount decimal.Decimal) (*Referral, error) {
	params := db.CreateReferralParams{
		ReferrerID:   int32(referrerID),
		RefereeID:    int32(refereeID),
		EarnedAmount: amount.String(),
	}

	referral, err := r.queries.CreateReferral(ctx, params)
	if err != nil {
		return nil, err
	}

	return &Referral{
		ReferrerID:   int64(referral.ReferrerID),
		RefereeID:    int64(referral.RefereeID),
		EarnedAmount: amount,
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

func (r *Repo) CreateWithdrawalRequest(ctx context.Context, userID int64, req WithdrawRequest) (*db.WithdrawalRequest, error) {
	const withdrawalThreshold = 10000.00 // 10,000 Naira

	// Check if user has enough balance
	earnings, err := r.GetReferralEarnings(ctx, userID)
	if err != nil {
		return nil, err
	}

	aBToDecimal, err := decimal.NewFromString(earnings.AvailableBalance)
	if err != nil {
		return nil, err
	}

	if aBToDecimal.LessThan(req.Amount) {
		return nil, ErrInsufficientBalance
	}

	wtToDecimal := decimal.NewFromFloat(withdrawalThreshold)
	if req.Amount.LessThan(wtToDecimal) {
		return nil, ErrWithdrawalThreshold
	}

	paymentDetails, err := json.Marshal(req.PaymentDetails)
	if err != nil {
		return nil, err
	}

	params := db.CreateWithdrawalRequestParams{
		UserID:         int32(userID),
		Amount:         req.Amount.String(),
		PaymentMethod:  req.PaymentMethod,
		PaymentDetails: paymentDetails,
	}

	// Start transaction
	tx, err := r.queries.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	q := r.queries.WithTx(tx)

	// Create withdrawal request
	wr, err := q.CreateWithdrawalRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	// Update available balance
	_, err = q.UpdateAvailableBalanceAfterWithdrawal(ctx, db.UpdateAvailableBalanceAfterWithdrawalParams{
		UserID:           int32(userID),
		AvailableBalance: req.Amount.String(),
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	pd, err := json.Marshal(req.PaymentDetails)
	if err != nil {
		return nil, err
	}

	return &db.WithdrawalRequest{
		ID:             wr.ID,
		UserID:         wr.UserID,
		Amount:         wr.Amount,
		Status:         wr.Status,
		PaymentMethod:  wr.PaymentMethod,
		PaymentDetails: pd,
		AdminNotes:     wr.AdminNotes,
		CreatedAt:      wr.CreatedAt,
		UpdatedAt:      wr.UpdatedAt,
	}, nil

}

func (r *Repo) UpdateWithdrawalRequestStatus(ctx context.Context, requestID int64, status WithdrawalRequestStatus, notes string) (*db.WithdrawalRequest, error) {
	// Convert string to sql.NullString
	adminNotes := sql.NullString{
		String: notes,
		Valid:  notes != "", // Valid is true if the string is non-empty
	}
	params := db.UpdateWithdrawalRequestParams{
		ID:         requestID,
		Status:     string(status),
		AdminNotes: adminNotes,
	}

	wr, err := r.queries.UpdateWithdrawalRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	var paymentDetails map[string]interface{}
	if err := json.Unmarshal(wr.PaymentDetails, &paymentDetails); err != nil {
		return nil, err
	}

	pdls, err := json.Marshal(paymentDetails)
	if err != nil {
		return nil, err
	}

	return &db.WithdrawalRequest{
		ID:             wr.ID,
		UserID:         wr.UserID,
		Amount:         wr.Amount,
		Status:         wr.Status,
		PaymentMethod:  wr.PaymentMethod,
		PaymentDetails: pdls,
		AdminNotes:     wr.AdminNotes,
		CreatedAt:      wr.CreatedAt,
		UpdatedAt:      wr.UpdatedAt,
	}, nil

}
