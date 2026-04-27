ALTER TABLE user_tokens DROP CONSTRAINT IF EXISTS user_tokens_token_key;
DELETE FROM user_tokens a USING user_tokens b
WHERE a.id < b.id
  AND a.user_id = b.user_id
  AND a.device_uuid = b.device_uuid
  AND a.provider = b.provider;
CREATE UNIQUE INDEX user_tokens_user_device_provider_idx
  ON user_tokens (user_id, device_uuid, provider)
  WHERE device_uuid IS NOT NULL;