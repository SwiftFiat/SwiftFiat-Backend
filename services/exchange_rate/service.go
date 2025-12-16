package exchangerate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/shopspring/decimal"
)

// ExchangeRateService handles real-time exchange rate fetching
type ExchangeRateService struct {
	cryptomusProvider *cryptocurrency.CryptomusProvider
	logger            *logging.Logger
	httpClient        *http.Client
}

func NewExchangeRateService(cryptomusProvider *cryptocurrency.CryptomusProvider, logger *logging.Logger) *ExchangeRateService {
	return &ExchangeRateService{
		cryptomusProvider: cryptomusProvider,
		logger:            logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetExchangeRate fetches real-time exchange rate between two currencies
func (s *ExchangeRateService) GetExchangeRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	s.logger.Info(fmt.Sprintf("Fetching exchange rate: %s -> %s", from, to))

	// Handle direct currency pairs
	rate, err := s.getDirectRate(ctx, from, to)
	if err == nil {
		return rate, nil
	}

	// If direct rate fails, try triangular arbitrage through USD
	s.logger.Info(fmt.Sprintf("Direct rate failed, trying triangular: %v", err))
	rate, err = s.getTriangularRate(ctx, from, to)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Triangular rate failed: %v", err))
		return nil, ErrRateNotAvailable
	}

	return rate, nil
}

// getDirectRate attempts to get rate directly between two currencies
func (s *ExchangeRateService) getDirectRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	// For crypto pairs (USDT, USDC), use Cryptomus
	if s.isCrypto(from) || s.isCrypto(to) {
		return s.getCryptoRate(ctx, from, to)
	}

	// For fiat pairs, use external API
	if from == "NGN" || to == "NGN" {
		return s.getFiatRate(ctx, from, to)
	}

	return nil, fmt.Errorf("no direct rate available")
}

// getTriangularRate calculates rate through USD intermediary
func (s *ExchangeRateService) getTriangularRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	// Get from -> USD
	fromToUSD, err := s.getDirectRate(ctx, from, "USD")
	if err != nil {
		return nil, fmt.Errorf("failed to get %s to USD rate: %w", from, err)
	}

	// Get USD -> to
	usdToTarget, err := s.getDirectRate(ctx, "USD", to)
	if err != nil {
		return nil, fmt.Errorf("failed to get USD to %s rate: %w", to, err)
	}

	// Calculate final rate: from -> to
	finalRate := fromToUSD.Rate.Mul(usdToTarget.Rate)

	return &ExchangeRate{
		From:     from,
		To:       to,
		Rate:     finalRate,
		Provider: fmt.Sprintf("Triangular: %s,%s", fromToUSD.Provider, usdToTarget.Provider),
		Time:     time.Now(),
	}, nil
}

// getCryptoRate gets rate for crypto currencies using Cryptomus
func (s *ExchangeRateService) getCryptoRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	// Cryptomus provides rates to USD
	if to == "USD" {
		rateStr, err := s.cryptomusProvider.GetUSDRate(from)
		if err != nil {
			return nil, fmt.Errorf("cryptomus rate fetch failed: %w", err)
		}

		rate, err := decimal.NewFromString(rateStr)
		if err != nil {
			return nil, fmt.Errorf("invalid rate format: %w", err)
		}

		return &ExchangeRate{
			From:     from,
			To:       "USD",
			Rate:     rate,
			Provider: "Cryptomus",
			Time:     time.Now(),
		}, nil
	}

	// For other conversions, go through USD
	return nil, fmt.Errorf("crypto rate only available to USD")
}

// getFiatRate gets fiat currency rates
func (s *ExchangeRateService) getFiatRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	// For NGN pairs, use a reliable Nigerian forex API
	if from == "NGN" || to == "NGN" {
		return s.getNGNRate(ctx, from, to)
	}

	// For other fiat pairs, use exchangerate-api.com or similar
	return s.getGenericFiatRate(ctx, from, to)
}

