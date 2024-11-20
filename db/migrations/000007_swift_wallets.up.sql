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
    "created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT "positive_balance" CHECK (balance >= 0),
    CONSTRAINT "unique_customer_currency" UNIQUE (customer_id, currency)
);

-- Transactions table
CREATE TABLE "transactions" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "type" VARCHAR(20) NOT NULL,
    "amount" DECIMAL(19,4) NOT NULL,
    "currency" VARCHAR(3) NOT NULL,
    "from_account_id" UUID REFERENCES swift_wallets(id),
    "to_account_id" UUID REFERENCES swift_wallets(id),
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending',
    "description" TEXT,
    "created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT "positive_amount" CHECK (amount > 0)
);

-- Ledger entries table (for double-entry accounting)
CREATE TABLE "ledger_entries" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "transaction_id" UUID NOT NULL REFERENCES transactions(id),
    "account_id" UUID NOT NULL REFERENCES swift_wallets(id),
    "type" VARCHAR(10) NOT NULL, -- 'debit' or 'credit'
    "amount" DECIMAL(19,4) NOT NULL,
    "balance" DECIMAL(19,4) NOT NULL,
    "created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT "positive_amount" CHECK (amount > 0)
);

-- Indexes
CREATE INDEX "idx_accounts_customer" ON swift_wallets(customer_id);
CREATE INDEX "idx_transactions_accounts" ON transactions(from_account_id, to_account_id);
CREATE INDEX "idx_transactions_status" ON transactions(status);
CREATE INDEX "idx_ledger_transaction" ON ledger_entries(transaction_id);
CREATE INDEX "idx_ledger_account" ON ledger_entries(account_id);

-- Create triggers for updated_at
CREATE TRIGGER "update_accounts_updated_at"
    BEFORE UPDATE ON swift_wallets
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER "update_transactions_updated_at"
    BEFORE UPDATE ON transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();