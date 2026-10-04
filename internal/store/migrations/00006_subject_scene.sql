-- +goose Up
-- Declared, never inferred: only flagged subjects can host a scene.
ALTER TABLE subjects ADD COLUMN scene INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE subjects DROP COLUMN scene;
