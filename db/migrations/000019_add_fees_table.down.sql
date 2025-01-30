--! To disable storage and retrieval of fees for transactions
-- Start transaction
BEGIN;

-- Drop indexes
DROP INDEX IF EXISTS "idx_fees_lookup";
DROP INDEX IF EXISTS "idx_fees_effective_time";
DROP INDEX IF EXISTS "idx_fees_transaction_type";

-- Drop Transaction Fees table
DROP TABLE IF EXISTS "transaction_fees";

-- End Transaction
COMMIT;

