-- +goose Up
-- Whether a human answers for this actor. Meaningful only on actors.
ALTER TABLE subjects ADD COLUMN user INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE subjects DROP COLUMN user;
