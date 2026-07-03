CREATE TABLE IF NOT EXISTS notifications (
    id            VARCHAR(64) PRIMARY KEY,
    recipient     VARCHAR(255) NOT NULL,
    template      VARCHAR(255) NOT NULL,
    variable      JSONB NOT NULL,
    retry_count   INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL,
    type          VARCHAR(32) NOT NULL,
    status        VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    next_retry_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS dlq_notifications (
    id            VARCHAR(64) PRIMARY KEY,
    recipient     VARCHAR(255) NOT NULL,
    template      VARCHAR(255) NOT NULL,
    variable      JSONB NOT NULL,
    retry_count   INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL,
    type          VARCHAR(32) NOT NULL,
    status        VARCHAR(32) NOT NULL DEFAULT 'DLQ',
    next_retry_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notifications_pending_polling
    ON notifications (next_retry_at)
    WHERE status = 'PENDING';
