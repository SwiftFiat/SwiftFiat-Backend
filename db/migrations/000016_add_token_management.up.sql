CREATE TABLE IF NOT EXISTS "user_tokens" (
    "id" SERIAL PRIMARY KEY,
    "user_id" BIGSERIAL NOT NULL,
    "token" TEXT UNIQUE NOT NULL,
    "provider" VARCHAR(10) NOT NULL,
    "device_uuid" TEXT,
    "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES "users" (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS "idx_user_tokens_user_id" ON "user_tokens" (user_id);

