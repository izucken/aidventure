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
	ErrNoLink  = errors.New("link not found")
	ErrBadLink = errors.New("invalid link")
)

// Link is how one subject stands to another, in the author's words: a
// directed edge read as "From relation To". Both ends are subjects of the
// same game.
type Link struct {
	ID        string    `json:"id"`
	GameID    string    `json:"gameId"`
	FromID    string    `json:"fromId"`
	ToID      string    `json:"toId"`
	Relation  string    `json:"relation"`
	CreatedAt time.Time `json:"createdAt"`
}

// LinkInput is what an author sets on a link.
type LinkInput struct {
	FromID   string `json:"fromId"`
	ToID     string `json:"toId"`
	Relation string `json:"relation"`
}

func (in *LinkInput) validate() error {
	in.Relation = strings.TrimSpace(in.Relation)
	switch {
	case in.Relation == "":
		return fmt.Errorf("%w: relation is empty", ErrBadLink)
	case in.FromID == in.ToID:
		return fmt.Errorf("%w: a subject cannot link to itself", ErrBadLink)
	}
	return nil
}

// Links returns the game's links in the order they were made.
func (s *Store) Links(ctx context.Context, gameID string) ([]Link, error) {
	if err := game.Require(ctx, s.db, gameID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, game_id, from_id, to_id, relation, created_at FROM links WHERE game_id = ? ORDER BY id`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()
	links := []Link{}
	for rows.Next() {
		var l Link
		var created string
		if err := rows.Scan(&l.ID, &l.GameID, &l.FromID, &l.ToID, &l.Relation, &created); err != nil {
			return nil, fmt.Errorf("list links: %w", err)
		}
		if l.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list links: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

func (s *Store) AddLink(ctx context.Context, gameID string, in LinkInput) (Link, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Link{}, fmt.Errorf("add link: %w", err)
	}
	defer tx.Rollback()
	l, err := InsertLink(ctx, tx, gameID, in)
	if err != nil {
		return Link{}, err
	}
	return l, tx.Commit()
}

// InsertLink adds a link inside a caller's transaction. Both ends must be subjects of the game.
func InsertLink(ctx context.Context, tx *sql.Tx, gameID string, in LinkInput) (Link, error) {
	if err := in.validate(); err != nil {
		return Link{}, err
	}
	for _, id := range []string{in.FromID, in.ToID} {
		if err := Require(ctx, tx, gameID, id); err != nil {
			return Link{}, err
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Link{}, fmt.Errorf("add link: %w", err)
	}
	l := Link{ID: id.String(), GameID: gameID, FromID: in.FromID, ToID: in.ToID, Relation: in.Relation, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if _, err := tx.ExecContext(ctx, `INSERT INTO links (id, game_id, from_id, to_id, relation, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		l.ID, l.GameID, l.FromID, l.ToID, l.Relation, l.CreatedAt.Format(time.RFC3339)); err != nil {
		return Link{}, fmt.Errorf("add link: %w", err)
	}
	return l, nil
}

// UpdateLink replaces the ends and the relation of a link of the game.
func (s *Store) UpdateLink(ctx context.Context, gameID, id string, in LinkInput) error {
	if err := in.validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update link: %w", err)
	}
	defer tx.Rollback()
	for _, sid := range []string{in.FromID, in.ToID} {
		if err := Require(ctx, tx, gameID, sid); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE links SET from_id = ?, to_id = ?, relation = ? WHERE id = ? AND game_id = ?`, in.FromID, in.ToID, in.Relation, id, gameID)
	if err != nil {
		return fmt.Errorf("update link: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("update link: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: link %s in game %s", ErrNoLink, id, gameID)
	}
	return tx.Commit()
}

func (s *Store) DeleteLink(ctx context.Context, gameID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM links WHERE id = ? AND game_id = ?`, id, gameID)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("delete link: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: link %s in game %s", ErrNoLink, id, gameID)
	}
	return nil
}

// Require fails with ErrNotFound unless the subject exists in the game.
func Require(ctx context.Context, q game.Querier, gameID, id string) error {
	var exists int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM subjects WHERE id = ? AND game_id = ?`, id, gameID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: subject %s in game %s", ErrNotFound, id, gameID)
	}
	if err != nil {
		return fmt.Errorf("look up subject: %w", err)
	}
	return nil
}
