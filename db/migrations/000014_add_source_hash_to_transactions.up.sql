-- Start transaction
BEGIN;

-- Add `source_hash` column to `transactions` table to track inflows without a `from_account_id`
ALTER TABLE transactions
ADD COLUMN source_hash VARCHAR(64) DEFAULT NULL,
ADD COLUMN coin VARCHAR(10) DEFAULT NULL;

-- Add a unique constraint on `source_hash` to prevent duplicate inflow tracking
ALTER TABLE transactions
ADD CONSTRAINT unique_source_hash UNIQUE (source_hash);

-- End Transaction
COMMIT;