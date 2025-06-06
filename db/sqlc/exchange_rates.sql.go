// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: exchange_rates.sql

package db

import (
	"context"
	"time"
)

const createExchangeRate = `-- name: CreateExchangeRate :one
INSERT INTO exchange_rates (
    base_currency,
    quote_currency, 
    rate,
    effective_time,
    source
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING id, base_currency, quote_currency, rate, effective_time, source, created_at
`

type CreateExchangeRateParams struct {
	BaseCurrency  string    `json:"base_currency"`
	QuoteCurrency string    `json:"quote_currency"`
	Rate          string    `json:"rate"`
	EffectiveTime time.Time `json:"effective_time"`
	Source        string    `json:"source"`
}

func (q *Queries) CreateExchangeRate(ctx context.Context, arg CreateExchangeRateParams) (ExchangeRate, error) {
	row := q.db.QueryRowContext(ctx, createExchangeRate,
		arg.BaseCurrency,
		arg.QuoteCurrency,
		arg.Rate,
		arg.EffectiveTime,
		arg.Source,
	)
	var i ExchangeRate
	err := row.Scan(
		&i.ID,
		&i.BaseCurrency,
		&i.QuoteCurrency,
		&i.Rate,
		&i.EffectiveTime,
		&i.Source,
		&i.CreatedAt,
	)
	return i, err
}

const deleteOldExchangeRates = `-- name: DeleteOldExchangeRates :exec
DELETE FROM exchange_rates 
WHERE effective_time < $1
`

func (q *Queries) DeleteOldExchangeRates(ctx context.Context, effectiveTime time.Time) error {
	_, err := q.db.ExecContext(ctx, deleteOldExchangeRates, effectiveTime)
	return err
}

const getExchangeRateAtTime = `-- name: GetExchangeRateAtTime :one
SELECT id, base_currency, quote_currency, rate, effective_time, source, created_at FROM exchange_rates
WHERE base_currency = $1 
AND quote_currency = $2
AND effective_time <= $3
ORDER BY effective_time DESC
LIMIT 1
`

type GetExchangeRateAtTimeParams struct {
	BaseCurrency  string    `json:"base_currency"`
	QuoteCurrency string    `json:"quote_currency"`
	EffectiveTime time.Time `json:"effective_time"`
}

func (q *Queries) GetExchangeRateAtTime(ctx context.Context, arg GetExchangeRateAtTimeParams) (ExchangeRate, error) {
	row := q.db.QueryRowContext(ctx, getExchangeRateAtTime, arg.BaseCurrency, arg.QuoteCurrency, arg.EffectiveTime)
	var i ExchangeRate
	err := row.Scan(
		&i.ID,
		&i.BaseCurrency,
		&i.QuoteCurrency,
		&i.Rate,
		&i.EffectiveTime,
		&i.Source,
		&i.CreatedAt,
	)
	return i, err
}

const getLatestExchangeRate = `-- name: GetLatestExchangeRate :one
SELECT id, base_currency, quote_currency, rate, effective_time, source, created_at FROM exchange_rates
WHERE base_currency = $1 
AND quote_currency = $2
ORDER BY effective_time DESC
LIMIT 1
`

type GetLatestExchangeRateParams struct {
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
}

func (q *Queries) GetLatestExchangeRate(ctx context.Context, arg GetLatestExchangeRateParams) (ExchangeRate, error) {
	row := q.db.QueryRowContext(ctx, getLatestExchangeRate, arg.BaseCurrency, arg.QuoteCurrency)
	var i ExchangeRate
	err := row.Scan(
		&i.ID,
		&i.BaseCurrency,
		&i.QuoteCurrency,
		&i.Rate,
		&i.EffectiveTime,
		&i.Source,
		&i.CreatedAt,
	)
	return i, err
}

const listExchangeRates = `-- name: ListExchangeRates :many
SELECT id, base_currency, quote_currency, rate, effective_time, source, created_at FROM exchange_rates
WHERE base_currency = $1 
AND quote_currency = $2
AND effective_time BETWEEN $3 AND $4
ORDER BY effective_time DESC
`

type ListExchangeRatesParams struct {
	BaseCurrency    string    `json:"base_currency"`
	QuoteCurrency   string    `json:"quote_currency"`
	EffectiveTime   time.Time `json:"effective_time"`
	EffectiveTime_2 time.Time `json:"effective_time_2"`
}

func (q *Queries) ListExchangeRates(ctx context.Context, arg ListExchangeRatesParams) ([]ExchangeRate, error) {
	rows, err := q.db.QueryContext(ctx, listExchangeRates,
		arg.BaseCurrency,
		arg.QuoteCurrency,
		arg.EffectiveTime,
		arg.EffectiveTime_2,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ExchangeRate{}
	for rows.Next() {
		var i ExchangeRate
		if err := rows.Scan(
			&i.ID,
			&i.BaseCurrency,
			&i.QuoteCurrency,
			&i.Rate,
			&i.EffectiveTime,
			&i.Source,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listLatestExchangeRates = `-- name: ListLatestExchangeRates :many
SELECT DISTINCT ON (base_currency, quote_currency)
  base_currency,
  quote_currency,
  rate,
  effective_time,
  source
FROM exchange_rates
WHERE effective_time > '1900-01-01'
ORDER BY base_currency, quote_currency, effective_time DESC
`

type ListLatestExchangeRatesRow struct {
	BaseCurrency  string    `json:"base_currency"`
	QuoteCurrency string    `json:"quote_currency"`
	Rate          string    `json:"rate"`
	EffectiveTime time.Time `json:"effective_time"`
	Source        string    `json:"source"`
}

func (q *Queries) ListLatestExchangeRates(ctx context.Context) ([]ListLatestExchangeRatesRow, error) {
	rows, err := q.db.QueryContext(ctx, listLatestExchangeRates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListLatestExchangeRatesRow{}
	for rows.Next() {
		var i ListLatestExchangeRatesRow
		if err := rows.Scan(
			&i.BaseCurrency,
			&i.QuoteCurrency,
			&i.Rate,
			&i.EffectiveTime,
			&i.Source,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
