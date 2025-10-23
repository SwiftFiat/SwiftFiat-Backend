ALTER TABLE audit_logs DROP COLUMN ip;
ALTER TABLE audit_logs DROP COLUMN user_agent;

ALTER TABLE audit_logs RENAME TO activity_logs;
