-- +goose Up
CREATE TABLE models (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    engine     TEXT NOT NULL CHECK (engine IN ('llamacpp')),
    endpoint   TEXT NOT NULL,
    model      TEXT NOT NULL,
    created_at TEXT NOT NULL
) STRICT;

-- +goose Down
DROP TABLE models;
