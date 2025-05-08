-- Add "is_active" field to users table
ALTER TABLE "users"
ADD COLUMN "is_active" BOOLEAN NOT NULL DEFAULT TRUE;