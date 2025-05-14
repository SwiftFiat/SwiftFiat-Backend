CREATE TABLE activity_logs (
    "id" SERIAL PRIMARY KEY,
    "user_id" INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "action" VARCHAR(255) NOT NULL,
    "created_at" TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create an index for faster lookups
CREATE INDEX idx_activity_logs_user_id ON activity_logs(user_id);
CREATE INDEX idx_activity_logs_created_at ON activity_logs(created_at);