-- +goose Up
CREATE TABLE scenes (
    id         TEXT PRIMARY KEY,
    thread_id  TEXT NOT NULL REFERENCES threads (id),
    place_id   TEXT NOT NULL REFERENCES subjects (id),
    created_at TEXT NOT NULL
) STRICT;
CREATE INDEX scenes_thread ON scenes (thread_id);

-- +goose Down
DROP TABLE scenes;
