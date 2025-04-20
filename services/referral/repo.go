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
}

type WithdrawalRequestStatus string

const (
	WithdrawalStatusPending   WithdrawalRequestStatus = "pending"
	WithdrawalStatusApproved  WithdrawalRequestStatus = "approved"
	WithdrawalStatusCompleted WithdrawalRequestStatus = "completed"
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

func (r *Repo) CreateWithdrawalRequest(ctx context.Context, userID int64, amount decimal.Decimal) (*db.WithdrawalRequest, error) {
    const withdrawalThreshold = 10000.00 // 10,000 Naira

    var wr db.WithdrawalRequest // Change to non-pointer type

    err := r.queries.ExecTx(ctx, func(q *db.Queries) error {
        // Check if user has enough balance
        earnings, err := q.GetReferralEarnings(ctx, int32(userID))
        if err != nil {
            return err
        }

        aBToDecimal, err := decimal.NewFromString(earnings.AvailableBalance)
        if err != nil {
            return err
        }

        if aBToDecimal.LessThan(amount) {
            return ErrInsufficientBalance
        }

        wtToDecimal := decimal.NewFromFloat(withdrawalThreshold)
        if amount.LessThan(wtToDecimal) {
            return ErrWithdrawalThreshold
        }

        // Deduct the amount from available balance
        newAvailableBalance := aBToDecimal.Sub(amount)
        updateParams := db.UpdateAvailableBalanceAfterWithdrawalParams{
            UserID:           int32(userID),
            AvailableBalance: newAvailableBalance.String(),
        }
        if _, err := q.UpdateAvailableBalanceAfterWithdrawal(ctx, updateParams); err != nil {
            return err
        }

		// Get user naira wallet
		walletParams := db.GetWalletByCurrencyParams {
			CustomerID:    userID,
			Currency:  "NGN",
		}

		wallet, err := q.GetWalletByCurrency(ctx, walletParams)
		if err != nil {
			return err
		}

        // Create withdrawal request
        params := db.CreateWithdrawalRequestParams{
            UserID: int32(userID),
            Amount: amount.String(),
			WalletID: wallet.ID,
        }
        wr, err = q.CreateWithdrawalRequest(ctx, params) 
        if err != nil {
            return err
        }

        return nil
    })

    if err != nil {
        return nil, err
    }

    return &wr, nil 
}

func (r *Repo) UpdateWithdrawalRequestStatus(ctx context.Context, requestID int64, status WithdrawalRequestStatus) (*db.WithdrawalRequest, error) {
    var wr db.WithdrawalRequest

    err := r.queries.ExecTx(ctx, func(q *db.Queries) error {
        // Fetch the withdrawal request
        requested, err := q.GetWithdrawalRequest(ctx, requestID)
        if err != nil {
            return err
        }

        if requested.Status == string(WithdrawalStatusCompleted) {
            return errors.New("status is already completed")
        }

        // Update the withdrawal request status
        params := db.UpdateWithdrawalRequestParams{
            ID:     requestID,
            Status: string(status),
        }
        wr, err = q.UpdateWithdrawalRequest(ctx, params)
        if err != nil {
            return err
        }

        // If the status is rejected, return the funds to the user's available balance
        if status == WithdrawalStatusPending {
            amount, err := decimal.NewFromString(requested.Amount)
            if err != nil {
                return err
            }

            earnings, err := q.GetReferralEarnings(ctx, requested.UserID)
            if err != nil {
                return err
            }

            availableBalance, err := decimal.NewFromString(earnings.AvailableBalance)
            if err != nil {
                return err
            }

            newAvailableBalance := availableBalance.Add(amount)
            updateParams := db.UpdateAvailableBalanceAfterWithdrawalParams{
                UserID:           requested.UserID,
                AvailableBalance: newAvailableBalance.String(),
            }
            if _, err := q.UpdateAvailableBalanceAfterWithdrawal(ctx, updateParams); err != nil {
                return err
            }
        }

        return nil
    })

    if err != nil {
        return nil, err
    }

    return &wr, nil
}
