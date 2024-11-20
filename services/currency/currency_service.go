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

func (c *CurrencyService) GetExchangeRate(ctx context.Context, fromCurrency string, toCurrency string) (decimal.Decimal, error) {
	// TODO: Implement DB Call to fetch current exchange rate
	c.logger.Info(fmt.Sprintf("fetching rate of %v -> %v", fromCurrency, toCurrency))
	exchange, err := c.store.GetLatestExchangeRate(ctx, db.GetLatestExchangeRateParams{
		BaseCurrency:  fromCurrency,
		QuoteCurrency: toCurrency,
	})
	if err != nil {
		return decimal.Zero, err
	}
	decimalValue, err := decimal.NewFromString(exchange.Rate)
	return decimalValue, err
}

func (c *CurrencyService) SetExchangeRate(ctx context.Context, dbTX *sql.Tx, fromCurrency string, toCurrency string, rate int64) error {
	c.logger.Info(fmt.Sprintf("setting exchange rate %v -> %v: %v", fromCurrency, toCurrency, rate))

	// Convert rate to decimal string for storage
	rateDecimal := decimal.NewFromInt(rate)

	params := db.CreateExchangeRateParams{
		BaseCurrency:  fromCurrency,
		QuoteCurrency: toCurrency,
		Rate:          rateDecimal.String(),
		Source:        "manual", // Indicating this was set manually
	}

	if dbTX != nil {
		// Execute within provided transaction
		_, err := c.store.WithTx(dbTX).CreateExchangeRate(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to create exchange rate: %w", err)
		}
	} else {
		// Execute without transaction
		_, err := c.store.CreateExchangeRate(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to create exchange rate: %w", err)
		}
	}

	return nil
}
