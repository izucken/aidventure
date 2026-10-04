-- +goose Up
-- A scene may carry a title of the author's choosing; its place stays what it is.
ALTER TABLE scenes ADD COLUMN title TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE scenes DROP COLUMN title;
