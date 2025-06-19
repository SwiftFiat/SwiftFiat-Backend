-- Base transactions table with common fields only
CREATE TABLE IF NOT EXISTS "transactions" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "type" VARCHAR(20) NOT NULL, -- e.g. swap | transfer | crypto | giftcard | withdrawal | service (airtime | data | etc)
    "description" TEXT, -- e.g. User entered transaction description
    "transaction_flow" VARCHAR(50), -- e.g. tbtc -> USD
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending', -- e.g success | pending | failed | unknown
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
 
-- Swift Wallet Transactions metadata for transfer or swap
CREATE TABLE IF NOT EXISTS "swap_transfer_metadata" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "currency" VARCHAR(3) NOT NULL,
    "transaction_id" UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    "transfer_type" VARCHAR(10) NOT NULL, -- 'transfer' or 'swap'
    "description" TEXT,
    "source_wallet" UUID REFERENCES swift_wallets(id), -- if user is owner of source waller, then sent amount is outflow
    "destination_wallet" UUID REFERENCES swift_wallets(id), -- if user is owner of destination wallet, then received amount is inflow
    "user_tag" VARCHAR(50),
    "rate" DECIMAL(19,4), -- determines amount to be received by other party
    "fees" DECIMAL(19,4), -- determines fees charged to transaction initiator (i.e. sender)
    "received_amount" DECIMAL(19,4), -- amount received by other party
    "sent_amount" DECIMAL(19,4), -- amount sent by current party
    CONSTRAINT "unique_swift_wallet_transaction" UNIQUE (transaction_id)
);

-- Crypto transaction metadata
CREATE TABLE IF NOT EXISTS "crypto_transaction_metadata" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "destination_wallet" UUID REFERENCES swift_wallets(id),
    "transaction_id" UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    "coin" VARCHAR(10) NOT NULL, -- specifies coin received (e.g. btc, tbtc)
    "source_hash" VARCHAR(64) UNIQUE, -- specifies webhook hash
    "rate" DECIMAL(40,20), -- determines amount to be received by destination wallet
    "fees" DECIMAL(19,4), -- determines platform charges to recipient
    "received_amount" DECIMAL(40,20), -- amount entered into destination wallet
    "sent_amount" DECIMAL(19,4), -- coin value sent by user on other platform
    "service_provider" VARCHAR(100) NOT NULL, -- e.g., BitGo
    "service_transaction_id" VARCHAR(100), -- e.g to track the crypto inflow at service provider level
    CONSTRAINT "unique_transaction_crypto" UNIQUE (transaction_id)
);

-- Giftcard transaction metadata
CREATE TABLE IF NOT EXISTS "giftcard_transaction_metadata" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "source_wallet" UUID REFERENCES swift_wallets(id), -- wallet from which funds for purchase were pulled
    "transaction_id" UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    "rate" DECIMAL(19,4), -- determines amount to be removed from the wallet
    "received_amount" DECIMAL(19,4), -- amount received by giftcard supplier
    "sent_amount" DECIMAL(19,4), -- amount pulled from the wallet in the wallet's currency
    "fees" DECIMAL(19,4),
    "service_provider" VARCHAR(100) NOT NULL, -- e.g., Reloadly
    "service_transaction_id" VARCHAR(100), -- e.g to track the giftcard purchase at service provider level
    CONSTRAINT "unique_transaction_giftcard" UNIQUE (transaction_id)
);

