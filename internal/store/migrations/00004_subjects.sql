-- +goose Up
CREATE TABLE subjects (
    id          TEXT PRIMARY KEY,
    game_id     TEXT NOT NULL REFERENCES games (id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at  TEXT NOT NULL
) STRICT;
CREATE INDEX subjects_game ON subjects (game_id);

-- +goose Down
DROP TABLE subjects;
