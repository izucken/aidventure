-- +goose Up
-- A second engine, OpenRouter, and the key it needs. The engine check is baked
-- into the table, so the table is rebuilt; nothing references it.
CREATE TABLE models_copy AS SELECT id, name, engine, endpoint, model, think, think_budget, created_at FROM models;
DROP TABLE models;
CREATE TABLE models (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    engine       TEXT NOT NULL CHECK (engine IN ('llamacpp', 'openrouter')),
    endpoint     TEXT NOT NULL,
    model        TEXT NOT NULL,
    think        INTEGER NOT NULL DEFAULT 1,
    think_budget INTEGER NOT NULL DEFAULT 0,
    api_key      TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL
) STRICT;
INSERT INTO models (id, name, engine, endpoint, model, think, think_budget, created_at)
    SELECT id, name, engine, endpoint, model, think, think_budget, created_at FROM models_copy ORDER BY rowid;
DROP TABLE models_copy;

-- +goose Down
CREATE TABLE models_copy AS SELECT id, name, engine, endpoint, model, think, think_budget, created_at FROM models WHERE engine = 'llamacpp';
DROP TABLE models;
CREATE TABLE models (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    engine       TEXT NOT NULL CHECK (engine IN ('llamacpp')),
    endpoint     TEXT NOT NULL,
    model        TEXT NOT NULL,
    think        INTEGER NOT NULL DEFAULT 1,
    think_budget INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
) STRICT;
INSERT INTO models (id, name, engine, endpoint, model, think, think_budget, created_at)
    SELECT id, name, engine, endpoint, model, think, think_budget, created_at FROM models_copy ORDER BY rowid;
DROP TABLE models_copy;
