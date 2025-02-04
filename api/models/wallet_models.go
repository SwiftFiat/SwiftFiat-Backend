package models

import (
	"encoding/json"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
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

type TransactionResponseCollection []TransactionResponse

type TransactionResponse struct {
	ID                   uuid.UUID   `json:"id"`
	Type                 string      `json:"type"`
	Description          string      `json:"description"`
	TransactionFlow      string      `json:"transaction_flow"`
	Status               string      `json:"status"`
	CreatedAt            time.Time   `json:"created_at"`
	UpdatedAt            time.Time   `json:"updated_at"`
	DeletedFromAccountID uuid.UUID   `json:"deleted_from_account_id,omitempty"`
	DeletedToAccountID   uuid.UUID   `json:"deleted_to_account_id,omitempty"`
	Metadata             interface{} `json:"metadata"`
}

func ToTransactionResponseCollection(transRows []db.GetTransactionsForWalletRow) TransactionResponseCollection {
	var responses TransactionResponseCollection
	for _, transRow := range transRows {
		responses = append(responses, *ToTransactionResponse(transRow))
	}
	return responses
}

func ToTransactionResponse(transRow db.GetTransactionsForWalletRow) *TransactionResponse {
	metadata, err := InterfaceToMap(transRow.Metadata)
	if err != nil {
		fmt.Println("Error unmarshalling metadata: ", err)
		return nil
	}

	response := &TransactionResponse{
		ID:                   transRow.ID,
		Type:                 transRow.Type,
		Description:          transRow.Description.String,
		TransactionFlow:      transRow.TransactionFlow.String,
		Status:               transRow.Status,
		CreatedAt:            transRow.CreatedAt,
		UpdatedAt:            transRow.UpdatedAt,
		DeletedFromAccountID: transRow.DeletedFromAccountID.UUID,
		DeletedToAccountID:   transRow.DeletedToAccountID.UUID,
		Metadata:             metadata,
	}

	return response
}

func InterfaceToMap(metadata interface{}) (map[string]interface{}, error) {
	var metadataMap map[string]interface{}

	switch v := metadata.(type) {
	case []byte: // Handle if metadata is []byte
		err := json.Unmarshal(v, &metadataMap)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	case string: // Handle if metadata is string
		err := json.Unmarshal([]byte(v), &metadataMap)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	case json.RawMessage: // Directly unmarshal if it’s json.RawMessage
		err := json.Unmarshal(v, &metadataMap)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	default:
		return nil, fmt.Errorf("unexpected type for metadata: %T", metadata)
	}

	return metadataMap, nil
}
