-- name: CreateNewReferral :one
INSERT INTO referrals (
    user_id,
    referral_key
) VALUES ($1, $2) RETURNING *;

-- name: GetReferralByID :one
SELECT * FROM referrals WHERE id = $1;

-- name: GetReferralByUserID :one
SELECT * FROM referrals WHERE user_id = $1;

-- name: DeleteReferral :exec
DELETE FROM referrals WHERE id = $1;

-- name: DeleteAllReferrals :exec
DELETE FROM referrals;