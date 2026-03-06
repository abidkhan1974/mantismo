-- Mantismo Vault Schema
-- Encrypted via SQLCipher (AES-256)

CREATE TABLE IF NOT EXISTS vault_entries (
    key           TEXT PRIMARY KEY,
    value         TEXT NOT NULL,
    category      TEXT NOT NULL,
    sensitivity   TEXT NOT NULL DEFAULT 'standard',
    label         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_category ON vault_entries(category);
CREATE INDEX IF NOT EXISTS idx_sensitivity ON vault_entries(sensitivity);

CREATE TABLE IF NOT EXISTS vault_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
