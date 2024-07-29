package models

import (
	"time"

	_ "github.com/go-playground/validator/v10"
)

type UserLoginParams struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterUserParams struct {
	FirstName   string `json:"first_name" binding:"required"`
	LastName    string `json:"last_name" binding:"required"`
	Email       string `json:"email" binding:"required"`
	PhoneNumber string `json:"phone_number" binding:"required"`
	Password    string `json:"password" binding:"required"`
}

type RegisterAdminParams struct {
	FirstName   string `json:"first_name" binding:"required"`
	LastName    string `json:"last_name" binding:"required"`
	Email       string `json:"email" binding:"required"`
	PhoneNumber string `json:"phone_number" binding:"required"`
	AdminKey    string `json:"admin_key" binding:"required" validate:"oneof=919d89nd3uinnwe2K 283d9h29nc3uncsa"`
}

type UserResponse struct {
	ID          int64     `json:"id"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	Email       string    `json:"email"`
	PhoneNumber string    `json:"phone_number"`
	Verified    bool      `json:"verified"`
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

type VerifyOTPPasswordParams struct {
	Email string `json:"email" binding:"required"`
	OTP   string `json:"otp" binding:"required"`
}

type ChangePasswordParams struct {
	Password string `json:"password" binding:"required"`
}
