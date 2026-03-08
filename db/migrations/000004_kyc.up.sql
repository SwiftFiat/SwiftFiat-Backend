/**
 * Tiered KYC System
 * 
 * - Tier 1: Created on email verification (pending)
 * - Tier 2: BVN + NIN verified (verified)
 * - Tier 3: Address + Proof of Address verified (verified)
 */

CREATE TABLE IF NOT EXISTS "kyc" (
    "id" BIGSERIAL PRIMARY KEY,
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK ("status" IN ('pending', 'verified', 'rejected')),
    "tier" VARCHAR(20) NOT NULL DEFAULT 'tier_1' CHECK ("tier" IN ('tier_1', 'tier_2', 'tier_3')),
    "verification_date" TIMESTAMPTZ,

    -- Personal Information
    "full_name" VARCHAR(255),
    "phone_number" VARCHAR(20),
    "email" VARCHAR(255),
    "gender" VARCHAR(20),
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
 * Automatically updates tier and status based on completed fields
 */
CREATE OR REPLACE FUNCTION auto_verify_kyc()
RETURNS TRIGGER AS $$
BEGIN
    -- Tier 2 Verification: Both BVN and NIN must be present
    IF (NEW.bvn IS NOT NULL AND NEW.bvn != '') AND (NEW.nin IS NOT NULL AND NEW.nin != '') THEN
        -- Upgrade to tier_2 if currently tier_1
        IF NEW.tier = 'tier_1' THEN
            NEW.tier := 'tier_2';
        END IF;
        
        -- Set status to verified if it was pending
        IF NEW.status = 'pending' THEN
            NEW.status := 'verified';
            NEW.verification_date := COALESCE(NEW.verification_date, CURRENT_TIMESTAMP);
        END IF;
    END IF;

    -- Tier 3 Verification: Address proof must be present
    IF (NEW.proof_of_address_url IS NOT NULL AND NEW.proof_of_address_url != '') THEN
        NEW.tier := 'tier_3';
        
        -- Ensure status is verified
        IF NEW.status = 'pending' THEN
            NEW.status := 'verified';
            NEW.verification_date := COALESCE(NEW.verification_date, CURRENT_TIMESTAMP);
        END IF;
    END IF;

    -- Sync with users table if status changed to verified
    IF NEW.status = 'verified' AND (TG_OP = 'INSERT' OR OLD.status != 'verified') THEN
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