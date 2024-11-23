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
WITH LatestTimes AS (
  SELECT 
    base_currency,
    quote_currency,
    MAX(effective_time) as latest_time
  FROM exchange_rates
  GROUP BY base_currency, quote_currency
)
SELECT 
  er.base_currency,
  er.quote_currency,
  er.rate,
  er.effective_time,
  er.source
FROM exchange_rates er
INNER JOIN LatestTimes lt 
  ON er.base_currency = lt.base_currency 
  AND er.quote_currency = lt.quote_currency
  AND er.effective_time = lt.latest_time
ORDER BY er.base_currency, er.quote_currency;

-- name: DeleteOldExchangeRates :exec
DELETE FROM exchange_rates 
WHERE effective_time < $1;
