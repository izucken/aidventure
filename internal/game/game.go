// Package game holds the top-level unit of play. A game roots the threads,
// memory and cast that belong to one run; several games exist side by side.
package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("game not found")

// Settings is how a game is told. Both are free text and may be empty.
type Settings struct {
	// Director is the role whoever decides what happens next plays, when not simply a director: a dungeon master, a showrunner.
	Director string `json:"director"`
	// Voice is how narration lines are written: person, tense, distance, register.
	Voice string `json:"voice"`
}

type Game struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	Settings
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, name string) (Game, error) {
	id, err := uuid.NewV7() // time-ordered, so creation order survives without a counter
	if err != nil {
		return Game{}, fmt.Errorf("create game: %w", err)
	}
	g := Game{ID: id.String(), Name: name, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO games (id, name, created_at) VALUES (?, ?, ?)`,
		g.ID, g.Name, g.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Game{}, fmt.Errorf("create game: %w", err)
	}
	return g, nil
}

func (s *Store) List(ctx context.Context) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, director, voice FROM games ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	defer rows.Close()

	games := []Game{}
	for rows.Next() {
		var g Game
		var created string
		if err := rows.Scan(&g.ID, &g.Name, &created, &g.Director, &g.Voice); err != nil {
			return nil, fmt.Errorf("list games: %w", err)
		}
		if g.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list games: %w", err)
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

// Current returns the id of the selected game, or "" when none is selected.
func (s *Store) Current(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'current_game'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("current game: %w", err)
	}
	return id, nil
}

func (s *Store) SetCurrent(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("select game: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM games WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: game %s", ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("select game: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES ('current_game', ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, id)
	if err != nil {
		return fmt.Errorf("select game: %w", err)
	}
	return tx.Commit()
}

// SetSettings replaces how the game is told.
func (s *Store) SetSettings(ctx context.Context, id string, in Settings) error {
	res, err := s.db.ExecContext(ctx, `UPDATE games SET director = ?, voice = ? WHERE id = ?`, strings.TrimSpace(in.Director), strings.TrimSpace(in.Voice), id)
	if err != nil {
		return fmt.Errorf("set game settings: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("set game settings: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: game %s", ErrNotFound, id)
	}
	return nil
}

// ReadSettings is how the game is told, for whoever renders a prompt.
func ReadSettings(ctx context.Context, q Querier, id string) (Settings, error) {
	var in Settings
	err := q.QueryRowContext(ctx, `SELECT director, voice FROM games WHERE id = ?`, id).Scan(&in.Director, &in.Voice)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, fmt.Errorf("%w: game %s", ErrNotFound, id)
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read game settings: %w", err)
	}
	return in, nil
}

// Rename changes what the game is called.
func (s *Store) Rename(ctx context.Context, id, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE games SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("rename game: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("rename game: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: game %s", ErrNotFound, id)
	}
	return nil
}

// Querier is the slice of *sql.DB and *sql.Tx that Require needs.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Require fails with ErrNotFound unless the game exists. Stores that own
// per-game rows call it so an unknown game is an error, not an empty result.
func Require(ctx context.Context, q Querier, id string) error {
	var exists int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM games WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: game %s", ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("look up game: %w", err)
	}
	return nil
}
