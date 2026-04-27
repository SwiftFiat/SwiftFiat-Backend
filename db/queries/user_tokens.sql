-- name: UpsertToken :one
INSERT INTO user_tokens (user_id, token, provider, device_uuid)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, device_uuid, provider)
DO UPDATE SET token = EXCLUDED.token, updated_at = NOW()
RETURNING *;

-- name: GetTokens :many
SELECT * FROM user_tokens WHERE user_id = $1 ORDER BY updated_at DESC;

-- name: UpdateToken :one
UPDATE user_tokens SET token = $1 WHERE user_id = $2 AND device_uuid = $3 RETURNING *;

-- name: RemoveToken :exec
DELETE FROM user_tokens WHERE user_id = $1 AND token = $2;

-- name: ListActiveUserTokens :many
SELECT ut.* FROM user_tokens ut
JOIN users u ON u.id = ut.user_id
WHERE u.is_active = TRUE AND u.deleted_at IS NULL;

