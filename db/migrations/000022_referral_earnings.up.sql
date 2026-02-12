/**
    total_earned DECIMAL(10, 2) NOT NULL DEFAULT 0

    This tracks the cumulative total amount the user has earned from all successful referrals

    It only increases when new referrals are made

    Example: If a user earns ₦500 from 3 referrals, this would be ₦1500

    Purpose: Shows the user their lifetime earnings from the referral program


  available_balance DECIMAL(10, 2) NOT NULL DEFAULT 0

    This represents the withdrawable amount currently available to the user

    Starts at 0 and increases with each successful referral

    Decreases when the user makes withdrawal requests

    Example: If a user has ₦1500 total_earned but withdrew ₦500, this would be ₦1000

    Purpose: Shows users how much they can withdraw right now


  withdrawn_balance DECIMAL(10, 2) NOT NULL DEFAULT 0

    This tracks the total amount already withdrawn by the user

    Only increases when withdrawals are successfully processed

    Example: If a user made two withdrawals of ₦500 each, this would be ₦1000

    Purpose: Shows users their historical withdrawal activity
 */
CREATE TABLE user_referrals (
                                id SERIAL PRIMARY KEY,
                                referrer_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                referee_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                earned_amount DECIMAL(10, 2) NOT NULL,
                                created_at TIMESTAMP NOT NULL DEFAULT NOW(),
                                UNIQUE (referee_id) -- Ensure a user can't be referred multiple times
);
CREATE TABLE referral_earnings ( 
                                   "id" BIGSERIAL PRIMARY KEY,
                                   "user_id" INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
--     total_earned = available_balance + withdrawn_balance
                                   "total_earned" DECIMAL(10, 2) NOT NULL DEFAULT 0,
                                   "available_balance" DECIMAL(10, 2) NOT NULL DEFAULT 0,
                                   "withdrawn_balance" DECIMAL(10, 2) NOT NULL DEFAULT 0,
                                   "created_at" timestamptz NOT NULL DEFAULT (now()),
                                   "updated_at" timestamptz NOT NULL DEFAULT (now())
);
ALTER TABLE referral_earnings
ADD CONSTRAINT referral_earnings_user_unique UNIQUE (user_id);
ALTER TABLE referral_earnings
ADD CONSTRAINT available_balance_non_negative
CHECK (available_balance >= 0);

ALTER TABLE referral_earnings
ADD CONSTRAINT withdrawn_balance_non_negative
CHECK (withdrawn_balance >= 0);

CREATE TABLE referral_transactions (
  "id" BIGSERIAL PRIMARY KEY,
  "user_id" INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  "amount" DECIMAL(10, 2) NOT NULL,
  "transaction_id" UUID REFERENCES transactions(id) ON DELETE SET NULL,
  "transaction_type" VARCHAR(20) NOT NULL CHECK (transaction_type IN ('credit', 'debit')),
  "reference" VARCHAR(100) NOT NULL,
  "status" VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'successful', 'failed')),
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
); 

CREATE TABLE referral_configs (
      "id" BIGSERIAL PRIMARY KEY,
      "referral_amount" DECIMAL(10, 2) NOT NULL CHECK (referral_amount > 0),
      "referral_percentage_earned_per_conversion" DECIMAL(10, 2) NOT NULL,
      "singleton" BOOLEAN NOT NULL DEFAULT TRUE,
      "created_at" timestamptz NOT NULL DEFAULT (now()),
      "updated_at" timestamptz NOT NULL DEFAULT (now())
);
CREATE UNIQUE INDEX referral_configs_singleton_idx
ON referral_configs (singleton);

-- seed referral configs
INSERT INTO referral_configs (referral_amount, referral_percentage_earned_per_conversion) VALUES (1000, 0.05); 