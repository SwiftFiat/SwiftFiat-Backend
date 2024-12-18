BEGIN;
-- Add `transaction_info` columns to `transactions` table to track withdrawals without a `to_account_id` for 'fiat withdrawals'
ALTER TABLE "transactions"
ADD COLUMN "fiat_account_name" VARCHAR(100),
ADD COLUMN "fiat_account_bank_code" VARCHAR(20),
ADD COLUMN "fiat_account_number" VARCHAR(20);

COMMIT;