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

type CreateTransferRecipientRequest struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	AccountNumber string `json:"account_number"`
	BankCode      string `json:"bank_code"`
	Currency      string `json:"currency"`
}

type TransferRequest struct {
	Source    string `json:"source"`
	Reason    string `json:"reason"`
	Amount    int64  `json:"amount"`
	Recipient string `json:"recipient"`
}

type TransferResponse struct {
	Transfersessionid []interface{} `json:"transfersessionid"`
	Transfertrials    []interface{} `json:"transfertrials"`
	Domain            string        `json:"domain"`
	Amount            int64         `json:"amount"`
	Currency          string        `json:"currency"`
	Reference         string        `json:"reference"`
	Source            string        `json:"source"`
	SourceDetails     interface{}   `json:"source_details"`
	Reason            string        `json:"reason"`
	Status            string        `json:"status"`
	Failures          interface{}   `json:"failures"`
	TransferCode      string        `json:"transfer_code"`
	TitanCode         interface{}   `json:"titan_code"`
	TransferredAt     interface{}   `json:"transferred_at"`
	ID                int64         `json:"id"`
	Integration       int64         `json:"integration"`
	Request           int64         `json:"request"`
	Recipient         int64         `json:"recipient"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}
