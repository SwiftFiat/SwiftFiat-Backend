-- 000046_user_tokens_unique_per_device.down.sql
ALTER TABLE user_tokens DROP CONSTRAINT IF EXISTS user_tokens_user_device_provider_uniq;
ALTER TABLE user_tokens DROP CONSTRAINT IF EXISTS user_tokens_token_uniq;
ALTER TABLE user_tokens ALTER COLUMN device_uuid DROP NOT NULL;
ALTER TABLE user_tokens ADD CONSTRAINT user_tokens_token_key UNIQUE (token);
DROP INDEX IF EXISTS idx_user_tokens_device_uuid;