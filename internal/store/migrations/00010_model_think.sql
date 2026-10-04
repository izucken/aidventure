-- +goose Up
-- Whether the model reasons before answering. A property of the model entry, not of requests.
ALTER TABLE models ADD COLUMN think INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE models DROP COLUMN think;
