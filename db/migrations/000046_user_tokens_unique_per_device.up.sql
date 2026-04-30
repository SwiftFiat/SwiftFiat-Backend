BEGIN;

-- 0a. Wipe rows with NULL device_uuid — they predate device tracking,
--     can't be deduped meaningfully, users will re-register on next login.
DELETE FROM user_tokens WHERE device_uuid IS NULL OR device_uuid = '';

-- 0b. For (user, device, provider) duplicates, keep the most recently updated.
WITH ranked AS (
  SELECT id,
         ROW_NUMBER() OVER (
           PARTITION BY user_id, device_uuid, provider
           ORDER BY updated_at DESC, id DESC
         ) AS rn
  FROM user_tokens
)
DELETE FROM user_tokens WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

-- 0c. For global token duplicates (same token across multiple users — the
--     classic Bug #1 footprint), keep the most recently updated.
WITH ranked AS (
  SELECT id,
         ROW_NUMBER() OVER (
           PARTITION BY token
           ORDER BY updated_at DESC, id DESC
         ) AS rn
  FROM user_tokens
)
DELETE FROM user_tokens WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

-- 1. Now the constraint changes can apply cleanly:
ALTER TABLE user_tokens DROP CONSTRAINT IF EXISTS user_tokens_token_key;
ALTER TABLE user_tokens ALTER COLUMN device_uuid SET NOT NULL;
ALTER TABLE user_tokens
  ADD CONSTRAINT user_tokens_user_device_provider_uniq
  UNIQUE (user_id, device_uuid, provider);
ALTER TABLE user_tokens
  ADD CONSTRAINT user_tokens_token_uniq UNIQUE (token);

CREATE INDEX IF NOT EXISTS idx_user_tokens_device_uuid
  ON user_tokens (device_uuid);

COMMIT;