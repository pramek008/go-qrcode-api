-- migrations/003_add_api_keys.sql
CREATE TABLE IF NOT EXISTS api_keys (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(100) NOT NULL,
    key               VARCHAR(64) NOT NULL UNIQUE,
    rate_limit        INT NOT NULL DEFAULT 30,
    rate_limit_window INT NOT NULL DEFAULT 60,
    quota             INT NOT NULL DEFAULT 0,
    quota_used        INT NOT NULL DEFAULT 0,
    is_active         BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(key);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(is_active);
