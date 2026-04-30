-- name: UpsertToken :one
INSERT INTO user_tokens (user_id, token, provider, device_uuid) 
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, device_uuid, provider) DO UPDATE
SET token      = EXCLUDED.token,
    updated_at = NOW()
RETURNING *;

-- name: ClaimTokenForUser :exec
-- Detach this token from any other (user, device) it might be bound to.
DELETE FROM user_tokens
WHERE token = $1
  AND NOT (user_id = $2 AND device_uuid = $3 AND provider = $4);

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

-- name: RemoveTokenByDevice :exec
DELETE FROM user_tokens
WHERE user_id = $1 AND device_uuid = $2;

-- name: RemoveTokenGlobal :exec
-- Used by handleTokenError — the token might not belong to the user we
-- thought we were sending to (Bug #1's legacy). Wipe it by token alone.
DELETE FROM user_tokens WHERE token = $1;