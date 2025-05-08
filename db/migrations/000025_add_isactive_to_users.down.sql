-- Remove "is_active" field from users table
ALTER TABLE "users"
DROP COLUMN "is_active";