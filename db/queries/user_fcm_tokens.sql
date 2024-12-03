-- name: UpsertFCMToken :one
INSERT INTO user_fcm_tokens (user_id, fcm_token, device_uuid) 
VALUES ($1, $2, $3)
ON CONFLICT (fcm_token) DO UPDATE 
SET 
    fcm_token = EXCLUDED.fcm_token,
    device_uuid = EXCLUDED.device_uuid,
    updated_at = NOW()
RETURNING *;

-- name: GetFCMTokens :many
SELECT * FROM user_fcm_tokens WHERE user_id = $1;

-- name: UpdateFCMToken :one
UPDATE user_fcm_tokens SET fcm_token = $1 WHERE user_id = $2 AND device_uuid = $3 RETURNING *;

-- name: RemoveToken :exec
DELETE FROM user_fcm_tokens WHERE user_id = $1 AND fcm_token = $2;

