ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_pkey;
ALTER TABLE templates DROP COLUMN IF EXISTS user_id;
ALTER TABLE templates ADD CONSTRAINT templates_pkey PRIMARY KEY (name);

DROP INDEX IF EXISTS idx_notifications_user_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS user_id;
