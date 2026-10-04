-- +goose Up
CREATE TABLE games (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL
) STRICT;

-- +goose Down
DROP TABLE games;
