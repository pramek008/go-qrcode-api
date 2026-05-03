-- migrations/001_init.sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS qr_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    data       TEXT NOT NULL,
    format     VARCHAR(10) NOT NULL DEFAULT 'png',
    size       INT NOT NULL DEFAULT 150,
    color      VARCHAR(7) NOT NULL DEFAULT '#000000',
    bgcolor    VARCHAR(7) NOT NULL DEFAULT '#ffffff',
    file_path  TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_qr_codes_created_at ON qr_codes(created_at DESC);
