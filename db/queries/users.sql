-- name: CreateUser :one
INSERT INTO users (
    first_name,
    last_name,
    email,
    phone_number,
    hashed_password,
    role
) VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone_number = $1;

-- name: ListUsers :many
SELECT * FROM users WHERE deleted_at = NULL ORDER BY id
LIMIT $1 OFFSET $2;

-- name: ListAdmins :many
SELECT * FROM users WHERE role=$1 ORDER BY id
LIMIT $2 OFFSET $3;

-- name: UpdateUserPassword :one
UPDATE users SET hashed_password = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserPasscodee :one
UPDATE users SET hashed_passcode = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserPin :one
UPDATE users SET hashed_pin = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserPhone :one
UPDATE users SET phone_number = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserFirstName :one
UPDATE users SET first_name = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserLastName :one
UPDATE users SET last_name = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserVerification :one
UPDATE users SET verified = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserWalletStatus :one
UPDATE users SET has_wallets = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: DeleteAllUsers :exec
DELETE FROM users;