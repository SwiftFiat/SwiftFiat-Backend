-- Start transaction
BEGIN;

-- Drop the unique constraint on `source_hash`
ALTER TABLE transactions
DROP CONSTRAINT unique_source_hash;

-- Remove the `source_hash` column
ALTER TABLE transactions
DROP COLUMN source_hash,
DROP COLUMN coin;

-- End Transaction
COMMIT;
