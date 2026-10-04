-- +goose Up
-- The one open turn of a thread: the actor whose entry is awaited, if any.
ALTER TABLE threads ADD COLUMN turn_actor_id TEXT;

-- +goose Down
ALTER TABLE threads DROP COLUMN turn_actor_id;
