CREATE TABLE IF NOT EXISTS app_logs (
    id        BIGSERIAL    PRIMARY KEY,
    ts        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    level     TEXT         NOT NULL,
    msg       TEXT         NOT NULL,
    attrs     JSONB
);

CREATE INDEX IF NOT EXISTS idx_app_logs_ts    ON app_logs (ts DESC);
CREATE INDEX IF NOT EXISTS idx_app_logs_level ON app_logs (level);
