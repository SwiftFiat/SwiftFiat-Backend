ALTER TABLE users
ADD COLUMN twofa_secret VARCHAR(64),
ADD COLUMN twofa_enabled BOOLEAN DEFAULT false;
