-- +goose Up
-- A link is how one subject stands to another, in the author's words: a
-- directed edge read as "from relation to". Links are authoring, not play,
-- and go with their subjects.
CREATE TABLE links (
    id         TEXT PRIMARY KEY,
    game_id    TEXT NOT NULL REFERENCES games (id),
    from_id    TEXT NOT NULL REFERENCES subjects (id),
    to_id      TEXT NOT NULL REFERENCES subjects (id),
    relation   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    CHECK (from_id <> to_id)
) STRICT;
CREATE INDEX links_game ON links (game_id);

-- +goose Down
DROP TABLE links;
