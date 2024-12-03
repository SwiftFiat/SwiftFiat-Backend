CREATE TABLE "user_fcm_tokens" (
    "id" SERIAL PRIMARY KEY,
    "user_id" BIGSERIAL NOT NULL,
    "fcm_token" TEXT UNIQUE NOT NULL,
    "device_uuid" TEXT,
    "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES "users" (id) ON DELETE CASCADE
);

CREATE INDEX "idx_user_fcm_tokens_user_id" ON "user_fcm_tokens" (user_id);

