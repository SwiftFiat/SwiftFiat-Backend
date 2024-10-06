package models

import db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"

func ToUserKYCInformation(kyc *db.Kyc) *UserKYCInformation {
	return &UserKYCInformation{
		ID:                    ID(kyc.ID),
		UserID:                ID(kyc.UserID),
		Tier:                  kyc.Tier,
		DailyTransferLimitNgn: kyc.DailyTransferLimitNgn.String,
		WalletBalanceLimitNgn: kyc.WalletBalanceLimitNgn.String,
		Status:                kyc.Status,
		VerificationDate:      kyc.VerificationDate.Time,
		FullName:              kyc.FullName.String,
		PhoneNumber:           kyc.PhoneNumber.String,
		Email:                 kyc.Email.String,
		Bvn:                   kyc.Bvn.String,
		Nin:                   kyc.Nin.String,
		Gender:                kyc.Gender.String,
		SelfieUrl:             kyc.SelfieUrl.String,
		IDType:                kyc.IDType.String,
		IDNumber:              kyc.IDNumber.String,
		IDImageUrl:            kyc.IDImageUrl.String,
		State:                 kyc.State.String,
		Lga:                   kyc.Lga.String,
		HouseNumber:           kyc.HouseNumber.String,
		StreetName:            kyc.StreetName.String,
		NearestLandmark:       kyc.NearestLandmark.String,
		ProofOfAddressType:    kyc.ProofOfAddressType.String,
		CreatedAt:             kyc.CreatedAt,
		UpdatedAt:             kyc.UpdatedAt,
	}
}
