-- +goose Up
-- An entry's type is how it moves the play: on in its scene, or into another
-- scene or thread. The cut flag becomes the cut type; the place it carried is
-- the opened scene's place, so the column goes. SQLite cannot drop a column
-- under a foreign key, hence the rebuild. Checks are deferred so segments may
-- point at a table that is dropped and recreated inside the transaction; the
-- violations the drop counts are paid back row by row as the copy comes back.
PRAGMA defer_foreign_keys = ON;
CREATE TABLE entries_copy AS SELECT id, scene_id, actor_id, seq, think, cut_place_id, created_at FROM entries;
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
    SELECT id, scene_id, actor_id, seq, CASE WHEN cut_place_id IS NULL THEN 'narrative' ELSE 'cut' END, think, created_at
    FROM entries_copy ORDER BY rowid;
DROP TABLE entries_copy;

-- Where a thread came from and went: the scene a split left it from, and the
-- converge entry, in another thread, that absorbed it. An ended thread is history.
-- No REFERENCES, as with current_thread_id.
ALTER TABLE threads ADD COLUMN parent_scene_id TEXT;
ALTER TABLE threads ADD COLUMN ended_by_entry_id TEXT;
-- The direction the open turn is to carry out, as the director decided it.
ALTER TABLE threads ADD COLUMN turn_direction TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE threads DROP COLUMN turn_direction;
ALTER TABLE threads DROP COLUMN ended_by_entry_id;
ALTER TABLE threads DROP COLUMN parent_scene_id;
PRAGMA defer_foreign_keys = ON;
CREATE TABLE entries_copy AS SELECT id, scene_id, actor_id, seq, type, think, created_at FROM entries;
DROP TABLE entries;
CREATE TABLE entries (
    id           TEXT PRIMARY KEY,
    scene_id     TEXT NOT NULL REFERENCES scenes (id),
    actor_id     TEXT NOT NULL REFERENCES subjects (id),
    seq          INTEGER NOT NULL,
    think        TEXT NOT NULL DEFAULT '',
    cut_place_id TEXT REFERENCES subjects (id),
    created_at   TEXT NOT NULL,
    UNIQUE (scene_id, seq)
) STRICT;
INSERT INTO entries (id, scene_id, actor_id, seq, think, cut_place_id, created_at)
    SELECT c.id, c.scene_id, c.actor_id, c.seq, c.think,
           CASE WHEN c.type = 'narrative' THEN NULL ELSE (SELECT place_id FROM scenes WHERE id = c.scene_id) END, c.created_at
    FROM entries_copy c ORDER BY rowid;
DROP TABLE entries_copy;
