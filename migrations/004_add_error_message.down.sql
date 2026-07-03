ALTER TABLE notifications DROP COLUMN IF EXISTS error_message;
ALTER TABLE dlq_notifications DROP COLUMN IF EXISTS error_message;

DROP INDEX IF EXISTS idx_dlq_notifications_user_id;
ALTER TABLE dlq_notifications DROP COLUMN IF EXISTS user_id;
