// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: beneficiaries.sql

package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

const createBeneficiary = `-- name: CreateBeneficiary :one
INSERT INTO beneficiaries (user_id, bank_code, account_number, beneficiary_name)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, bank_code, account_number, beneficiary_name, created_at, updated_at
`

type CreateBeneficiaryParams struct {
	UserID          sql.NullInt64 `json:"user_id"`
	BankCode        string        `json:"bank_code"`
	AccountNumber   string        `json:"account_number"`
	BeneficiaryName string        `json:"beneficiary_name"`
}

func (q *Queries) CreateBeneficiary(ctx context.Context, arg CreateBeneficiaryParams) (Beneficiary, error) {
	row := q.db.QueryRowContext(ctx, createBeneficiary,
		arg.UserID,
		arg.BankCode,
		arg.AccountNumber,
		arg.BeneficiaryName,
	)
	var i Beneficiary
	err := row.Scan(
		&i.ID,
		&i.UserID,
		&i.BankCode,
		&i.AccountNumber,
		&i.BeneficiaryName,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const deleteBeneficiary = `-- name: DeleteBeneficiary :exec
DELETE FROM beneficiaries WHERE id = $1
`

func (q *Queries) DeleteBeneficiary(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, deleteBeneficiary, id)
	return err
}

const getAllBeneficiaries = `-- name: GetAllBeneficiaries :many
SELECT id, user_id, bank_code, account_number, beneficiary_name, created_at, updated_at FROM beneficiaries
`

func (q *Queries) GetAllBeneficiaries(ctx context.Context) ([]Beneficiary, error) {
	rows, err := q.db.QueryContext(ctx, getAllBeneficiaries)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Beneficiary{}
	for rows.Next() {
		var i Beneficiary
		if err := rows.Scan(
			&i.ID,
			&i.UserID,
			&i.BankCode,
			&i.AccountNumber,
			&i.BeneficiaryName,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getBeneficiariesByUserID = `-- name: GetBeneficiariesByUserID :many
SELECT id, user_id, bank_code, account_number, beneficiary_name, created_at, updated_at FROM beneficiaries WHERE user_id = $1
`

func (q *Queries) GetBeneficiariesByUserID(ctx context.Context, userID sql.NullInt64) ([]Beneficiary, error) {
	rows, err := q.db.QueryContext(ctx, getBeneficiariesByUserID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Beneficiary{}
	for rows.Next() {
		var i Beneficiary
		if err := rows.Scan(
			&i.ID,
			&i.UserID,
			&i.BankCode,
			&i.AccountNumber,
			&i.BeneficiaryName,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getBeneficiaryByID = `-- name: GetBeneficiaryByID :one
SELECT id, user_id, bank_code, account_number, beneficiary_name, created_at, updated_at FROM beneficiaries WHERE id = $1
`

func (q *Queries) GetBeneficiaryByID(ctx context.Context, id uuid.UUID) (Beneficiary, error) {
	row := q.db.QueryRowContext(ctx, getBeneficiaryByID, id)
	var i Beneficiary
	err := row.Scan(
		&i.ID,
		&i.UserID,
		&i.BankCode,
		&i.AccountNumber,
		&i.BeneficiaryName,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const updateBeneficiary = `-- name: UpdateBeneficiary :one
UPDATE beneficiaries
SET user_id = $1, bank_code = $2, account_number = $3, beneficiary_name = $4
WHERE id = $5
RETURNING id, user_id, bank_code, account_number, beneficiary_name, created_at, updated_at
`

type UpdateBeneficiaryParams struct {
	UserID          sql.NullInt64 `json:"user_id"`
	BankCode        string        `json:"bank_code"`
	AccountNumber   string        `json:"account_number"`
	BeneficiaryName string        `json:"beneficiary_name"`
	ID              uuid.UUID     `json:"id"`
}

func (q *Queries) UpdateBeneficiary(ctx context.Context, arg UpdateBeneficiaryParams) (Beneficiary, error) {
	row := q.db.QueryRowContext(ctx, updateBeneficiary,
		arg.UserID,
		arg.BankCode,
		arg.AccountNumber,
		arg.BeneficiaryName,
		arg.ID,
	)
	var i Beneficiary
	err := row.Scan(
		&i.ID,
		&i.UserID,
		&i.BankCode,
		&i.AccountNumber,
		&i.BeneficiaryName,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}
