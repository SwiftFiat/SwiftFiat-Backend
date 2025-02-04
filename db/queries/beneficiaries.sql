-- name: GetAllBeneficiaries :many
SELECT * FROM beneficiaries;

-- name: GetBeneficiaryByID :one
SELECT * FROM beneficiaries WHERE id = $1;

-- name: GetBeneficiariesByUserID :many
SELECT * FROM beneficiaries WHERE user_id = $1;

-- name: CreateBeneficiary :one
INSERT INTO beneficiaries (user_id, bank_code, account_number, beneficiary_name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateBeneficiary :one
UPDATE beneficiaries
SET user_id = $1, bank_code = $2, account_number = $3, beneficiary_name = $4
WHERE id = $5
RETURNING *;

-- name: DeleteBeneficiary :exec
DELETE FROM beneficiaries WHERE id = $1;

