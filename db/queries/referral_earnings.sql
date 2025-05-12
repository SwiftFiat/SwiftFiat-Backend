-- name: CreateReferral :one
INSERT INTO user_referrals (referrer_id, referee_id, earned_amount, status)
VALUES ($1, $2, $3, $4)
    RETURNING *;

-- name: GetReferralByRefereeID :one
SELECT * FROM user_referrals WHERE referee_id = $1;

-- name: GetUserReferrals :many
SELECT * FROM user_referrals WHERE referrer_id = $1
ORDER BY created_at DESC;

-- name: UpdateReferralStatus :exec
UPDATE user_referrals
SET status = $1
WHERE referee_id = $2;


-- name: GetReferralEarnings :one
SELECT * FROM referral_earnings WHERE user_id = $1;

-- name: CreateReferralEarnings :one
INSERT INTO referral_earnings (user_id)
VALUES ($1)
    RETURNING *;

-- name: UpdateReferralEarnings :one
UPDATE referral_earnings
SET
    total_earned = total_earned + $2,
    available_balance = available_balance + $2,
    updated_at = NOW()
WHERE user_id = $1
    RETURNING *;

-- name: CreateWithdrawalRequest :one
INSERT INTO withdrawal_requests (user_id, amount, wallet_id)
VALUES ($1, $2, $3)
    RETURNING *;

-- name: UpdateWithdrawalRequest :one
UPDATE withdrawal_requests
SET
    status = $2,
    updated_at = NOW()
WHERE id = $1
    RETURNING *;

-- name: GetWithdrawalRequest :one
SELECT * FROM withdrawal_requests WHERE id = $1;

-- name: ListWithdrawalRequests :many
SELECT * FROM withdrawal_requests
ORDER BY created_at DESC;

-- name: ListUserWithdrawalRequests :many
SELECT * FROM withdrawal_requests WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateAvailableBalanceAfterWithdrawal :one
UPDATE referral_earnings
SET
    available_balance = available_balance - $2,
    withdrawn_balance = withdrawn_balance + $2,
    updated_at = NOW()
WHERE user_id = $1 AND available_balance >= $2
    RETURNING *;