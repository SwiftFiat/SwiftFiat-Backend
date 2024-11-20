package currency

import (
	"context"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
)

type CurrencyService struct {
	logger *logging.Logger
}

func (c *CurrencyService) GetExchangeRate(ctx context.Context, fromCurrency string, toCurrency string) (int64, error) {
	// TODO: Implement DB Call to fetch current exchange rate
	c.logger.Info(fmt.Sprintf("fetching rate of %v -> %v", fromCurrency, toCurrency))
	return 1600, nil
}
