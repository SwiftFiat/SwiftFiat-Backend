package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

// MarshalMetadata converts map[string]any → pqtype.NullRawMessage
func MarshalMetadata(meta map[string]any) pqtype.NullRawMessage {
	if meta == nil {
		return pqtype.NullRawMessage{Valid: false}
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return pqtype.NullRawMessage{Valid: false}
	}

	return pqtype.NullRawMessage{
		RawMessage: raw,
		Valid:      true,
	}
}

// UnmarshalMetadata converts pqtype.NullRawMessage → map[string]any
func UnmarshalMetadata(raw pqtype.NullRawMessage) map[string]any {
	if !raw.Valid || len(raw.RawMessage) == 0 {
		return nil
	}

	var meta map[string]any
	if err := json.Unmarshal(raw.RawMessage, &meta); err != nil {
		return nil
	}

	return meta
}

func NewTxRef(prefix string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, uuid.New().String()[:8], time.Now().Unix())
}

func ToDecimal(value string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, err
	}
	return d, nil
}

func ToFloat(value string) (float64, error) {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return 0, err
	}
	return d.InexactFloat64(), nil
}

// DollarStringToCentsString converts e.g. "3.50" → "350"
func DollarStringToCentsString(amount string) (string, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return "", err
	}

	cents := d.Mul(decimal.NewFromInt(100))
	return cents.String(), nil
}

// CentsStringToDollarString converts e.g. "350" → "3.50"
func CentsStringToDollarString(cents string) (string, error) {
	d, err := decimal.NewFromString(cents)
	if err != nil {
		return "", err
	}

	dollars := d.Div(decimal.NewFromInt(100))
	// Ensure fixed 2-decimal format
	return dollars.StringFixed(2), nil
}

type ExchangeRateResponse struct {
	Base  string                     `json:"base"`
	Rates map[string]decimal.Decimal `json:"rates"`
}

func GetNGNUSDRate(ctx context.Context) (decimal.Decimal, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.exchangerate-api.com/v4/latest/NGN",
		nil,
	)
	if err != nil {
		return decimal.Zero, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("exchange rate api returned %d", resp.StatusCode)
	}

	var data ExchangeRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return decimal.Zero, err
	}

	rate, ok := data.Rates["USD"]
	if !ok || rate.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("USD rate missing or invalid")
	}

	return rate, nil
}

type cryptomusRateItem struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Course string `json:"course"`
}

type cryptomusRateResponse struct {
	State  int                 `json:"state"`
	Result []cryptomusRateItem `json:"result"`
}

func GetCryptoToUSDRate(ctx context.Context, currency string) (decimal.Decimal, error) {
	url := fmt.Sprintf("https://api.cryptomus.com/v1/exchange-rate/%s/list", currency)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return decimal.Zero, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("cryptomus rate API returned status %d", resp.StatusCode)
	}

	var data cryptomusRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return decimal.Zero, err
	}

	for _, item := range data.Result {
		if item.To == "USD" {
			rate, err := decimal.NewFromString(item.Course)
			if err != nil {
				return decimal.Zero, fmt.Errorf("invalid rate format: %w", err)
			}
			if rate.LessThanOrEqual(decimal.Zero) {
				return decimal.Zero, fmt.Errorf("invalid non-positive rate for %s", currency)
			}
			return rate, nil
		}
	}

	return decimal.Zero, fmt.Errorf("USD rate not found for %s", currency)
}

func ConvertToUSD(
	ctx context.Context,
	amount decimal.Decimal,
	currency string,
) (decimal.Decimal, error) {

	switch currency {

	// USD & stablecoins → 1:1
	case "USD", "USDT", "USDC":
		return amount, nil

	// NGN → USD via your existing API
	case "NGN":
		rate, err := GetNGNUSDRate(ctx)
		if err != nil {
			return decimal.Zero, err
		}
		return amount.Mul(rate), nil

	// Crypto via Cryptomus
	default:
		// Try to fetch crypto → USD rate from Cryptomus
		rate, err := GetCryptoToUSDRate(ctx, currency)
		if err != nil {
			return decimal.Zero, fmt.Errorf("crypto to USD rate error: %w", err)
		}
		return amount.Mul(rate), nil
	}
}

// watRequestID generates a WAT-formatted request ID for VTPass idempotency.
// FIX [B4]: was time.Now().UTC().Add(time.Hour * 1) — fragile manual offset.
func WatRequestID() string {
	loc, err := time.LoadLocation("Africa/Lagos")
	if err != nil {
		loc = time.FixedZone("WAT", 3600) // safe fallback on stripped tzdata
	}
	return time.Now().In(loc).Format("20060102150405")
}

func SplitName(fullName string) (string, string) {
	parts := strings.Fields(fullName)

	if len(parts) == 0 {
		return "", ""
	}

	firstName := parts[0]
	lastName := ""

	if len(parts) > 1 {
		lastName = parts[len(parts)-1]
	}

	return firstName, lastName
}
