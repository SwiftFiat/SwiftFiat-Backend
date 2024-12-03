-- name: UpsertToken :one
INSERT INTO user_tokens (user_id, token, provider, device_uuid) 
VALUES ($1, $2, $3, $4)
ON CONFLICT (token) DO UPDATE 
SET 
    token = EXCLUDED.token,
    provider = EXCLUDED.provider,
    device_uuid = EXCLUDED.device_uuid,
    updated_at = NOW()
RETURNING *;

-- name: GetTokens :many
SELECT * FROM user_tokens WHERE user_id = $1;

-- name: UpdateToken :one
UPDATE user_tokens SET token = $1 WHERE user_id = $2 AND device_uuid = $3 RETURNING *;

-- name: RemoveToken :exec
DELETE FROM user_tokens WHERE user_id = $1 AND token = $2;

