-- Remove VIP-related fields from users table
ALTER TABLE users DROP COLUMN IF EXISTS current_vip_level_id;
ALTER TABLE users DROP COLUMN IF EXISTS total_transaction_volume;
ALTER TABLE users DROP COLUMN IF EXISTS total_conversion_volume;