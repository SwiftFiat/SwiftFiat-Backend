-- Exchange rates table
CREATE TABLE IF NOT EXISTS "exchange_rates" (
    "id" BIGSERIAL PRIMARY KEY,
    "base_currency" CHAR(3) NOT NULL,
    "quote_currency" CHAR(3) NOT NULL,
    "rate" DECIMAL(20,8) NOT NULL,
    "effective_time" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "source" VARCHAR(50) NOT NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX "idx_currency_pair" ON exchange_rates(base_currency, quote_currency);
CREATE INDEX "idx_effective_time" ON exchange_rates(effective_time);
CREATE INDEX "idx_lookup" ON exchange_rates(base_currency, quote_currency, effective_time);