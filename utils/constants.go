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
	ErrQRCodeNotFound          = &SwiftError{Code: "QR_CODE_NOT_FOUND", Message: "QR code not found"}
	ErrQRCodeExpired           = &SwiftError{Code: "QR_CODE_EXPIRED", Message: "QR code has expired"}
	ErrQRCodeInactive          = &SwiftError{Code: "QR_CODE_INACTIVE", Message: "QR code is not active"}
	ErrQRCodeUsageLimitReached = &SwiftError{Code: "USAGE_LIMIT_REACHED", Message: "QR code usage limit reached"}
	ErrBankAccountNotFound     = &SwiftError{Code: "BANK_ACCOUNT_NOT_FOUND", Message: "Bank account not found"}
	ErrBankAccountNotVerified  = &SwiftError{Code: "BANK_ACCOUNT_NOT_VERIFIED", Message: "Bank account not verified"}
	ErrTransactionNotFound     = &SwiftError{Code: "TRANSACTION_NOT_FOUND", Message: "Transaction not found"}
	ErrDuplicateWebhook        = &SwiftError{Code: "DUPLICATE_WEBHOOK", Message: "Webhook already processed"}
	ErrPayoutFailed            = &SwiftError{Code: "PAYOUT_FAILED", Message: "Bank payout failed"}
)
