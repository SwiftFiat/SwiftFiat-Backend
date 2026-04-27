-- 1. Drop the partial unique index
DROP INDEX IF EXISTS user_tokens_user_device_provider_idx;

-- 2. Restore original unique constraint on token (if needed)
ALTER TABLE user_tokens
ADD CONSTRAINT user_tokens_token_key UNIQUE (token);