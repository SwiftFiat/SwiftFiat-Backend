-- Add transaction_id column to vault_transactions
ALTER TABLE vault_transactions 
ADD COLUMN transaction_id UUID REFERENCES transactions(id) ON DELETE CASCADE;

-- Create index for better query performance
CREATE INDEX idx_vault_txn_transaction_id ON vault_transactions(transaction_id);

-- Backfill existing data by matching transactions with vault_transactions
-- This matches transactions with transaction_flow containing "savings" 
-- to vault_transactions within 5 seconds
UPDATE vault_transactions vt
SET transaction_id = t.id
FROM transactions t
WHERE t.transaction_flow IN ('wallet -> savings', 'savings -> wallet')
  AND ABS(EXTRACT(EPOCH FROM (t.created_at - vt.created_at))) < 5
  AND vt.transaction_id IS NULL;

-- Make it NOT NULL after backfilling (optional, or keep it nullable for flexibility)
-- ALTER TABLE vault_transactions ALTER COLUMN transaction_id SET NOT NULL;

-- Add unique constraint to ensure one vault_transaction per transaction
CREATE UNIQUE INDEX idx_vault_txn_transaction_id_unique ON vault_transactions(transaction_id) WHERE transaction_id IS NOT NULL;