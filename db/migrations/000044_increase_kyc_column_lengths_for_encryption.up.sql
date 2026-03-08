-- Increase column lengths in kyc table to accommodate encrypted data
ALTER TABLE "kyc" ALTER COLUMN "full_name" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "phone_number" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "gender" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "bvn" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "nin" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "id_number" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "state" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "lga" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "house_number" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "postal_code" TYPE TEXT;
ALTER TABLE "kyc" ALTER COLUMN "country" TYPE TEXT;

-- Remove check constraints for encrypted fields
ALTER TABLE "kyc" DROP CONSTRAINT IF EXISTS kyc_id_type_check;
ALTER TABLE "kyc" ALTER COLUMN "id_type" TYPE TEXT;

ALTER TABLE "kyc" DROP CONSTRAINT IF EXISTS kyc_proof_of_address_type_check;
ALTER TABLE "kyc" ALTER COLUMN "proof_of_address_type" TYPE TEXT;
