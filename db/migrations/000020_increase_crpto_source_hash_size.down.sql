--! Trim source hashes to 64 characters
UPDATE crypto_transaction_metadata
SET source_hash = LEFT(source_hash, 64);

ALTER TABLE crypto_transaction_metadata
ALTER COLUMN source_hash TYPE VARCHAR(64);