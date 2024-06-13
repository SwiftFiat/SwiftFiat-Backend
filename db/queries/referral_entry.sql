-- name: AddNewReferralEntry :one
INSERT INTO referral_entries (
    referral_key,
    referrer,
    referee,
    referral_detail
) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetReferralEntryByID :one
SELECT * FROM referral_entries WHERE id = $1;

-- name: GetReferralEntryByReferrer :one
SELECT * FROM referral_entries WHERE referrer = $1;

-- name: GetReferralEntryByReferee :one
SELECT * FROM referral_entries WHERE referee = $1;

-- name: DeleteReferralEntry :exec
DELETE FROM referral_entries WHERE id = $1;

-- name: DeleteAllEntries :exec
DELETE FROM referral_entries;