-- +goose Up
-- A director's decision is an entry too: what happened in the thread, in
-- order, beats and directions alike. A direction has no actor and carries its
-- decision; an open turn is simply a direction nothing has followed yet, so
-- the thread no longer holds one. The actor column loosens and the type check
-- grows, hence the rebuild; deferred checks let segments ride it out.
PRAGMA defer_foreign_keys = ON;
CREATE TABLE entries_copy AS SELECT id, scene_id, actor_id, seq, type, think, created_at FROM entries;
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
INSERT INTO entries (id, scene_id, actor_id, seq, type, think, created_at)
    SELECT id, scene_id, actor_id, seq, type, think, created_at FROM entries_copy ORDER BY rowid;
DROP TABLE entries_copy;
ALTER TABLE threads DROP COLUMN turn_actor_id;
ALTER TABLE threads DROP COLUMN turn_think;
ALTER TABLE threads DROP COLUMN turn_direction;

-- +goose Down
ALTER TABLE threads ADD COLUMN turn_actor_id TEXT;
ALTER TABLE threads ADD COLUMN turn_think TEXT NOT NULL DEFAULT '';
ALTER TABLE threads ADD COLUMN turn_direction TEXT NOT NULL DEFAULT '';
PRAGMA defer_foreign_keys = ON;
CREATE TABLE entries_copy AS SELECT id, scene_id, actor_id, seq, type, think, created_at FROM entries WHERE type <> 'direction';
DROP TABLE entries;
CREATE TABLE entries (
    id         TEXT PRIMARY KEY,
    scene_id   TEXT NOT NULL REFERENCES scenes (id),
    actor_id   TEXT NOT NULL REFERENCES subjects (id),
    seq        INTEGER NOT NULL,
    type       TEXT NOT NULL DEFAULT 'narrative' CHECK (type IN ('narrative', 'cut', 'split', 'converge')),
    think      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (scene_id, seq)
) STRICT;
INSERT INTO entries (id, scene_id, actor_id, seq, type, think, created_at)
    SELECT id, scene_id, actor_id, seq, type, think, created_at FROM entries_copy ORDER BY rowid;
DROP TABLE entries_copy;
