package models

import (
	"encoding/json"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
)

func ToUserKYCInformationExtended(kyc *db.GetUserAndKYCWithProofOfAddressRow) *UserKYCInformationExtended {
	return &UserKYCInformationExtended{
		KYC: &UserKYCInformation{
			ID:                 ID(kyc.ID),
			UserID:             kyc.UserID.Int64,
			Status:             kyc.Status,
			VerificationDate:   kyc.VerificationDate.Time,
			FullName:           kyc.FullName.String,
			PhoneNumber:        kyc.PhoneNumber.String,
			Email:              kyc.Email.String,
			Tier:               kyc.Tier,
			Bvn:                kyc.Bvn.String,
			Nin:                kyc.Nin.String,
			Gender:             kyc.Gender.String,
			SelfieUrl:          kyc.SelfieUrl.String,
			IDType:             kyc.IDType.String,
			IDNumber:           kyc.IDNumber.String,
			IDImageUrl:         kyc.IDImageUrl.String,
			State:              kyc.State.String,
			Lga:                kyc.Lga.String,
			HouseNumber:        kyc.HouseNumber.String,
			StreetName:         kyc.StreetName.String,
			NearestLandmark:    kyc.NearestLandmark.String,
			ProofOfAddressType: kyc.ProofOfAddressType.String,
			CreatedAt:          kyc.CreatedAt,
			UpdatedAt:          kyc.UpdatedAt,
		},
		POICollection: ToProofOfAddressCollection(&kyc.ProofOfAddressImages),
	}
}

func ToUserKYCInformation(kyc *db.Kyc) *UserKYCInformation {
	return &UserKYCInformation{
		ID:                 ID(kyc.ID),
		UserID:             int64(kyc.UserID),
		Status:             kyc.Status,
		VerificationDate:   kyc.VerificationDate.Time,
		FullName:           kyc.FullName.String,
		PhoneNumber:        kyc.PhoneNumber.String,
		Email:              kyc.Email.String,
		PostalCode:         kyc.PostalCode.String,
		Bvn:                kyc.Bvn.String,
		Tier:               kyc.Tier,
		Nin:                kyc.Nin.String,
		Gender:             kyc.Gender.String,
		SelfieUrl:          kyc.SelfieUrl.String,
		IDType:             kyc.IDType.String,
		IDNumber:           kyc.IDNumber.String,
		IDImageUrl:         kyc.IDImageUrl.String,
		State:              kyc.State.String,
		Lga:                kyc.Lga.String,
		HouseNumber:        kyc.HouseNumber.String,
		StreetName:         kyc.StreetName.String,
		NearestLandmark:    kyc.NearestLandmark.String,
		ProofOfAddressType: kyc.ProofOfAddressType.String,
		CreatedAt:          kyc.CreatedAt,
		UpdatedAt:          kyc.UpdatedAt,
	}
}

func ToProofOfAddressOutput(poi *db.ProofOfAddressImage) *ProofOfAddressOutputElement {
	return &ProofOfAddressOutputElement{
		Filename:  poi.Filename,
		ProofType: poi.ProofType,
		Verified:  poi.Verified,
	}
}

func ToProofOfAddressCollection(data *json.RawMessage) []ProofOfAddressOutputElement {
	var collection []ProofOfAddressOutputElement
	err := json.Unmarshal(*data, &collection)
	if err != nil {
		return nil
	}
	return collection
}
