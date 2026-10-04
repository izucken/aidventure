// Package model holds the model entries the server can talk to: which engine
// serves it, where, which model to ask for, and how it may think.
package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("model not found")
	ErrInvalid  = errors.New("invalid model")
)

type Engine string

const (
	// LlamaCpp is a llama-server at an endpoint of your own, serving one model; grammars apply.
	LlamaCpp Engine = "llamacpp"
	// OpenRouter is the hosted gateway: many models behind one key, no grammars.
	OpenRouter Engine = "openrouter"
)

// openRouterEndpoint is where OpenRouter is, which is known ahead of time.
const openRouterEndpoint = "https://openrouter.ai/api/v1"

// Input is what an author sets on a model entry.
type Input struct {
	Name     string `json:"name"`
	Engine   Engine `json:"engine"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	// Think lets the model reason before answering, where the model supports it.
	Think bool `json:"think"`
	// ThinkBudget is how many tokens the thought may run before the model is asked to answer; 0 is unbounded.
	ThinkBudget int `json:"thinkBudget"`
	// Key is the API key an engine needs, if any. Read in, never written out; empty on update keeps the one stored.
	Key string `json:"key,omitempty"`
}

type Model struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Input
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, in Input) (Model, error) {
	if err := in.validate(); err != nil {
		return Model{}, err
	}
	if in.Engine == OpenRouter && in.Key == "" {
		return Model{}, fmt.Errorf("%w: OpenRouter needs an API key", ErrInvalid)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Model{}, fmt.Errorf("create model: %w", err)
	}
	m := Model{ID: id.String(), CreatedAt: time.Now().UTC().Truncate(time.Second), Input: in}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO models (id, name, engine, endpoint, model, think, think_budget, api_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Name, m.Engine, m.Endpoint, m.Model, m.Think, m.ThinkBudget, m.Key, m.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Model{}, fmt.Errorf("create model: %w", err)
	}
	return m, nil
}

func (s *Store) List(ctx context.Context) ([]Model, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, engine, endpoint, model, think, think_budget, api_key, created_at FROM models ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	models := []Model{}
	for rows.Next() {
		var m Model
		var created string
		if err := rows.Scan(&m.ID, &m.Name, &m.Engine, &m.Endpoint, &m.Model, &m.Think, &m.ThinkBudget, &m.Key, &created); err != nil {
			return nil, fmt.Errorf("list models: %w", err)
		}
		if m.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list models: %w", err)
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

func (s *Store) Update(ctx context.Context, id string, in Input) error {
	if err := in.validate(); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE models SET name = ?, engine = ?, endpoint = ?, model = ?, think = ?, think_budget = ?, api_key = CASE WHEN ? = '' THEN api_key ELSE ? END WHERE id = ?`,
		in.Name, in.Engine, in.Endpoint, in.Model, in.Think, in.ThinkBudget, in.Key, in.Key, id)
	if err != nil {
		return fmt.Errorf("update model: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("update model: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: model %s", ErrNotFound, id)
	}
	return nil
}

func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	in.Model = strings.TrimSpace(in.Model)
	in.Key = strings.TrimSpace(in.Key)
	if in.Engine == OpenRouter && in.Endpoint == "" {
		in.Endpoint = openRouterEndpoint
	}
	switch {
	case in.Name == "":
		return fmt.Errorf("%w: name is required", ErrInvalid)
	case in.Engine != LlamaCpp && in.Engine != OpenRouter:
		return fmt.Errorf("%w: unsupported engine %q", ErrInvalid, in.Engine)
	case in.Model == "":
		return fmt.Errorf("%w: model is required", ErrInvalid)
	case in.ThinkBudget < 0:
		return fmt.Errorf("%w: thinking budget %d is negative", ErrInvalid, in.ThinkBudget)
	}
	u, err := url.Parse(in.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: endpoint must be an http(s) URL", ErrInvalid)
	}
	return nil
}

// Copy makes a new entry from an existing one, key and all, named after it.
func (s *Store) Copy(ctx context.Context, id string) (Model, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return Model{}, err
	}
	in := m.Input
	in.Name += " copy"
	return s.Create(ctx, in)
}

func (s *Store) Get(ctx context.Context, id string) (Model, error) {
	var m Model
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, engine, endpoint, model, think, think_budget, api_key, created_at FROM models WHERE id = ?`, id).
		Scan(&m.ID, &m.Name, &m.Engine, &m.Endpoint, &m.Model, &m.Think, &m.ThinkBudget, &m.Key, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Model{}, fmt.Errorf("%w: model %s", ErrNotFound, id)
	}
	if err != nil {
		return Model{}, fmt.Errorf("get model: %w", err)
	}
	if m.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return Model{}, fmt.Errorf("get model: %w", err)
	}
	return m, nil
}

// Current returns the id of the active model entry, or "" when none is active.
func (s *Store) Current(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'current_model'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("current model: %w", err)
	}
	return id, nil
}

func (s *Store) SetCurrent(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES ('current_model', ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, id)
	if err != nil {
		return fmt.Errorf("select model: %w", err)
	}
	return nil
}
