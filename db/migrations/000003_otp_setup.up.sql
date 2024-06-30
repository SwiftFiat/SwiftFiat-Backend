CREATE TABLE "otps" (
    "id" BIGSERIAL PRIMARY KEY,
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "otp" VARCHAR(256) NOT NULL,
    "expired" BOOLEAN NOT NULL DEFAULT TRUE,
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "expires_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now())
);