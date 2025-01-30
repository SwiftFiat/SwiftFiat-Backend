-- Schema for the ledger system

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Accounts table
CREATE TABLE "swift_wallets" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "customer_id" BIGSERIAL NOT NULL REFERENCES users(id),
    "type" VARCHAR(50) NOT NULL,
    "currency" VARCHAR(3) NOT NULL,
    "balance" DECIMAL(19,4) DEFAULT 0,
    "status" VARCHAR(20) NOT NULL DEFAULT 'active',
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT "positive_balance" CHECK (balance >= 0),
    CONSTRAINT "unique_customer_currency" UNIQUE (customer_id, currency)
);

-- Indexes
CREATE INDEX "idx_accounts_customer" ON swift_wallets(customer_id);

-- Create triggers for updated_at
CREATE TRIGGER "update_accounts_updated_at"
    BEFORE UPDATE ON swift_wallets
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();