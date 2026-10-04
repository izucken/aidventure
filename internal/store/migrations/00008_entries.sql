-- +goose Up
CREATE TABLE entries (
    id         TEXT PRIMARY KEY,
    scene_id   TEXT NOT NULL REFERENCES scenes (id),
    actor_id   TEXT NOT NULL REFERENCES subjects (id),
    seq        INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (scene_id, seq)
) STRICT;

CREATE TABLE segments (
    entry_id TEXT NOT NULL REFERENCES entries (id),
    seq      INTEGER NOT NULL,
    kind     TEXT NOT NULL CHECK (kind IN ('narration', 'action', 'speech', 'thought')),
    text     TEXT NOT NULL,
    to_id    TEXT REFERENCES subjects (id),
    PRIMARY KEY (entry_id, seq)
) STRICT;

-- +goose Down
DROP TABLE segments;
DROP TABLE entries;
