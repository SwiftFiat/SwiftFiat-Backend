BEGIN;

DROP INDEX IF EXISTS idx_admin_alerts_unacked;
DROP INDEX IF EXISTS idx_notifications_created;
DROP INDEX IF EXISTS idx_notification_recipients_user;

DROP TABLE IF EXISTS admin_alerts;
DROP TABLE IF EXISTS notification_recipients;
DROP TABLE IF EXISTS notifications;

COMMIT;
