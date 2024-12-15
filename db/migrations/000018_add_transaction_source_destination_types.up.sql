
BEGIN;
-- Add `transaction_source` column to `transactions` table to track inflows without a `from_account_id` for 'giftcards'
-- Add `transaction_destination` column to `transactions` table to track outflows without a `to_account_id` for 'giftcards'
ALTER TABLE "transactions"
ADD COLUMN "transaction_source" VARCHAR(20),
ADD COLUMN "transaction_destination" VARCHAR(20);

COMMIT;