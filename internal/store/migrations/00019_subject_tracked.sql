-- +goose Up
-- Whether the story follows this actor: a split exists to keep following
-- tracked actors who part. Meaningful only on actors.
ALTER TABLE subjects ADD COLUMN tracked INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE subjects DROP COLUMN tracked;
