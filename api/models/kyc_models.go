package models

import (
	"time"

	"github.com/shopspring/decimal"
)

type KYCVerificationData struct {
	BVN bool `json:"bvn,omitempty"`
	NIN bool `json:"nin,omitempty"`
}

type UserKYCInformationExtended struct {
	KYC           *UserKYCInformation           `json:"kyc"`
	POICollection []ProofOfAddressOutputElement `json:"document_collection"`
}

type UserKYCInformation struct {
	ID                    ID        `json:"id"`
	UserID                ID        `json:"user_id"`
	Tier                  int32     `json:"tier"`
	DailyTransferLimitNgn string    `json:"daily_transfer_limit_ngn"`
	WalletBalanceLimitNgn string    `json:"wallet_balance_limit_ngn"`
	Status                string    `json:"status"`
	VerificationDate      time.Time `json:"verification_date"`
	FullName              string    `json:"full_name"`
	PhoneNumber           string    `json:"phone_number"`
	Email                 string    `json:"email"`
	Bvn                   string    `json:"bvn"`
	Nin                   string    `json:"nin"`
	Gender                string    `json:"gender"`
	SelfieUrl             string    `json:"selfie_url"`
	IDType                string    `json:"id_type"`
	IDNumber              string    `json:"id_number"`
	IDImageUrl            string    `json:"id_image_url"`
	State                 string    `json:"state"`
	Lga                   string    `json:"lga"`
	HouseNumber           string    `json:"house_number"`
	StreetName            string    `json:"street_name"`
	NearestLandmark       string    `json:"nearest_landmark"`
	ProofOfAddressType    string    `json:"proof_of_address_type"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type ProofOfAddressOutputElement struct {
	Filename  string `json:"filename"`
	ProofType string `json:"proof_type"`
	Verified  bool   `json:"verified"`
}

type KYCTransaction struct {
	UserID      string          `json:"user_id"`
	TotalAmount decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	CreatedAt   time.Time       `json:"created_at"`
}
