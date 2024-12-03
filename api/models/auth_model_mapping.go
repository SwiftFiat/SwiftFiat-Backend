package models

import db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"

func (u UserResponse) ToUserResponse(user *db.User) *UserResponse {
	return &UserResponse{
		ID:          ID(user.ID),
		FirstName:   user.FirstName.String,
		LastName:    user.LastName.String,
		Email:       user.Email,
		UserTag:     user.UserTag.String,
		PhoneNumber: user.PhoneNumber,
		Verified:    user.Verified,
		HasPin:      user.HashedPin.Valid,
		HasPasscode: user.HashedPasscode.Valid,
		FreshChatID: user.FreshChatID.String,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func ToUserFCMTokenResponse(user *db.UserFcmToken) *UserFCMTokenResponse {
	return &UserFCMTokenResponse{
		UserID:     ID(user.ID),
		FCMToken:   user.FcmToken,
		DeviceUUID: user.DeviceUuid.String,
		CreatedAt:  user.CreatedAt,
		UpdatedAt:  user.UpdatedAt,
	}
}
