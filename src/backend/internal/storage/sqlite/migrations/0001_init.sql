CREATE TABLE IF NOT EXISTS messages(
    id TEXT PRIMARY KEY,
    ts INTEGER NOT NULL,
    username TEXT NOT NULL,
    platform TEXT NOT NULL,
    text TEXT NOT NULL,
    emotes_json TEXT NOT NULL DEFAULT '[]',
    badges_json TEXT NOT NULL DEFAULT '[]',
    raw_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(ts DESC);
CREATE INDEX IF NOT EXISTS idx_messages_platform_ts ON messages(platform, ts DESC);
CREATE INDEX IF NOT EXISTS idx_messages_user_ts ON messages(username, ts DESC);
