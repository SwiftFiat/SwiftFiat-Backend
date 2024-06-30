-- name: CreateNewOTP :one
INSERT INTO otps (
    user_id,
    otp,
    expired,
    expires_at
) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpsertOTP :one
INSERT INTO otps (
    user_id,
    otp,
    expired,
    expires_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET 
    otp = EXCLUDED.otp,
    expired = EXCLUDED.expired,
    expires_at = EXCLUDED.expires_at
RETURNING *;


-- name: GetOTPByID :one
SELECT *,
       CASE
           WHEN expires_at <= NOW() THEN true
           ELSE expired
       END AS actual_expired
FROM otps
WHERE id = $1;

-- name: GetOTPByUserID :one
SELECT *,
       CASE
           WHEN expires_at <= NOW() THEN true
           ELSE expired
       END AS actual_expired
FROM otps
WHERE user_id = $1;

-- name: UpdateOTP :exec
UPDATE otps
SET 
    otp = $1,
    expired = CASE
                WHEN expires_at <= NOW() THEN true
                ELSE expired  -- Keep the current value of expired if expires_at is in the future
              END,
    updated_at = NOW()
WHERE id = $2;

-- name: DeleteOTP :exec
DELETE FROM otps WHERE id = $1;

-- name: DeleteAllOTPS :exec
DELETE FROM otps;