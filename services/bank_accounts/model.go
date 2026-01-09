package bankaccounts

import (
	"time"

	"github.com/google/uuid"
)

type BankAccount struct {
	ID                    uuid.UUID  `json:"id"`
	UserID                int64      `json:"user_id"`
	AccountName           string     `json:"account_name"`
	AccountNumber         string     `json:"account_number"`
	BankCode              string     `json:"bank_code"`
	BankName              string     `json:"bank_name"`
	AccountType           *string    `json:"account_type,omitempty"`
	Currency              string     `json:"currency"`
	IsVerified            bool       `json:"is_verified"`
	VerifiedAt            *time.Time `json:"verified_at,omitempty"`
	VerificationMethod    *string    `json:"verification_method,omitempty"`
	VerificationReference *string    `json:"verification_reference,omitempty"`
	IsDefault             bool       `json:"is_default"`
	IsActive              bool       `json:"is_active"`
	Status                string     `json:"status"`
	Label                 *string    `json:"label,omitempty"`
	Description           *string    `json:"description,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	DeletedAt             *time.Time `json:"deleted_at,omitempty"`
}

type CreateBankAccountRequest struct {
	// AccountName   string  `json:"account_name" binding:"required"`
	AccountNumber string  `json:"account_number" binding:"required"`
	BankCode      string  `json:"bank_code" binding:"required"`
	BankName      string  `json:"bank_name" binding:"required"`
	AccountType   *string `json:"account_type"`
	Label         *string `json:"label"`
	Description   *string `json:"description"`
}

type BankAccountResponse struct {
	ID            uuid.UUID `json:"id"`
	AccountName   string    `json:"account_name"`
	AccountNumber string    `json:"account_number"`
	BankCode      string    `json:"bank_code"`
	BankName      string    `json:"bank_name"`
	BankLogo      string    `json:"bank_logo"`
	AccountType   *string   `json:"account_type,omitempty"`
	Currency      string    `json:"currency"`
	IsVerified    bool      `json:"is_verified"`
	IsDefault     bool      `json:"is_default"`
	Status        string    `json:"status"`
	Label         *string   `json:"label,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
