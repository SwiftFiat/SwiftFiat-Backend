BEGIN;
-- Drop `transaction_info` from `transactions` table
ALTER TABLE "transactions"
DROP COLUMN "fiat_account_number",
DROP COLUMN "fiat_account_bank_code",
DROP COLUMN "fiat_account_name";

COMMIT;
