package models

import (
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/fiat"
	"github.com/google/uuid"
)

type WalletCollectionResponse []WalletResponse

type WalletResponse struct {
	ID         uuid.UUID `json:"id"`
	CustomerID ID        `json:"customer_id"`
	Type       string    `json:"type"`
	Currency   string    `json:"currency"`
	Balance    string    `json:"balance"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TagResolveResponse struct {
	CustomerName string    `json:"customer_name"`
	WalletID     uuid.UUID `json:"wallet_id"`
	CustomerID   ID        `json:"customer_id"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
}

func ToTagResolveResponse(tagRes db.GetWalletByTagRow) *TagResolveResponse {
	fullName := tagRes.FirstName.String + " " + tagRes.LastName.String
	return &TagResolveResponse{
		CustomerName: fullName,
		WalletID:     tagRes.ID,
		CustomerID:   ID(tagRes.ID_2),
		Currency:     tagRes.Currency,
		Status:       tagRes.Status,
	}
}

type BeneficiaryResponseCollection []BeneficiaryResponse

type BeneficiaryResponse struct {
	ID              uuid.UUID `json:"id"`
	UserID          ID        `json:"user_id"`
	BankCode        string    `json:"bank_code"`
	AccountNumber   string    `json:"account_number"`
	BeneficiaryName string    `json:"beneficiary_name"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func ToBeneficiaryResponseCollection(benRows []db.Beneficiary) BeneficiaryResponseCollection {
	var responses BeneficiaryResponseCollection
	for _, benRow := range benRows {
		responses = append(responses, *ToBeneficiaryResponse(benRow))
	}
	return responses
}

func ToBeneficiaryResponse(benRow db.Beneficiary) *BeneficiaryResponse {
	return &BeneficiaryResponse{
		ID:              benRow.ID,
		UserID:          ID(benRow.UserID.Int64),
		BankCode:        benRow.BankCode,
		AccountNumber:   benRow.AccountNumber,
		BeneficiaryName: benRow.BeneficiaryName,
		CreatedAt:       benRow.CreatedAt,
		UpdatedAt:       benRow.UpdatedAt,
	}
}

type FiatTransferResponse struct {
	ID                  uuid.UUID     `json:"id"`
	Type                string        `json:"type"`
	Amount              string        `json:"amount"`
	Currency            string        `json:"currency"`
	FromAccountID       uuid.NullUUID `json:"from_account_id"`
	Status              string        `json:"status"`
	Description         string        `json:"description"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	CurrencyFlow        string        `json:"currency_flow"`
	FiatAccountName     string        `json:"fiat_account_name"`
	FiatAccountBankCode string        `json:"fiat_account_bank_code"`
	FiatAccountNumber   string        `json:"fiat_account_number"`
	SavedBeneficiary    bool          `json:"saved_beneficiary"`
}

func ToFiatTransferResponse(fiatRes *fiat.TransferResponse, transInf *db.Transaction, benSaved bool) *FiatTransferResponse {
	return &FiatTransferResponse{
		ID:                  transInf.ID,
		Type:                transInf.Type,
		Amount:              transInf.Amount,
		Currency:            transInf.Currency,
		FromAccountID:       transInf.FromAccountID,
		Status:              fiatRes.Status,
		Description:         transInf.Description.String,
		CreatedAt:           transInf.CreatedAt,
		UpdatedAt:           transInf.UpdatedAt,
		CurrencyFlow:        transInf.CurrencyFlow.String,
		FiatAccountName:     transInf.FiatAccountName.String,
		FiatAccountBankCode: transInf.FiatAccountBankCode.String,
		FiatAccountNumber:   transInf.FiatAccountNumber.String,
		SavedBeneficiary:    benSaved,
	}
}
