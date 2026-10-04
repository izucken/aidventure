-- +goose Up
-- Who is in a scene is a fold over its entries: a present entry at the head
-- says who is there as it opens, enter and exit entries say who came and went.
-- The cast set goes; each scene gets a present entry holding what the set
-- held, so old scenes read everyone as there from the start. The type check
-- grows, hence the rebuild; deferred checks let segments ride it out.
PRAGMA defer_foreign_keys = ON;
CREATE TABLE entries_copy AS SELECT id, scene_id, actor_id, seq, type, think, direction, created_at FROM entries;
DROP TABLE entries;
CREATE TABLE entries (
    id         TEXT PRIMARY KEY,
    scene_id   TEXT NOT NULL REFERENCES scenes (id),
    actor_id   TEXT REFERENCES subjects (id),
    seq        INTEGER NOT NULL,
    type       TEXT NOT NULL DEFAULT 'narrative' CHECK (type IN ('narrative', 'cut', 'split', 'converge', 'direction', 'present', 'enter', 'exit')),
    think      TEXT NOT NULL DEFAULT '',
    direction  TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (scene_id, seq)
) STRICT;
INSERT INTO entries (id, scene_id, actor_id, seq, type, think, direction, created_at)
    SELECT id, scene_id, actor_id, seq, type, think, direction, created_at FROM entries_copy ORDER BY rowid;
DROP TABLE entries_copy;

-- Who a present, enter or exit entry is about.
CREATE TABLE entry_actors (
    entry_id   TEXT NOT NULL REFERENCES entries (id),
    subject_id TEXT NOT NULL REFERENCES subjects (id),
    PRIMARY KEY (entry_id, subject_id)
) STRICT;

INSERT INTO entries (id, scene_id, seq, type, created_at)
    SELECT 'present:' || id, id, 0, 'present', created_at FROM scenes;
INSERT INTO entry_actors (entry_id, subject_id)
    SELECT 'present:' || scene_id, subject_id FROM scene_cast;
DROP TABLE scene_cast;

-- +goose Down
CREATE TABLE scene_cast (
    scene_id   TEXT NOT NULL REFERENCES scenes (id),
    subject_id TEXT NOT NULL REFERENCES subjects (id),
    PRIMARY KEY (scene_id, subject_id)
) STRICT;
INSERT OR IGNORE INTO scene_cast (scene_id, subject_id)
    SELECT e.scene_id, a.subject_id FROM entry_actors a JOIN entries e ON e.id = a.entry_id WHERE e.type IN ('present', 'enter');
DROP TABLE entry_actors;
PRAGMA defer_foreign_keys = ON;
CREATE TABLE entries_copy AS SELECT id, scene_id, actor_id, seq, type, think, direction, created_at FROM entries WHERE type NOT IN ('present', 'enter', 'exit');
DROP TABLE entries;
CREATE TABLE entries (
    id         TEXT PRIMARY KEY,
    scene_id   TEXT NOT NULL REFERENCES scenes (id),
    actor_id   TEXT REFERENCES subjects (id),
    seq        INTEGER NOT NULL,
    type       TEXT NOT NULL DEFAULT 'narrative' CHECK (type IN ('narrative', 'cut', 'split', 'converge', 'direction')),
    think      TEXT NOT NULL DEFAULT '',
    direction  TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (scene_id, seq)
) STRICT;
INSERT INTO entries (id, scene_id, actor_id, seq, type, think, direction, created_at)
    SELECT id, scene_id, actor_id, seq, type, think, direction, created_at FROM entries_copy ORDER BY rowid;
DROP TABLE entries_copy;
