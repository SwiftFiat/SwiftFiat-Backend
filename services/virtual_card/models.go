package virtualcard

import (
	"errors"
)

var (
	ErrInsufficientBalance   = errors.New("insufficient wallet balance")
	ErrCardNotFound          = errors.New("virtual card not found")
	ErrCardFrozen            = errors.New("card is frozen")
	ErrUnauthorized          = errors.New("unauthorized access to card")
	ErrInvalidAmount         = errors.New("invalid amount")
	ErrExceedsLimit          = errors.New("amount exceeds limit")
	ErrCardAlreadyTerminated = errors.New("card already terminated")
)
