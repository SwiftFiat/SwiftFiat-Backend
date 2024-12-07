package currency

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/cryptocurrency"
	"github.com/shopspring/decimal"
)

var SupportedCurrencies = []string{"NGN", "USD", "EUR"}

type CurrencyService struct {
	store  *db.Store
	logger *logging.Logger
}

func IsCurrencyValid(request string) bool {
	for _, c := range SupportedCurrencies {
		if request == c {
			return true
		}
	}

	return false
}

func IsCurrencyInvalid(request string) bool {
	return !IsCurrencyValid(request)
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

func (c *CurrencyService) GetCryptoExchangeRate(ctx context.Context, fromCoin string, toCurrency string, prov *provider.ProviderService) (decimal.Decimal, error) {
	// / Get Rate
	if provider, exists := prov.GetProvider(provider.CoinGecko); exists {
		rateProvider, ok := provider.(*cryptocurrency.CoinGeckoProvider)
		if !ok {
			c.logger.Error("failed to connect to provicer")
			return decimal.Zero, fmt.Errorf("failed to instantiate provider")
		}

		coinRate, err := rateProvider.GetUSDRate(&fromCoin)
		if err != nil {
			c.logger.Error(err)
			return decimal.Zero, fmt.Errorf("failed to connect to Crypto Rates Provider Error: %s", err)
		}

		c.logger.Info(fmt.Sprintf("USD Rate for coin: %v | rate: %+v", fromCoin, coinRate))

		exchange_rate, err := decimal.NewFromString(coinRate)
		if err != nil {
			c.logger.Error(err)
		}

		return exchange_rate, nil
	}

	return decimal.Zero, fmt.Errorf("no such rates provider exists")
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
		EffectiveTime: time.Now(),
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
