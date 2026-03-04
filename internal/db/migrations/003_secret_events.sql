-- Tracks lifecycle events for secrets: created, retrieved (consumed), and expired.
-- count > 1 is used for batch expiry rows so the cleanup goroutine emits one row
-- per run rather than one row per expired secret.
CREATE TABLE IF NOT EXISTS secret_events (
    id         BIGSERIAL    PRIMARY KEY,
    event_type TEXT         NOT NULL CHECK (event_type IN ('created', 'retrieved', 'expired')),
    count      BIGINT       NOT NULL DEFAULT 1,
    ts         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_secret_events_type ON secret_events (event_type);
CREATE INDEX IF NOT EXISTS idx_secret_events_ts   ON secret_events (ts DESC);
