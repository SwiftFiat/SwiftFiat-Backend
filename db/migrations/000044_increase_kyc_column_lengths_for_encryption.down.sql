-- Revert column lengths in kyc table (Note: This might fail if data is too long for original types)
ALTER TABLE "kyc" ALTER COLUMN "full_name" TYPE VARCHAR(255);
ALTER TABLE "kyc" ALTER COLUMN "phone_number" TYPE VARCHAR(20);
ALTER TABLE "kyc" ALTER COLUMN "gender" TYPE VARCHAR(20);
ALTER TABLE "kyc" ALTER COLUMN "bvn" TYPE VARCHAR(11);
ALTER TABLE "kyc" ALTER COLUMN "nin" TYPE VARCHAR(11);
ALTER TABLE "kyc" ALTER COLUMN "id_number" TYPE VARCHAR(50);
ALTER TABLE "kyc" ALTER COLUMN "state" TYPE VARCHAR(100);
ALTER TABLE "kyc" ALTER COLUMN "lga" TYPE VARCHAR(100);
ALTER TABLE "kyc" ALTER COLUMN "house_number" TYPE VARCHAR(50);
ALTER TABLE "kyc" ALTER COLUMN "postal_code" TYPE VARCHAR(10);
ALTER TABLE "kyc" ALTER COLUMN "country" TYPE VARCHAR(20);

-- Restore check constraints and types
ALTER TABLE "kyc" ALTER COLUMN "id_type" TYPE VARCHAR(30);
ALTER TABLE "kyc" ADD CONSTRAINT kyc_id_type_check CHECK ("id_type" IN ('international_passport', 'voters_card', 'drivers_license'));

ALTER TABLE "kyc" ALTER COLUMN "proof_of_address_type" TYPE VARCHAR(30);
ALTER TABLE "kyc" ADD CONSTRAINT kyc_proof_of_address_type_check CHECK ("proof_of_address_type" IN ('utility_bill', 'bank_statement', 'tenancy_agreement'));
