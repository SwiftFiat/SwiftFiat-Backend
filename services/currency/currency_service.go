package currency

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/shopspring/decimal"
)

// SupportedCurrencies is a list of currencies that are supported by the currency service
var SupportedCurrencies = []string{"NGN", "USD"} // , "EUR"}

// denominationMap stores the number of decimal places for each coin - with case-sensitivity
var denominationMap = map[string]int64{
	"BCH":      8,  // 1 BCH = 100,000,000 satoshis
	"bch":      8,  // 1 BCH = 100,000,000 satoshis
	"BNB":      18, // 1 BNB = 10^18 wei
	"bnb":      18, // 1 BNB = 10^18 wei
	"BTC":      8,  // 1 BTC = 100,000,000 satoshis
	"btc":      8,  // 1 BTC = 100,000,000 satoshis
	"DOGE":     8,  // 1 DOGE = 10^8 drops
	"doge":     8,  // 1 DOGE = 10^8 drops
	"DOT":      10, // 1 DOT = 10^10Planck
	"dot":      10, // 1 DOT = 10^10Planck
	"ETH":      18, // 1 ETH = 10^18 wei
	"eth":      18, // 1 ETH = 10^18 wei
	"LINK":     18, // 1 LINK = 10^18 drops
	"link":     18, // 1 LINK = 10^18 drops
	"LTC":      8,  // 1 LTC = 100,000,000 photons
	"ltc":      8,  // 1 LTC = 100,000,000 photons
	"MATIC":    18, // 1 MATIC = 10^18 shannon
	"matic":    18, // 1 MATIC = 10^18 shannon
	"SHIB":     18, // 1 SHIB = 10^18 drops
	"shib":     18, // 1 SHIB = 10^18 drops
	"SOL":      9,  // 1 SOL = 10^9 lamports
	"sol":      9,  // 1 SOL = 10^9 lamports
	"TRON":     6,  // 1 TRON = 10^6 drops
	"tron":     6,  // 1 TRON = 10^6 drops
	"UNI":      18, // 1 UNI = 10^18 wei
	"uni":      18, // 1 UNI = 10^18 wei
	"USDC":     6,  // 1 USDC = 10^6 drops
	"usdc":     6,  // 1 USDC = 10^6 drops
	"USDT":     6,  // 1 USDT = 10^6 drops
	"usdt":     6,  // 1 USDT = 10^6 drops
	"XLM":      7,  // 1 XLM = 10^7 stroops
	"xlm":      7,  // 1 XLM = 10^7 stroops
	"XRP":      6,  // 1 XRP = 10^6 drops
	"xrp":      6,  // 1 XRP = 10^6 drops
	"sol:usdc": 6,  // 1 USDT = 10^6 drops
	"sol:usdt": 6,  // 1 USDT = 10^6 drops
	"SOL:USDC": 6,  // 1 USDT = 10^6 drops
	"SOL:USDT": 6,  // 1 USDT = 10^6 drops
}

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

func (c *CurrencyService) GetCryptoExchangeRate(ctx context.Context, fromCoin string, toCurrency string, prov *providers.ProviderService) (decimal.Decimal, error) {
	// / Get Rate
	if provider, exists := prov.GetProvider(providers.CoinGecko); exists {
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

// SatoshiToCoin converts a satoshi amount to its coin equivalent
func (c *CurrencyService) SatoshiToCoin(satoshiAmount decimal.Decimal, coinType string) (decimal.Decimal, error) {
	denomination, exists := denominationMap[coinType]
	if !exists {
		return decimal.Zero, fmt.Errorf("unsupported coin type")
	}

	divisor := decimal.New(1, int32(denomination))
	return satoshiAmount.Div(divisor), nil
}

// CoinToSatoshi converts a coin amount to its satoshi equivalent
func (c *CurrencyService) CoinToSatoshi(coinAmount decimal.Decimal, coinType string) (decimal.Decimal, error) {
	denomination, exists := denominationMap[coinType]
	if !exists {
		return decimal.Zero, fmt.Errorf("unsupported coin type")
	}

	multiplier := decimal.New(1, int32(denomination))
	return coinAmount.Mul(multiplier), nil
}
