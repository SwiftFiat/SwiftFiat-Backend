--! To enable storage and retrieval of fiat customer accounts
-- Start transaction
BEGIN;

-- Fiat Beneficiaries table
CREATE TABLE IF NOT EXISTS "beneficiaries" (
    "id" UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    "user_id" BIGSERIAL REFERENCES users(id) ON DELETE CASCADE,
    "bank_code" VARCHAR(20) NOT NULL,
    "account_number" VARCHAR(10) UNIQUE NOT NULL,
    "beneficiary_name" VARCHAR(100) NOT NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for faster lookup
CREATE INDEX IF NOT EXISTS "idx_beneficiaries_user_id" ON beneficiaries(user_id);

-- Create triggers for updated_at
CREATE OR REPLACE TRIGGER update_beneficiary_updated_at
    BEFORE UPDATE ON beneficiaries
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- End Transaction
COMMIT;