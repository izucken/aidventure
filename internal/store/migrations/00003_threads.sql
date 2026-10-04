-- +goose Up
CREATE TABLE threads (
    id         TEXT PRIMARY KEY,
    game_id    TEXT NOT NULL REFERENCES games (id),
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL
) STRICT;
CREATE INDEX threads_game ON threads (game_id);

-- No REFERENCES here: SQLite cannot DROP a column that carries one, and the
-- store checks ownership before writing it.
ALTER TABLE games ADD COLUMN current_thread_id TEXT;

-- +goose Down
ALTER TABLE games DROP COLUMN current_thread_id;
DROP TABLE threads;
