-- +goose Up
-- Why the open turn went to its actor: the pick job's reasoning, for the human it was handed to.
ALTER TABLE threads ADD COLUMN turn_think TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE threads DROP COLUMN turn_think;
