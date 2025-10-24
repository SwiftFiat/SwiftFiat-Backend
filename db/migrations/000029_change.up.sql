ALTER TABLE activity_logs RENAME TO audit_logs;

ALTER TABLE audit_logs
ADD COLUMN ip VARCHAR(45),
ADD COLUMN user_agent VARCHAR(255);
