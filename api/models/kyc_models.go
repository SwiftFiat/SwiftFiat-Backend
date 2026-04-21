package models

import (
	"time"

	"github.com/google/uuid"
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
	UserID                uuid.UUID        `json:"user_id"`
	Status                string    `json:"status"`
	VerificationDate      time.Time `json:"verification_date"`
	FullName              string    `json:"full_name"`
	PhoneNumber           string    `json:"phone_number"`
	Tier                  string    `json:"tier"`
	Email                 string    `json:"email"`
	Bvn                   string    `json:"bvn"`
	Nin                   string    `json:"nin"`
	PostalCode            string    `json:"postal_code"`
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
