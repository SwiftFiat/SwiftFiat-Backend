-- name: CreateExchangeRate :one
INSERT INTO exchange_rates (
    base_currency,
    quote_currency, 
    rate,
    effective_time,
    source
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetLatestExchangeRate :one
SELECT * FROM exchange_rates
WHERE base_currency = $1 
AND quote_currency = $2
ORDER BY effective_time DESC
LIMIT 1;

-- name: GetExchangeRateAtTime :one
SELECT * FROM exchange_rates
WHERE base_currency = $1 
AND quote_currency = $2
AND effective_time <= $3
ORDER BY effective_time DESC
LIMIT 1;

-- name: ListExchangeRates :many
SELECT * FROM exchange_rates
WHERE base_currency = $1 
AND quote_currency = $2
AND effective_time BETWEEN $3 AND $4
ORDER BY effective_time DESC;

-- name: ListLatestExchangeRates :many
SELECT DISTINCT ON (base_currency, quote_currency)
  base_currency,
  quote_currency,
  rate,
  effective_time,
  source
FROM exchange_rates
WHERE effective_time > '1900-01-01'
ORDER BY base_currency, quote_currency, effective_time DESC;

-- name: DeleteOldExchangeRates :exec
DELETE FROM exchange_rates 
WHERE effective_time < $1;
