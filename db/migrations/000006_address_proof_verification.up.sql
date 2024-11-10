ALTER TABLE "proof_of_address_images" ADD COLUMN "verified" BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE "proof_of_address_images" ADD COLUMN "verified_at" TIMESTAMPTZ;
ALTER TABLE "proof_of_address_images" ADD CONSTRAINT "verified_at_must_exist_when_verified" CHECK ((verified = false) OR (verified = true AND verified_at IS NOT NULL));