CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ciphertext contains an encrypted envelope with the secret payload AND its metadata
-- (content_type, filename) so the server never sees plaintext metadata.
CREATE TABLE IF NOT EXISTS secrets (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token        TEXT        NOT NULL UNIQUE,
    ciphertext   BYTEA       NOT NULL,
    nonce        BYTEA       NOT NULL,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_secrets_token ON secrets (token);
CREATE INDEX IF NOT EXISTS idx_secrets_expires_at ON secrets (expires_at) WHERE expires_at IS NOT NULL;
