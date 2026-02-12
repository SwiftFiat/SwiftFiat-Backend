/**
 * Simplified KYC System - Single Verification Flow
 * 
 * This removes the tiered system and implements a simple binary verification:
 * - User completes KYC → is_kyc_verified = true in users table
 * - No limits, no tiers - just verified or not verified
 * 
 * Required Information:
 * - Full Name
 * - Phone Number
 * - Email
 * - BVN or NIN (at least one required)
 * - Gender
 * - Selfie (Liveness check)
 * - Physical ID (International Passport, Voters Card, Driver's License)
 * - Address (State, LGA, House Number, Street Name, Landmark)
 * - Proof of Address (Utility Bill, Bank Statement, Tenancy Agreement - not older than 3 months)
 */

CREATE TABLE IF NOT EXISTS "kyc" (
    "id" BIGSERIAL PRIMARY KEY,
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK ("status" IN ('pending', 'verified', 'rejected')),
    "verification_date" TIMESTAMPTZ,

    -- Personal Information
    "full_name" VARCHAR(255),
    "phone_number" VARCHAR(20),
    "email" VARCHAR(255),
    "gender" VARCHAR(10) CHECK ("gender" IN ('male', 'female', 'other')),
    "selfie_url" TEXT,

    -- Identity Verification
    "bvn" VARCHAR(11),  -- BVN is typically 11 digits
    "nin" VARCHAR(11),  -- NIN is typically 11 digits
    
    -- Government ID
    "id_type" VARCHAR(30) CHECK ("id_type" IN ('international_passport', 'voters_card', 'drivers_license')),
    "id_number" VARCHAR(50),
    "id_image_url" TEXT,

    -- Address Information
    "state" VARCHAR(100),
    "lga" VARCHAR(100),
    "house_number" VARCHAR(50),
    "street_name" VARCHAR(255),
    "nearest_landmark" VARCHAR(255),
    "postal_code" VARCHAR(10),
    "country" VARCHAR(20),

    -- Proof of Address
    "proof_of_address_type" VARCHAR(30) CHECK ("proof_of_address_type" IN ('utility_bill', 'bank_statement', 'tenancy_agreement')),
    "proof_of_address_url" TEXT,
    "proof_of_address_date" DATE,

    -- Metadata
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
    "additional_info" JSONB DEFAULT '{"verification_logs":[],"admin_notes":""}'::jsonb
);

-- Create indexes for faster lookups
CREATE INDEX IF NOT EXISTS idx_kyc_user_id ON kyc(user_id);
CREATE INDEX IF NOT EXISTS idx_kyc_status ON kyc(status);

-- Auto-update timestamp trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER update_kyc_updated_at
    BEFORE UPDATE ON kyc
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

/**
 * Auto-verification trigger
 * Automatically sets status to 'verified' when all required fields are complete
 * and updates the is_kyc_verified flag in the users table
 */
CREATE OR REPLACE FUNCTION auto_verify_kyc()
RETURNS TRIGGER AS $$
DECLARE
    all_requirements_met BOOLEAN;
BEGIN
    -- Check if all required fields are present
    all_requirements_met := (
        NEW.full_name IS NOT NULL AND
        NEW.phone_number IS NOT NULL AND
        NEW.email IS NOT NULL AND
        (NEW.bvn IS NOT NULL OR NEW.nin IS NOT NULL) AND  -- At least one ID
        NEW.gender IS NOT NULL AND
        NEW.selfie_url IS NOT NULL AND
        NEW.id_type IS NOT NULL AND
        NEW.id_number IS NOT NULL AND
        NEW.id_image_url IS NOT NULL AND
        NEW.state IS NOT NULL AND
        NEW.lga IS NOT NULL AND
        NEW.house_number IS NOT NULL AND
        NEW.street_name IS NOT NULL AND
        NEW.proof_of_address_type IS NOT NULL AND
        NEW.proof_of_address_url IS NOT NULL AND
        NEW.proof_of_address_date IS NOT NULL AND
        -- Ensure proof of address is recent (within 6 months)
        NEW.proof_of_address_date >= (CURRENT_DATE - INTERVAL '6 months')
    );

    -- If all requirements are met, auto-verify
    IF all_requirements_met AND NEW.status = 'pending' THEN
        NEW.status := 'verified';
        NEW.verification_date := CURRENT_TIMESTAMP;
        
        -- Update the users table
        UPDATE users 
        SET is_kyc_verified = true, 
            updated_at = CURRENT_TIMESTAMP
        WHERE id = NEW.user_id;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER trigger_auto_verify_kyc
    BEFORE INSERT OR UPDATE ON kyc
    FOR EACH ROW
    EXECUTE FUNCTION auto_verify_kyc();

/**
 * Trigger to sync KYC rejection with users table
 * When KYC status is set to 'rejected', ensure is_kyc_verified is false
 */
CREATE OR REPLACE FUNCTION sync_kyc_rejection()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'rejected' AND (OLD.status IS NULL OR OLD.status != 'rejected') THEN
        UPDATE users 
        SET is_kyc_verified = false, 
            updated_at = CURRENT_TIMESTAMP
        WHERE id = NEW.user_id;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER trigger_sync_kyc_rejection
    AFTER UPDATE ON kyc
    FOR EACH ROW
    WHEN (NEW.status = 'rejected')
    EXECUTE FUNCTION sync_kyc_rejection();