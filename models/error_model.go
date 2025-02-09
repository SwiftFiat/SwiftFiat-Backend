package models

import "fmt"

// InternalError is an error that is returned by the internal server
// It is used to identify the error and return a specific error message
// For internal use only
type InternalError int

const (
	InternalErrorUnknown InternalError = iota
	InternalErrorInvalidRequest
	InternalErrorInvalidResponse
	InternalErrorInvalidState
)

func (e InternalError) Error() error {
	switch e {
	case InternalErrorUnknown:
		return fmt.Errorf("unknown internal error")
	case InternalErrorInvalidRequest:
		return fmt.Errorf("invalid request")
	case InternalErrorInvalidResponse:
		return fmt.Errorf("invalid response")
	case InternalErrorInvalidState:
		return fmt.Errorf("invalid state")
	default:
		return fmt.Errorf("unknown internal error")
	}
}

func (e InternalError) String() string {
	return fmt.Sprintf("internal error: %d", e)
}
