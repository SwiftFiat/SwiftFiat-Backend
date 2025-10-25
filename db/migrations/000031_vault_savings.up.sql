CREATE TABLE IF NOT EXISTS "vault_savings" (
    -- Unique identifier for each vault savings account
    "id" BIGSERIAL PRIMARY KEY,

    -- Reference to the user who owns this vault savings account
    "user_id" BIGINT NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,

    -- Name of the vault savings account
    "name" VARCHAR(100) NOT NULL,

    -- Description of the vault savings account
    "description" TEXT,

    -- Target amount to be saved in this vault
    "target_amount" NUMERIC(20, 8) NOT NULL DEFAULT 0.0,

    -- Current amount saved in this vault
    "current_amount" NUMERIC(20, 8) NOT NULL DEFAULT 0.0,

    -- Status of the vault savings account
    -- Suggested values: 'active', 'paused', 'completed'
    "status" VARCHAR(10) NOT NULL DEFAULT 'active',

    -- Audit timestamps
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now()),
    -- Soft delete implementation
    "deleted_at" timestamptz
);