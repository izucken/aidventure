-- +goose Up
-- Declared, never inferred: only flagged subjects can take turns.
ALTER TABLE subjects ADD COLUMN actor INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE subjects DROP COLUMN actor;
