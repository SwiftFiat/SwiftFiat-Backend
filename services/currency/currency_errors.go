package currency

import "fmt"

var (
	ErrNoExchangeRate = fmt.Errorf("could not retrieve exchange rate for")
)

type CurrencyError struct {
	ErrorObj      error
	BaseCurrency  string
	QuoteCurrency string
}

func (c *CurrencyError) Error() string {
	return c.ErrorObj.Error()
}

func (c *CurrencyError) ErrorOut() string {
	return fmt.Sprintf("%v: %v to %v", c.ErrorObj.Error(), c.BaseCurrency, c.QuoteCurrency)
}

func NewCurrencyError(err error, base string, quote string) *CurrencyError {
	return &CurrencyError{
		ErrorObj:      err,
		BaseCurrency:  base,
		QuoteCurrency: quote,
	}
}
