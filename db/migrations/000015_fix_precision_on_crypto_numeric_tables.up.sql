--! To fix the issue of 20,20 precision issues on the crypto tables
-- Start transaction
BEGIN;

-- Migration to update crypto_transaction_trail table
ALTER TABLE "crypto_transaction_trail" 
ALTER COLUMN "amount" TYPE DECIMAL(30, 10) USING "amount"::DECIMAL(30, 10);

-- Migration to update crypto_addresses table
ALTER TABLE "crypto_addresses" 
ALTER COLUMN "balance" TYPE DECIMAL(30, 10) USING "balance"::DECIMAL(30, 10);

-- End Transaction
COMMIT;