// getNGNRate gets NGN exchange rates from reliable Nigerian source
func (s *ExchangeRateService) getNGNRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	// Use exchangerate-api.com for NGN rates (free tier: 1,500 requests/month)
	// Or use: https://api.exchangerate-api.com/v4/latest/USD

	baseCurrency := from
	if to == "USD" || to == "NGN" {
		baseCurrency = "USD"
	}

	url := fmt.Sprintf("https://api.exchangerate-api.com/v4/latest/%s", baseCurrency)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Error(fmt.Sprintf("Rate API error: %d - %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("rate API returned status %d", resp.StatusCode)
	}

	var result struct {
		Base  string             `json:"base"`
		Rates map[string]float64 `json:"rates"`
		Date  string             `json:"date"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Calculate the rate
	var rate decimal.Decimal
	if from == baseCurrency {
		// Direct conversion
		targetRate, exists := result.Rates[to]
		if !exists {
			return nil, fmt.Errorf("rate for %s not found", to)
		}
		rate = decimal.NewFromFloat(targetRate)
	} else {
		// Inverse conversion
		fromRate, exists := result.Rates[from]
		if !exists {
			return nil, fmt.Errorf("rate for %s not found", from)
		}
		toRate, exists := result.Rates[to]
		if !exists {
			return nil, fmt.Errorf("rate for %s not found", to)
		}
		// Calculate: from -> base -> to
		rate = decimal.NewFromFloat(toRate / fromRate)
	}

	return &ExchangeRate{
		From:     from,
		To:       to,
		Rate:     rate,
		Provider: "ExchangeRate-API",
		Time:     time.Now(),
	}, nil
}

// getGenericFiatRate gets generic fiat rates
func (s *ExchangeRateService) getGenericFiatRate(ctx context.Context, from, to string) (*ExchangeRate, error) {
	// For now, delegate to NGN rate function which handles multiple currencies
	return s.getNGNRate(ctx, from, to)
}

// isCrypto checks if currency is a cryptocurrency
func (s *ExchangeRateService) isCrypto(currency string) bool {
	cryptos := map[string]bool{
		"USDT": true,
		"USDC": true,
		"BTC":  true,
		"ETH":  true,
	}
	return cryptos[currency]
}

// CalculateConversionAmount calculates the target amount based on source amount and rate
func (s *ExchangeRateService) CalculateConversionAmount(sourceAmount, rate decimal.Decimal, feePercentage decimal.Decimal) (targetAmount, fees, netAmount decimal.Decimal) {
	// Calculate gross target amount
	targetAmount = sourceAmount.Mul(rate)

	// Calculate fees
	fees = decimal.NewFromInt(0)

	// Calculate net amount
	netAmount = targetAmount.Sub(fees)

	return targetAmount, fees, netAmount
}

// CalculateInverseAmount calculates source amount needed for a target amount
func (s *ExchangeRateService) CalculateInverseAmount(targetAmount, rate decimal.Decimal, feePercentage decimal.Decimal) (sourceAmount, fees, netAmount decimal.Decimal) {
	// Calculate the amount with fees
	// targetAmount = (sourceAmount * rate) - fees
	// targetAmount = (sourceAmount * rate) - (sourceAmount * rate * feePercentage/100)
	// targetAmount = sourceAmount * rate * (1 - feePercentsourceage/100)
	// sourceAmount = targetAmount / (rate * (1 - feePercentage/100))

	multiplier := rate.Mul(decimal.NewFromInt(1).Sub(feePercentage.Div(decimal.NewFromInt(100))))
	sourceAmount = targetAmount.Div(multiplier)

	grossAmount := sourceAmount.Mul(rate)
	// fees = grossAmount.Mul(feePercentage.Div(decimal.NewFromInt(100)))
	fees = decimal.NewFromInt(0)
	netAmount = grossAmount.Sub(fees)

	return sourceAmount, fees, netAmount
}

// GetFeePercentage returns the fee percentage for a conversion
func (s *ExchangeRateService) GetFeePercentage(sourceCurrency, targetCurrency string) decimal.Decimal {
	// Define fee structure
	// Crypto to Fiat: 2%
	// Fiat to Crypto: 1.5%
	// Fiat to Fiat: 1%
	// Crypto to Crypto: 0.5%

	sourceIsCrypto := s.isCrypto(sourceCurrency)
	targetIsCrypto := s.isCrypto(targetCurrency)

	if sourceIsCrypto && !targetIsCrypto {
		return decimal.NewFromFloat(2.0) // 2%
	} else if !sourceIsCrypto && targetIsCrypto {
		return decimal.NewFromFloat(1.5) // 1.5%
	} else if !sourceIsCrypto && !targetIsCrypto {
		return decimal.NewFromFloat(1.0) // 1%
	} else {
		return decimal.NewFromFloat(0.5) // 0.5%
	}
}

// ValidateCurrencyPair checks if a currency pair is supported
func (s *ExchangeRateService) ValidateCurrencyPair(from, to string) error {
	supportedCurrencies := map[string]bool{
		"USD":  true,
		"NGN":  true,
		"USDT": true,
		"USDC": true,
	}

	if !supportedCurrencies[from] {
		return fmt.Errorf("unsupported source currency: %s", from)
	}

	if !supportedCurrencies[to] {
		return fmt.Errorf("unsupported target currency: %s", to)
	}

	if from == to {
		return fmt.Errorf("source and target currencies must be different")
	}

	return nil
}
