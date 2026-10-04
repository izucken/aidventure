-- +goose Up
-- How many tokens a thinking model may think for before it is asked to answer; 0 is unbounded.
ALTER TABLE models ADD COLUMN think_budget INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE models DROP COLUMN think_budget;
