package fiat

import "time"

// Recipient represents the main structure for the provided JSON
type Recipient struct {
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"createdAt"`
	Currency      string    `json:"currency"`
	Domain        string    `json:"domain"`
	ID            int       `json:"id"`
	Integration   int       `json:"integration"`
	Name          string    `json:"name"`
	RecipientCode string    `json:"recipient_code"`
	Type          string    `json:"type"`
	UpdatedAt     time.Time `json:"updatedAt"`
	IsDeleted     bool      `json:"is_deleted"`
	Details       Details   `json:"details"`
}

// Details represents the nested details structure
type Details struct {
	AuthorizationCode *string `json:"authorization_code"`
	AccountNumber     string  `json:"account_number"`
	AccountName       string  `json:"account_name"`
	BankCode          string  `json:"bank_code"`
	BankName          string  `json:"bank_name"`
}

// Response on resolving account information
type ResolvedAccount struct {
	AccountNumber string `json:"account_number,omitempty"`
	AccountName   string `json:"account_name,omitempty"`
	BankID        int64  `json:"bank_id,omitempty"`
}

type Response[T any] struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// Response on Fetching Banks
type BankCollection []Bank

type Bank struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	Code             string    `json:"code"`
	Longcode         string    `json:"longcode"`
	Gateway          string    `json:"gateway"`
	PayWithBank      bool      `json:"pay_with_bank"`
	SupportsTransfer bool      `json:"supports_transfer"`
	Active           bool      `json:"active"`
	Country          string    `json:"country"`
	Currency         string    `json:"currency"`
	Type             string    `json:"type"`
	IsDeleted        bool      `json:"is_deleted"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type AccountInfo struct {
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	BankID        int64  `json:"bank_id"`
}
