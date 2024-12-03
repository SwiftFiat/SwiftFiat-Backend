--! Rollback user-tags for the user
-- Down migration to remove user_tag column from users table
ALTER TABLE "users" 
DROP COLUMN IF EXISTS "user_tag",
DROP COLUMN IF EXISTS "fresh_chat_id";