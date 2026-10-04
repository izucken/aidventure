-- +goose Up
-- What a game says about how it is told: the role the director plays, when
-- not simply a director, and the voice narration is written in.
ALTER TABLE games ADD COLUMN director TEXT NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN voice TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE games DROP COLUMN voice;
ALTER TABLE games DROP COLUMN director;
