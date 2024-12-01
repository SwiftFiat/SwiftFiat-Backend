--! Rollback precision changes on crypto tables
-- Start transaction
BEGIN;

-- Revert crypto_addresses table to original precision
ALTER TABLE "crypto_addresses" 
ALTER COLUMN "balance" TYPE DECIMAL(20, 20) USING "balance"::DECIMAL(20, 20);

-- Revert crypto_transaction_trail table to original precision
ALTER TABLE "crypto_transaction_trail" 
ALTER COLUMN "amount" TYPE DECIMAL(20, 20) USING "amount"::DECIMAL(20, 20);

-- End Transaction
COMMIT;