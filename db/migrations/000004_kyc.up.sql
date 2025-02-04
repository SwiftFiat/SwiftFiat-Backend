/**
 * KYC Levels and Requirements:
 * 
 * LEVEL 1 (Daily Transfer: 50,000 NGN, Max Balance: 200,000 NGN)
 * - Full Name
 * - Phone Number
 * - Email
 * - BVN or NIN
 * - Gender
 * - Selfie (Liveness check)
 * 
 * LEVEL 2 (Daily Transfer: 200,000 NGN, Max Balance: 500,000 NGN)
 * - Additional ID (NIN if BVN was provided in Level 1, or vice versa)
 * - Physical ID (International Passport, Voters Card, Driver's License)
 * - Address (State, LGA, House Number, Street Name, Landmark)
 * 
 * LEVEL 3 (Daily Transfer: 5,000,000 NGN, Max Balance: Unlimited)
 * - Proof of Address (One of: Utility Bill, Bank Statement, Tenancy Agreement)
 *   Documents must not be older than 3 months
 */
CREATE TABLE IF NOT EXISTS "kyc" (
    "id" BIGSERIAL PRIMARY KEY,
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "tier" INTEGER NOT NULL DEFAULT 0 CHECK ("tier" BETWEEN 0 AND 3),  -- Tier can be 0, 1, 2, or 3
    "daily_transfer_limit_ngn" DECIMAL(15, 2) CHECK ("daily_transfer_limit_ngn" >= 0) DEFAULT 0,
    "wallet_balance_limit_ngn" DECIMAL(15, 2) CHECK ("wallet_balance_limit_ngn" >= 0) DEFAULT 0,
    "status" VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK ("status" IN ('pending', 'active', 'rejected')),
    "verification_date" TIMESTAMPTZ,

    -- Level 1 Fields
    "full_name" VARCHAR(255),
    "phone_number" VARCHAR(20),
    "email" VARCHAR(255),
    "bvn" VARCHAR(11),  -- BVN is typically 11 digits
    "nin" VARCHAR(11),  -- NIN is typically 11 digits
    "gender" VARCHAR(10) CHECK ("gender" IN ('male', 'female', 'other')),
    "selfie_url" TEXT,  -- URL to stored image

    -- Level 2 Fields
    "id_type" VARCHAR(20) CHECK ("id_type" IN ('international_passport', 'voters_card', 'drivers_license')),
    "id_number" VARCHAR(50),
    "id_image_url" TEXT,
    "state" VARCHAR(100),
    "lga" VARCHAR(100),
    "house_number" VARCHAR(50),
    "street_name" VARCHAR(255),
    "nearest_landmark" VARCHAR(255),

    -- Level 3 Fields
    "proof_of_address_type" VARCHAR(20) CHECK ("proof_of_address_type" IN ('utility_bill', 'bank_statement', 'tenancy_agreement')),
    "proof_of_address_url" TEXT,
    "proof_of_address_date" DATE,  -- To verify document is not older than 3 months

    -- Metadata
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
    "additional_info" JSONB DEFAULT '{"data":{}}'::jsonb
);

-- Create an index on user_id for faster lookups
CREATE INDEX IF NOT EXISTS idx_kyc_user_id ON kyc(user_id);

-- Create a trigger to automatically update the updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE OR REPLACE TRIGGER update_kyc_updated_at
    BEFORE UPDATE ON kyc
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

/** Augment KYC Tier**/
CREATE OR REPLACE FUNCTION update_kyc_tier()
RETURNS TRIGGER AS $$
DECLARE
    can_be_tier_1 BOOLEAN;
    can_be_tier_2 BOOLEAN;
    can_be_tier_3 BOOLEAN;
BEGIN
    -- Check Tier 1 requirements
    can_be_tier_1 := (
        NEW.full_name IS NOT NULL AND
        NEW.phone_number IS NOT NULL AND
        NEW.email IS NOT NULL AND
        (NEW.bvn IS NOT NULL OR NEW.nin IS NOT NULL) AND
        NEW.gender IS NOT NULL AND
        NEW.selfie_url IS NOT NULL
    );

    -- Check Tier 2 requirements (must meet Tier 1 first)
    can_be_tier_2 := (
        can_be_tier_1 AND
        NEW.bvn IS NOT NULL AND 
        NEW.nin IS NOT NULL
    );

    -- Check Tier 3 requirements (must meet Tier 2 first)
    can_be_tier_3 := (
        can_be_tier_2 AND
        NEW.proof_of_address_type IS NOT NULL AND
        NEW.proof_of_address_url IS NOT NULL AND
        NEW.proof_of_address_date IS NOT NULL AND
        -- Check if proof of address is not older than 3 months
        NEW.proof_of_address_date >= (CURRENT_DATE - INTERVAL '6 months')
    );

    -- Update tier and corresponding limits
    IF can_be_tier_3 THEN
        NEW.tier := 3;
        NEW.daily_transfer_limit_ngn := 5000000.00;
        NEW.wallet_balance_limit_ngn := NULL; -- Unlimited
    ELSIF can_be_tier_2 THEN
        NEW.tier := 2;
        NEW.daily_transfer_limit_ngn := 200000.00;
        NEW.wallet_balance_limit_ngn := 500000.00;
    ELSIF can_be_tier_1 THEN
        NEW.tier := 1;
        NEW.daily_transfer_limit_ngn := 50000.00;
        NEW.wallet_balance_limit_ngn := 200000.00;
    ELSE
        NEW.tier := 0;
        NEW.daily_transfer_limit_ngn := 0.00;
        NEW.wallet_balance_limit_ngn := 0.00;
    END IF;

    -- Update verification date if tier changed
    IF NEW.tier != OLD.tier OR OLD.tier IS NULL THEN
        NEW.verification_date := CURRENT_TIMESTAMP;
        
        -- Update status to 'active' if tier is greater than 0
        IF NEW.tier > 0 THEN
            NEW.status := 'active';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to run before insert or update
CREATE OR REPLACE TRIGGER trigger_update_kyc_tier
    BEFORE INSERT OR UPDATE ON kyc
    FOR EACH ROW
    EXECUTE FUNCTION update_kyc_tier();