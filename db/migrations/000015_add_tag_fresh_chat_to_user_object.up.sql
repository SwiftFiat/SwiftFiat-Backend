--! To introduce user-tags for the user
-- Migration to update users table
ALTER TABLE "users" 
ADD COLUMN IF NOT EXISTS "user_tag" VARCHAR(50) UNIQUE DEFAULT NULL,
ADD COLUMN IF NOT EXISTS "fresh_chat_id" TEXT UNIQUE DEFAULT NULL;