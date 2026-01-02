-- Add VIP-related fields to users table
ALTER TABLE users ADD COLUMN total_conversion_volume DECIMAL(20,8) DEFAULT 0;
ALTER TABLE users ADD COLUMN total_transaction_volume DECIMAL(20,8) DEFAULT 0;
ALTER TABLE users ADD COLUMN current_vip_level_id UUID REFERENCES vip_levels(id);