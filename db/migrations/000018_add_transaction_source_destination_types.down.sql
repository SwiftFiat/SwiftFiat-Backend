BEGIN;
-- Drop `transaction_source` and `transaction_destination` columns from `transactions` table
ALTER TABLE "transactions"
DROP COLUMN "transaction_destination",
DROP COLUMN "transaction_source";

COMMIT;

