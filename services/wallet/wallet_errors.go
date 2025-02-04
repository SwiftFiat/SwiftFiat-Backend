package wallet

import "fmt"

var (
	ErrWalletNotFound    = fmt.Errorf("wallet not found")
	ErrWalletNotPossible = fmt.Errorf("could not create wallet")
	ErrInsufficientFunds = fmt.Errorf("insufficient funds")
	ErrNotYours          = fmt.Errorf("you don't own the source wallet, this will be reported")
)

type WalletError struct {
	ErrorObj error
	WalletID string
	Other    []error
}

func (w *WalletError) Error() string {
	return w.ErrorObj.Error()
}

func (w *WalletError) ErrorOut() string {
	return fmt.Sprintf("%v: %v", w.ErrorObj.Error(), w.WalletID)
}

func NewWalletError(err error, wallID string, e ...error) *WalletError {
	return &WalletError{
		ErrorObj: err,
		WalletID: wallID,
		Other:    e,
	}
}
