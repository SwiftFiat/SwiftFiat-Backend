package utils

// This defines the changes to be sent to the frontEnd
// The revision number is a constant sent throughout
// the lifecycle of the API

// Please update this using SEMVER
var REVISION string = "1.0.0"

type SwiftError struct {
	Code    string
	Message string
	Err     error
}

func (e *SwiftError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

var (
	ErrBankAccountNotFound     = &SwiftError{Code: "BANK_ACCOUNT_NOT_FOUND", Message: "Bank account not found"}
	ErrBankAccountNotVerified  = &SwiftError{Code: "BANK_ACCOUNT_NOT_VERIFIED", Message: "Bank account not verified"}
)
