ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_pkey;
ALTER TABLE templates ADD COLUMN user_id VARCHAR(100) DEFAULT 'global';
ALTER TABLE templates ADD CONSTRAINT templates_pkey PRIMARY KEY (user_id, name);

ALTER TABLE notifications ADD COLUMN user_id VARCHAR(100) DEFAULT 'global';
CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id);
