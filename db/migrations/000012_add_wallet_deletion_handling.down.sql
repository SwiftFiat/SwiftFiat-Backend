-- Restore original foreign key constraints in correct order
ALTER TABLE ledger_entries
    DROP CONSTRAINT ledger_entries_account_id_fkey,
    ADD CONSTRAINT ledger_entries_account_id_fkey 
        FOREIGN KEY (wallet_id) 
        REFERENCES swift_wallets(id);

ALTER TABLE swift_wallets
    DROP CONSTRAINT swift_wallets_customer_id_fkey,
    ADD CONSTRAINT swift_wallets_customer_id_fkey 
        FOREIGN KEY (customer_id) 
        REFERENCES users(id);

-- Remove trigger
DROP TRIGGER IF EXISTS preserve_wallet_id_before_delete ON swift_wallets;

-- Remove trigger function
DROP FUNCTION IF EXISTS preserve_wallet_id_on_delete();

ALTER TABLE ledger_entries
    DROP COLUMN deleted_account_id,
    ALTER COLUMN wallet_id SET NOT NULL,
    ALTER COLUMN transaction_id SET NOT NULL;

-- Remove deleted ID columns
ALTER TABLE transactions
    DROP COLUMN deleted_from_account_id,
    DROP COLUMN deleted_to_account_id;