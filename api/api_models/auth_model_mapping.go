package api_models

import db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"

func (u UserResponse) ToUserResponse(user *db.User) *UserResponse {
	return &UserResponse{
		ID:          user.ID,
		FirstName:   user.FirstName.String,
		LastName:    user.LastName.String,
		Email:       user.Email,
		PhoneNumber: user.PhoneNumber,
		Verified:    user.Verified,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
