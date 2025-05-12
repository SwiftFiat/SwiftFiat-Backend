ALTER TABLE user_referrals
ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active'));
