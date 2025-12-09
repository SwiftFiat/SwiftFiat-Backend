package exchangerate

import (
	"time"

	"github.com/shopspring/decimal"
)

type ExchangeRateError struct {
	Code    string
	Message string
	Err     error
}

func (e *ExchangeRateError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

var (
	ErrInvalidCurrencyPair = &ExchangeRateError{Code: "INVALID_CURRENCY_PAIR", Message: "Invalid currency pair"}
	ErrRateNotAvailable    = &ExchangeRateError{Code: "RATE_NOT_AVAILABLE", Message: "Exchange rate not available"}
)

type ExchangeRate struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	Rate     decimal.Decimal `json:"rate"`
	Provider string          `json:"provider"`
	Time     time.Time       `json:"time"`
}

type GetRateRequest struct {
	From string `json:"from" binding:"required,oneof=USD NGN USDT USDC"`
	To   string `json:"to" binding:"required,oneof=USD NGN USDT USDC"`
}

type ExchangeRateResponse struct {
	SourceCurrency string          `json:"source_currency"`
	TargetCurrency string          `json:"target_currency"`
	Rate           decimal.Decimal `json:"rate"`
	Provider       string          `json:"provider"`
	LastUpdated    time.Time       `json:"last_updated"`
}
