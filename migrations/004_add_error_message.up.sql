ALTER TABLE dlq_notifications ADD COLUMN IF NOT EXISTS user_id VARCHAR(100) DEFAULT 'global';
CREATE INDEX IF NOT EXISTS idx_dlq_notifications_user_id ON dlq_notifications(user_id);

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS error_message TEXT;
ALTER TABLE dlq_notifications ADD COLUMN IF NOT EXISTS error_message TEXT;
