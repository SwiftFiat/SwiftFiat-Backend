package user_service

import "fmt"

var (
	ErrUserNotFound         = fmt.Errorf("user not found")
	ErrUserAlreadyExists    = fmt.Errorf("user already exists")
	ErrUserTagAlreadyExists = fmt.Errorf("tag already exists")
)

type UserError struct {
	ErrorObj error
	UserID   string
	Other    []error
}

func (u *UserError) Error() string {
	return u.ErrorObj.Error()
}

func (u *UserError) ErrorOut() string {
	return fmt.Sprintf("%v: %v", u.ErrorObj.Error(), u.UserID)
}

func NewUserError(err error, userID string, e ...error) *UserError {
	return &UserError{
		ErrorObj: err,
		UserID:   userID,
		Other:    e,
	}
}
