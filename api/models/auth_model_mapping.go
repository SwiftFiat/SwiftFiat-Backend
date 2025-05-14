package models

import db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"

func (u UserResponse) ToUserResponse(user *db.User) *UserResponse {
	return &UserResponse{
		ID:        ID(user.ID),
		UserID:    user.ID,
		FirstName: user.FirstName.String,
		LastName:  user.LastName.String,
		Email:     user.Email,
		UserTag:   user.UserTag.String,
		AvatarURL: user.AvatarUrl.String,
		// AvatarBlob:  user.AvatarBlob,
		PhoneNumber: user.PhoneNumber,
		Verified:    user.Verified,
		HasPin:      user.HashedPin.Valid,
		HasPasscode: user.HashedPasscode.Valid,
		FreshChatID: user.FreshChatID.String,
		IsActive:    user.IsActive,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func ToUserTokenResponse(token *db.UserToken) *UserTokenResponse {
	return &UserTokenResponse{
		UserID:     ID(token.ID),
		PushToken:  token.Token,
		Provider:   token.Provider,
		DeviceUUID: token.DeviceUuid.String,
		CreatedAt:  token.CreatedAt,
		UpdatedAt:  token.UpdatedAt,
	}
}

func (u UserResponse) ToUserResponseList(users []db.User) []*UserResponse {
	userResponses := make([]*UserResponse, len(users))
	for i, user := range users {
		userResponses[i] = &UserResponse{
			ID:          ID(user.ID),
			FirstName:   user.FirstName.String,
			LastName:    user.LastName.String,
			Email:       user.Email,
			UserTag:     user.UserTag.String,
			AvatarURL:   user.AvatarUrl.String,
			PhoneNumber: user.PhoneNumber,
			Verified:    user.Verified,
			HasPin:      user.HashedPin.Valid,
			HasPasscode: user.HashedPasscode.Valid,
			FreshChatID: user.FreshChatID.String,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
		}
	}
	return userResponses
}
