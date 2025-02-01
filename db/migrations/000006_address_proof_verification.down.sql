ALTER TABLE "proof_of_address_images" DROP CONSTRAINT "verified_at_must_exist_when_verified";
ALTER TABLE "proof_of_address_images" DROP COLUMN IF EXISTS "verified_at";
ALTER TABLE "proof_of_address_images" DROP COLUMN IF EXISTS "verified";
