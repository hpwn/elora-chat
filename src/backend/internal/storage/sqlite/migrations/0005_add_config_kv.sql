CREATE TABLE IF NOT EXISTS config_kv (
    key TEXT PRIMARY KEY,
    version INTEGER NOT NULL,
    value_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
