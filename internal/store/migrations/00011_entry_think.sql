-- +goose Up
-- The actor's reasoning behind a model-written entry. Never rendered into a prompt.
ALTER TABLE entries ADD COLUMN think TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE entries DROP COLUMN think;
