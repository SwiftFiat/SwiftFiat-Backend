-- name: CreateReferral :one
INSERT INTO user_referrals (referrer_id, referee_id, earned_amount, status)
VALUES ($1, $2, $3, $4)
    RETURNING id, referrer_id, referee_id, earned_amount, status, created_at;

-- name: GetReferralByRefereeID :one
SELECT * FROM user_referrals WHERE referee_id = $1;

-- name: GetUserReferrals :many
SELECT * FROM user_referrals WHERE referrer_id = $1
ORDER BY created_at DESC;

-- name: GetAllReferrals :many
SELECT * FROM user_referrals;

-- name: UpdateReferralStatus :exec
UPDATE user_referrals
SET status = $1
WHERE id = $2;


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

-- name: UpdateAvailableBalanceAfterWithdrawal :one
UPDATE referral_earnings
SET
    available_balance = available_balance - $2,
    withdrawn_balance = withdrawn_balance + $2,
    total_earned = available_balance + withdrawn_balance,
    updated_at = NOW()
WHERE user_id = $1
  AND $2 > 0
  AND available_balance >= $2
RETURNING *;


-- name: CreateReferralConfig :one
INSERT INTO referral_configs (referral_amount, referral_percentage_earned_per_conversion)
VALUES ($1, $2)
    RETURNING *;

-- name: UpdateReferralConfig :one
UPDATE referral_configs
SET
    referral_amount = COALESCE(sqlc.narg(referral_amount), referral_amount),
    referral_percentage_earned_per_conversion =
        COALESCE(sqlc.narg(referral_percentage_earned_per_conversion), referral_percentage_earned_per_conversion),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;


-- name: GetReferralConfig :one
SELECT * FROM referral_configs;


-- name: DeleteReferralConfig :exec
DELETE FROM referral_configs WHERE id = $1;

-- name: CreateReferralTransaction :one
INSERT INTO referral_transactions (user_id, amount, transaction_id, transaction_type, status, reference)
VALUES ($1, $2, $3, $4, $5, $6)
    RETURNING *;

-- name: UpdateReferralTransactionStatus :exec
UPDATE referral_transactions
SET
    status = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: GetReferralTransaction :one
SELECT * FROM referral_transactions WHERE id = $1;

