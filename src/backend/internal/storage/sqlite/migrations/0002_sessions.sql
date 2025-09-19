-- 0002_sessions.sql
CREATE TABLE IF NOT EXISTS sessions(
  token TEXT PRIMARY KEY,
  service TEXT NOT NULL,
  data_json TEXT NOT NULL,
  token_expiry INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
