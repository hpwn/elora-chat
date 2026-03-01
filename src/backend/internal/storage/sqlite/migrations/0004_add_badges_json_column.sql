-- 0004_add_badges_json_column.sql
ALTER TABLE messages ADD COLUMN badges_json TEXT NOT NULL DEFAULT '[]';
