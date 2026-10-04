-- +goose Up
-- A scene opens with an entry, and knows who is present.
ALTER TABLE scenes ADD COLUMN opening_entry_id TEXT;
ALTER TABLE entries ADD COLUMN cut_place_id TEXT REFERENCES subjects (id);

CREATE TABLE scene_cast (
    scene_id   TEXT NOT NULL REFERENCES scenes (id),
    subject_id TEXT NOT NULL REFERENCES subjects (id),
    PRIMARY KEY (scene_id, subject_id)
) STRICT;

-- +goose Down
DROP TABLE scene_cast;
ALTER TABLE entries DROP COLUMN cut_place_id;
ALTER TABLE scenes DROP COLUMN opening_entry_id;
