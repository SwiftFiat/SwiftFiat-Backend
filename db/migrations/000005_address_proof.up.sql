CREATE TABLE IF NOT EXISTS "proof_of_address_images" (
    "id" SERIAL PRIMARY KEY,
    "user_id" INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "filename" VARCHAR(255) NOT NULL,
    "proof_type" VARCHAR(100) NOT NULL,   -- Proof of Address (One of: Utility Bill, Bank Statement, Tenancy Agreement)
    "image_data" BYTEA NOT NULL,           -- Binary data for the image
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
