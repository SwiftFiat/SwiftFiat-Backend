-- Add deleted ID columns to transactions and ledger_entries
ALTER TABLE transactions 
    ADD COLUMN deleted_from_account_id UUID,
    ADD COLUMN deleted_to_account_id UUID;

ALTER TABLE ledger_entries
    ADD COLUMN deleted_account_id UUID,
    ALTER COLUMN wallet_id DROP NOT NULL,
    ALTER COLUMN transaction_id DROP NOT NULL;

-- Create trigger function for preserving wallet IDs
CREATE OR REPLACE FUNCTION preserve_wallet_id_on_delete()
RETURNS TRIGGER AS $$
BEGIN
    -- Update transactions where this wallet was involved
    UPDATE transactions 
    SET deleted_from_account_id = OLD.id,
        from_account_id = NULL
    WHERE from_account_id = OLD.id;
    
    UPDATE transactions 
    SET deleted_to_account_id = OLD.id,
        to_account_id = NULL
    WHERE to_account_id = OLD.id;
    
    -- Update ledger entries for this wallet
    UPDATE ledger_entries
    SET deleted_account_id = OLD.id,
        wallet_id = NULL
    WHERE wallet_id = OLD.id;
    
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

-- Create the trigger
CREATE TRIGGER preserve_wallet_id_before_delete
    BEFORE DELETE ON swift_wallets
    FOR EACH ROW
    EXECUTE FUNCTION preserve_wallet_id_on_delete();

-- Update foreign key constraints for cascade deletion
ALTER TABLE swift_wallets
    DROP CONSTRAINT swift_wallets_customer_id_fkey,
    ADD CONSTRAINT swift_wallets_customer_id_fkey 
        FOREIGN KEY (customer_id) 
        REFERENCES users(id) 
        ON DELETE CASCADE;

ALTER TABLE ledger_entries
    ADD CONSTRAINT ledger_entries_account_id_fkey 
        FOREIGN KEY (wallet_id) 
        REFERENCES swift_wallets(id)
        ON DELETE SET NULL;