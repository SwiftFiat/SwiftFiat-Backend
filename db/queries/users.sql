-- name: CreateUser :one
INSERT INTO users (
    first_name,
    last_name,
    email,
    phone_number,
    hashed_password,
    role
) VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: UpdateUserTag :one
UPDATE users SET user_tag = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserFreshChatID :one
UPDATE users SET fresh_chat_id = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserNameByUserTag :one
SELECT first_name, last_name FROM users WHERE user_tag = $1;

-- name: CheckUserTag :one
SELECT EXISTS (
    SELECT 1
    FROM users WHERE user_tag = $1
) AS exists;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 and deleted_at is null;

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone_number = $1 and deleted_at is null;

-- name: GetUserAvatar :one
SELECT avatar_url, avatar_blob FROM users WHERE avatar_url = $1;

-- name: ListUsers :many
SELECT * FROM users WHERE deleted_at IS NULL ORDER BY id
LIMIT $1 OFFSET $2;

-- name: ListAdmins :many
SELECT * FROM users WHERE role=$1 ORDER BY id
LIMIT $2 OFFSET $3;

-- name: CountNewUsersToday :one
SELECT COUNT(*)
FROM users
WHERE created_at::date = CURRENT_DATE
  AND deleted_at IS NULL;

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

-- name: UpdateUserNames :one
UPDATE users SET first_name = $1, last_name = $2, updated_at = $3
WHERE id = $4 RETURNING *;

-- name: UpdateUserAvatar :one
UPDATE users SET avatar_url = $1, avatar_blob = $2, updated_at = $3
WHERE id = $4 RETURNING *;

-- name: UpdateUserVerification :one
UPDATE users SET verified = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserKYCVerificationStatus :one
UPDATE users SET is_kyc_verified = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: UpdateUserWalletStatus :one
UPDATE users SET has_wallets = $1, updated_at = $2
WHERE id = $3 RETURNING *;

-- name: DeleteUser :one
UPDATE users
SET phone_number = $1,
    email = $2,
    first_name = $3,
    deleted_at = NOW()
WHERE id = $4
RETURNING *;

-- name: DeleteAllUsers :exec
DELETE FROM users;

-- name: DeactivateUser :one
UPDATE "users"
SET "is_active" = FALSE,
    "updated_at" = NOW()
WHERE "id" = $1
RETURNING *;

-- name: ActivateUser :one
UPDATE "users"
SET "is_active" = TRUE,
    "updated_at" = NOW()
WHERE "id" = $1
RETURNING *;

-- name: AdminUpdateUser :one
UPDATE users
SET
    first_name = COALESCE($2, first_name),
    last_name = COALESCE($3, last_name),
    email = COALESCE($4, email),
    phone_number = COALESCE($5, phone_number),
    role = COALESCE($6, role),
    updated_at = NOW()
WHERE id = $1
RETURNING *;