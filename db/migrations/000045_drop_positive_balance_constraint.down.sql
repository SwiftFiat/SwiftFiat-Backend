ALTER TABLE swift_wallets ADD CONSTRAINT positive_balance CHECK (balance >= 0);
