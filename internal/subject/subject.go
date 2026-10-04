// Package subject holds the things a game is about: anything that can carry
// facts and relations. Characters, places, objects, debts. Being a subject
// says nothing about taking turns; that is declared separately.
package subject

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"aidventure/internal/game"
)

var (
	ErrNotFound = errors.New("subject not found")
	// ErrInUse means the subject is referenced by play and must not vanish from under it.
	ErrInUse = errors.New("subject is in use")
)

type Subject struct {
	ID          string `json:"id"`
	GameID      string `json:"gameId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Actor marks a subject that may take turns. Declared by the author, never inferred.
	Actor bool `json:"actor"`
	// Scene marks a subject that may host a scene: a place, a setting.
	Scene bool `json:"scene"`
	// User marks an actor a human answers for; the model plays the rest.
	User bool `json:"user"`
	// Tracked marks an actor the story follows: a split keeps following tracked actors who part.
	Tracked   bool      `json:"tracked"`
	CreatedAt time.Time `json:"createdAt"`
}

// Input is what an author sets on a subject.
type Input struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Actor       bool   `json:"actor"`
	Scene       bool   `json:"scene"`
	User        bool   `json:"user"`
	Tracked     bool   `json:"tracked"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, gameID string, in Input) (Subject, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Subject{}, fmt.Errorf("create subject: %w", err)
	}
	sub := Subject{
		ID: id.String(), GameID: gameID, Name: in.Name, Description: in.Description, Actor: in.Actor, Scene: in.Scene, User: in.User, Tracked: in.Tracked,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Subject{}, fmt.Errorf("create subject: %w", err)
	}
	defer tx.Rollback()
	if err := game.Require(ctx, tx, gameID); err != nil {
		return Subject{}, err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO subjects (id, game_id, name, description, actor, scene, user, tracked, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.GameID, sub.Name, sub.Description, sub.Actor, sub.Scene, sub.User, sub.Tracked, sub.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Subject{}, fmt.Errorf("create subject: %w", err)
	}
	return sub, tx.Commit()
}

func (s *Store) List(ctx context.Context, gameID string) ([]Subject, error) {
	if err := game.Require(ctx, s.db, gameID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, game_id, name, description, actor, scene, user, tracked, created_at FROM subjects WHERE game_id = ? ORDER BY id`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	defer rows.Close()

	subjects := []Subject{}
	for rows.Next() {
		var sub Subject
		var created string
		if err := rows.Scan(&sub.ID, &sub.GameID, &sub.Name, &sub.Description, &sub.Actor, &sub.Scene, &sub.User, &sub.Tracked, &created); err != nil {
			return nil, fmt.Errorf("list subjects: %w", err)
		}
		if sub.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list subjects: %w", err)
		}
		subjects = append(subjects, sub)
	}
	return subjects, rows.Err()
}

// Delete removes a subject that nothing refers to. A subject that hosts a
// scene, acts in an entry or is addressed in one stays, with ErrInUse saying where.
func (s *Store) Delete(ctx context.Context, gameID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete subject: %w", err)
	}
	defer tx.Rollback()

	var uses []string
	for _, u := range []struct{ what, query string }{
		{"scene", `SELECT count(*) FROM scenes WHERE place_id = ?`},
		{"entry", `SELECT count(*) FROM entries WHERE actor_id = ?`},
		{"address", `SELECT count(*) FROM segments WHERE to_id = ?`},
		{"presence", `SELECT count(*) FROM entry_actors WHERE subject_id = ?`},
	} {
		var n int
		if err := tx.QueryRowContext(ctx, u.query, id).Scan(&n); err != nil {
			return fmt.Errorf("delete subject: %w", err)
		}
		if n > 0 {
			uses = append(uses, fmt.Sprintf("%d %s", n, plural(u.what, n)))
		}
	}
	if len(uses) > 0 {
		return fmt.Errorf("%w: %s", ErrInUse, strings.Join(uses, ", "))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE from_id = ? OR to_id = ?`, id, id); err != nil {
		return fmt.Errorf("delete subject: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM subjects WHERE id = ? AND game_id = ?`, id, gameID)
	if err != nil {
		return fmt.Errorf("delete subject: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("delete subject: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: subject %s in game %s", ErrNotFound, id, gameID)
	}
	return tx.Commit()
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	if word == "address" {
		return "addresses"
	}
	if word == "entry" {
		return "entries"
	}
	return word + "s"
}

// Update replaces the editable fields of a subject that belongs to gameID.
func (s *Store) Update(ctx context.Context, gameID, id string, in Input) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE subjects SET name = ?, description = ?, actor = ?, scene = ?, user = ?, tracked = ? WHERE id = ? AND game_id = ?`,
		in.Name, in.Description, in.Actor, in.Scene, in.User, in.Tracked, id, gameID)
	if err != nil {
		return fmt.Errorf("update subject: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("update subject: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: subject %s in game %s", ErrNotFound, id, gameID)
	}
	return nil
}