-- Fiat withdrawal metadata
CREATE TABLE IF NOT EXISTS "fiat_withdrawal_metadata" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "source_wallet" UUID REFERENCES swift_wallets(id), -- wallet from which withdrawal is initiated
    "rate" DECIMAL(19,4), -- rate from source to destination FIAT account
    "received_amount" DECIMAL(19,4), -- amount received into FIAT account
    "sent_amount" DECIMAL(19,4), -- amount pulled from the wallet in the wallet's currency
    "fees" DECIMAL(19,4), -- determines platform charges to sender's wallet
    "transaction_id" UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    "account_name" VARCHAR(100), -- FIAT account name
    "bank_code" VARCHAR(20), -- FIAT account's bank code
    "account_number" VARCHAR(20), -- FIAT account's NUBAN
    "service_provider" VARCHAR(100), -- e.g., Paystack
    "service_transaction_id" VARCHAR(100), -- e.g to track the withdrawal at service provider level
    CONSTRAINT "unique_transaction_fiat" UNIQUE (transaction_id)
);

-- Services metadata for services like TV subscription, airtime-data purchase, etc.
CREATE TABLE IF NOT EXISTS "services_metadata" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "source_wallet" UUID REFERENCES swift_wallets(id),
    "rate" DECIMAL(19,4), -- rate from source wallet to destination provider's service
    "received_amount" DECIMAL(19,4), -- amount received by services provider
    "sent_amount" DECIMAL(19,4), -- amount pulled from the wallet in the wallet's currency
    "fees" DECIMAL(19,4), -- determines platform charges to sender's wallet
    "transaction_id" UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    "service_type" VARCHAR(50) NOT NULL, -- e.g., 'tv_subscription', 'airtime_purchase', etc.
    "service_provider" VARCHAR(100), -- e.g., VTPass
    "service_id" VARCHAR(50), -- e.g., subscription ID, phone number, etc.
    "service_status" VARCHAR(20) NOT NULL DEFAULT 'pending', -- e.g., 'active', 'inactive', 'pending', etc.
    "service_transaction_id" VARCHAR(100), -- e.g to track the service purchase at service provider level
    CONSTRAINT "unique_transaction_service" UNIQUE (transaction_id)
);

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_transactions_type ON transactions(type);
CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(status);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON transactions(created_at);
-- Create metadata indexes
CREATE INDEX IF NOT EXISTS idx_swap_transfer_transaction_id ON swap_transfer_metadata(transaction_id);
CREATE INDEX IF NOT EXISTS idx_crypto_transaction_id ON crypto_transaction_metadata(transaction_id);
CREATE INDEX IF NOT EXISTS idx_giftcard_transaction_id ON giftcard_transaction_metadata(transaction_id);
CREATE INDEX IF NOT EXISTS idx_fiat_transaction_id ON fiat_withdrawal_metadata(transaction_id);
CREATE INDEX IF NOT EXISTS idx_services_transaction_id ON services_metadata(transaction_id);
-- Other metadata table indexes
CREATE INDEX IF NOT EXISTS idx_swap_transfer_source_wallet ON swap_transfer_metadata(source_wallet);
CREATE INDEX IF NOT EXISTS idx_crypto_source_hash ON crypto_transaction_metadata(source_hash);
CREATE INDEX IF NOT EXISTS idx_giftcard_source_wallet ON giftcard_transaction_metadata(source_wallet);
CREATE INDEX IF NOT EXISTS idx_fiat_withdrawal_source_wallet ON fiat_withdrawal_metadata(source_wallet);
CREATE INDEX IF NOT EXISTS idx_services_metadata_source_wallet ON services_metadata(source_wallet);


-- Auto Update Functions
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER set_updated_at
BEFORE UPDATE ON transactions
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- Add table comments for better documentation
COMMENT ON TABLE transactions IS 'Core transaction table storing all financial transactions';
COMMENT ON TABLE swap_transfer_metadata IS 'Metadata for wallet-to-wallet transfers and swaps';
COMMENT ON TABLE crypto_transaction_metadata IS 'Metadata for cryptocurrency transactions';
COMMENT ON TABLE giftcard_transaction_metadata IS 'Metadata for giftcard purchases';
COMMENT ON TABLE fiat_withdrawal_metadata IS 'Metadata for fiat currency withdrawals';
COMMENT ON TABLE services_metadata IS 'Metadata for service purchases like airtime and TV subscriptions';