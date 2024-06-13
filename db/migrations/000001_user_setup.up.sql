CREATE TABLE "users" (
    "id" BIGSERIAL PRIMARY KEY,
    "first_name" VARCHAR(50),
    "last_name" VARCHAR(50),
    "email" VARCHAR(256) UNIQUE NOT NULL,
    "hashed_password" VARCHAR(256),
    "hashed_passcode" VARCHAR(256),
    "hashed_pin" VARCHAR(256),
    "phone_number" VARCHAR(50) UNIQUE NOT NULL,
    "role" VARCHAR(10) NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now()),
    "deleted_at" timestamptz
);