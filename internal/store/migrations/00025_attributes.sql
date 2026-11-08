-- +goose Up
-- An attribute declared for the game: a named, described, typed quality that
-- subjects and links may carry. Options is a JSON array, meaningful on enums.
CREATE TABLE attributes (
    id          TEXT PRIMARY KEY,
    game_id     TEXT NOT NULL REFERENCES games (id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('flag', 'integer', 'float', 'enum')),
    options     TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL
) STRICT;
CREATE INDEX attributes_game ON attributes (game_id);

-- A value an owner carries: a subject or a link, never both. Structured
-- values point at a declaration; freeform ones carry their own name. The
-- text holds every kind; what fits is checked on the way in. Values go with
-- their owner, which is why they cascade and nothing else here does.
CREATE TABLE attribute_values (
    subject_id   TEXT REFERENCES subjects (id) ON DELETE CASCADE,
    link_id      TEXT REFERENCES links (id) ON DELETE CASCADE,
    attribute_id TEXT REFERENCES attributes (id),
    name         TEXT,
    value        TEXT NOT NULL,
    seq          INTEGER NOT NULL,
    CHECK ((subject_id IS NULL) <> (link_id IS NULL)),
    CHECK ((attribute_id IS NULL) <> (name IS NULL)),
    UNIQUE (subject_id, attribute_id),
    UNIQUE (subject_id, name),
    UNIQUE (link_id, attribute_id),
    UNIQUE (link_id, name)
) STRICT;
CREATE INDEX attribute_values_attribute ON attribute_values (attribute_id);

-- +goose Down
DROP TABLE attribute_values;
DROP TABLE attributes;
