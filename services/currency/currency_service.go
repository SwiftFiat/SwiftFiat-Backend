package currency

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/shopspring/decimal"
)

type CurrencyService struct {
	store  *db.Store
	logger *logging.Logger
}

func NewCurrencyService(store *db.Store, logger *logging.Logger) *CurrencyService {
	return &CurrencyService{
		store:  store,
		logger: logger,
	}
}

func (c *CurrencyService) GetExchangeRate(ctx context.Context, fromCurrency string, toCurrency string) (decimal.Decimal, error) {
	// TODO: Implement DB Call to fetch current exchange rate
	c.logger.Info(fmt.Sprintf("fetching rate of %v to %v", fromCurrency, toCurrency))
	exchange, err := c.store.GetLatestExchangeRate(ctx, db.GetLatestExchangeRateParams{
		BaseCurrency:  fromCurrency,
		QuoteCurrency: toCurrency,
	})
	if err == sql.ErrNoRows {
		return decimal.Zero, ErrNoExchangeRate
	} else if err != nil {
		return decimal.Zero, err
	}
	decimalValue, err := decimal.NewFromString(exchange.Rate)
	return decimalValue, err
}

func (c *CurrencyService) GetAllExchangeRates(ctx context.Context) (interface{}, error) {
	c.logger.Info("fetching all rates")
	exchangeRates, err := c.store.ListLatestExchangeRates(ctx)
	if err == sql.ErrNoRows {
		return nil, ErrNoExchangeRate
	} else if err != nil {
		return nil, err
	}
	return exchangeRates, err
}

func (c *CurrencyService) SetExchangeRate(ctx context.Context, dbTX *sql.Tx, fromCurrency string, toCurrency string, rate string) (*db.ExchangeRate, error) {
	c.logger.Info(fmt.Sprintf("setting exchange rate %v -> %v: %v", fromCurrency, toCurrency, rate))

	// Convert rate to decimal string for storage
	rateDecimal, err := decimal.NewFromString(rate)
	if err != nil {
		return nil, fmt.Errorf("could not determine rate from input")
	}

	params := db.CreateExchangeRateParams{
		BaseCurrency:  fromCurrency,
		QuoteCurrency: toCurrency,
		Rate:          rateDecimal.String(),
		Source:        "manual", // Indicating this was set manually
	}

	var exchObj db.ExchangeRate

	if dbTX != nil {
		// Execute within provided transaction
		exchObj, err = c.store.WithTx(dbTX).CreateExchangeRate(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create exchange rate: %w", err)
		}
	} else {
		// Execute without transaction
		exchObj, err = c.store.CreateExchangeRate(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to create exchange rate: %w", err)
		}
	}

	return &exchObj, nil
}
