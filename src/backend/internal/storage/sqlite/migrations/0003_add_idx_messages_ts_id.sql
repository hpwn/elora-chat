-- 0003_add_idx_messages_ts_id.sql
CREATE INDEX IF NOT EXISTS idx_messages_ts_id ON messages(ts DESC, id DESC);
