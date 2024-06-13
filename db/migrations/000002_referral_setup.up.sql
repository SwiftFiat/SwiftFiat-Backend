CREATE TABLE "referrals" (
    "id" BIGSERIAL PRIMARY KEY,
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "referral_key" VARCHAR(256) NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "referral_entries" (
    "id" BIGSERIAL PRIMARY KEY,
    "referral_key" VARCHAR(256) NOT NULL,
    "referrer" INTEGER NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    "referee" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    "referral_detail" VARCHAR(256) NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now()),
    "deleted_at" timestamptz
);