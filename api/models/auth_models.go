package models

import (
	"time"

	_ "github.com/go-playground/validator/v10"
)

type UserLoginParams struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type UserPasscodeLoginParams struct {
	Email    string `json:"email" binding:"required"`
	Passcode string `json:"passcode" binding:"required"`
}

type RegisterUserParams struct {
	FirstName    string `json:"first_name" binding:"required"`
	LastName     string `json:"last_name" binding:"required"`
	Email        string `json:"email" binding:"required"`
	PhoneNumber  string `json:"phone_number" binding:"required"`
	ReferralCode string `json:"referral_code"`
	Password     string `json:"password" binding:"required"`
}

type RegisterAdminParams struct {
	FirstName   string `json:"first_name" binding:"required"`
	LastName    string `json:"last_name" binding:"required"`
	Email       string `json:"email" binding:"required"`
	PhoneNumber string `json:"phone_number" binding:"required"`
	AdminKey    string `json:"admin_key" binding:"required" validate:"oneof=919d89nd3uinnwe2K 283d9h29nc3uncsa"`
}

type UserResponse struct {
	ID        ID     `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	// AvatarBlob  []byte    `json:"avatar_blob"`
	UserTag     string    `json:"user_tag"`
	PhoneNumber string    `json:"phone_number"`
	Verified    bool      `json:"verified"`
	HasPin      bool      `json:"has_pin"`
	HasPasscode bool      `json:"has_passcode"`
	FreshChatID string    `json:"fresh_chat_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UserWithToken struct {
	User  *UserResponse `json:"user"`
	Token string        `json:"token"`
}

const (
	ADMIN        = "admin"
	USER         = "user"
	CUSTOMER_REP = "customer_rep"
)

type UserOTPParams struct {
	OTP string `json:"otp"`
}

type ForgotPasswordParams struct {
	Email string `json:"email" binding:"required"`
}

type ForgotPasscodeParams struct {
	Email string `json:"email" binding:"required"`
}

type ResetPasswordParams struct {
	Email           string `json:"email" binding:"required"`
	OTP             string `json:"otp" binding:"required"`
	Password        string `json:"password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

type ChangePasswordParams struct {
	Password string `json:"password" binding:"required"`
}

type CreatePasscodeParams struct {
	Passcode string `json:"passcode" binding:"required"`
}

type ResetPasscodeParams struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
	OTP   string `json:"otp" binding:"required"`
}

type CreatePinParams struct {
	Pin string `json:"pin" binding:"required"`
}

type UserTokenResponse struct {
	UserID     ID        `json:"user_id"`
	PushToken  string    `json:"push_token"`
	Provider   string    `json:"provider"`
	DeviceUUID string    `json:"device_uuid"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type UpdateTransactionPinParams struct {
	Pin    string `json:"pin" binding:"required"`
	OldPin string `json:"old_pin" binding:"required"`
}
