// Package thread holds a line of play inside a game: an ordered run of scenes
// following a set of characters. Threads split and join as characters part and
// meet, so a game has any number of them. A thread that joined another has
// ended: it is history, and the play goes on in the thread it joined.
package thread

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"aidventure/internal/game"
)

var (
	ErrNotFound = errors.New("thread not found")
	ErrEnded    = errors.New("thread has ended")
)

type Thread struct {
	ID        string    `json:"id"`
	GameID    string    `json:"gameId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// ParentSceneID is the scene a split took this thread's cast from; empty for a thread started by hand.
	ParentSceneID string `json:"parentSceneId,omitempty"`
	// EndedByEntryID is the converge entry, in another thread, where this thread's cast walked in.
	EndedByEntryID string `json:"endedByEntryId,omitempty"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, gameID, name string) (Thread, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Thread{}, fmt.Errorf("create thread: %w", err)
	}
	defer tx.Rollback()
	t, err := Insert(ctx, tx, gameID, name, "")
	if err != nil {
		return Thread{}, err
	}
	return t, tx.Commit()
}

// Insert creates a thread inside a caller's transaction; parentSceneID is the
// scene a split takes it from, or empty.
func Insert(ctx context.Context, tx *sql.Tx, gameID, name, parentSceneID string) (Thread, error) {
	if err := game.Require(ctx, tx, gameID); err != nil {
		return Thread{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Thread{}, fmt.Errorf("create thread: %w", err)
	}
	t := Thread{ID: id.String(), GameID: gameID, Name: name, CreatedAt: time.Now().UTC().Truncate(time.Second), ParentSceneID: parentSceneID}
	var parent *string
	if parentSceneID != "" {
		parent = &parentSceneID
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO threads (id, game_id, name, created_at, parent_scene_id) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.GameID, t.Name, t.CreatedAt.Format(time.RFC3339), parent)
	if err != nil {
		return Thread{}, fmt.Errorf("create thread: %w", err)
	}
	return t, nil
}

func (s *Store) List(ctx context.Context, gameID string) ([]Thread, error) {
	if err := game.Require(ctx, s.db, gameID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, game_id, name, created_at, COALESCE(parent_scene_id, ''), COALESCE(ended_by_entry_id, '')
		FROM threads WHERE game_id = ? ORDER BY id`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	threads := []Thread{}
	for rows.Next() {
		var t Thread
		var created string
		if err := rows.Scan(&t.ID, &t.GameID, &t.Name, &created, &t.ParentSceneID, &t.EndedByEntryID); err != nil {
			return nil, fmt.Errorf("list threads: %w", err)
		}
		if t.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list threads: %w", err)
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

// Rename changes what the thread is called.
func (s *Store) Rename(ctx context.Context, gameID, id, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE threads SET name = ? WHERE id = ? AND game_id = ?`, name, id, gameID)
	if err != nil {
		return fmt.Errorf("rename thread: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("rename thread: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: thread %s in game %s", ErrNotFound, id, gameID)
	}
	return nil
}

// Delete removes a thread with everything in it: scenes, their casts, entries
// and segments. Threads split off it lose their parent; threads that converged
// into it are open again, at the scene they left from; a game looking at it
// looks at nothing.
func (s *Store) Delete(ctx context.Context, gameID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete thread: %w", err)
	}
	defer tx.Rollback()
	if err := Require(ctx, tx, gameID, id); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE threads SET parent_scene_id = NULL WHERE parent_scene_id IN (SELECT id FROM scenes WHERE thread_id = ?)`,
		`UPDATE threads SET ended_by_entry_id = NULL WHERE ended_by_entry_id IN (SELECT e.id FROM entries e JOIN scenes s ON s.id = e.scene_id WHERE s.thread_id = ?)`,
		`UPDATE games SET current_thread_id = NULL WHERE current_thread_id = ?`,
		`UPDATE scenes SET opening_entry_id = NULL WHERE thread_id = ?`,
		`DELETE FROM segments WHERE entry_id IN (SELECT e.id FROM entries e JOIN scenes s ON s.id = e.scene_id WHERE s.thread_id = ?)`,
		`DELETE FROM entries WHERE scene_id IN (SELECT id FROM scenes WHERE thread_id = ?)`,
		`DELETE FROM entry_actors WHERE entry_id IN (SELECT e.id FROM entries e JOIN scenes s ON s.id = e.scene_id WHERE s.thread_id = ?)`,
		`DELETE FROM scenes WHERE thread_id = ?`,
		`DELETE FROM threads WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("delete thread: %w", err)
		}
	}
	return tx.Commit()
}

// Current returns the id of the game's selected thread, or "" when none is selected.
func (s *Store) Current(ctx context.Context, gameID string) (string, error) {
	var id sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT current_thread_id FROM games WHERE id = ?`, gameID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: game %s", game.ErrNotFound, gameID)
	}
	if err != nil {
		return "", fmt.Errorf("current thread: %w", err)
	}
	return id.String, nil
}

func (s *Store) SetCurrent(ctx context.Context, gameID, threadID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("select thread: %w", err)
	}
	defer tx.Rollback()
	if err := Require(ctx, tx, gameID, threadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE games SET current_thread_id = ? WHERE id = ?`, threadID, gameID); err != nil {
		return fmt.Errorf("select thread: %w", err)
	}
	return tx.Commit()
}

// End closes a thread inside a caller's transaction: its cast walked into
// another thread at entryID.
func End(ctx context.Context, tx *sql.Tx, gameID, id, entryID string) error {
	if err := RequireOpen(ctx, tx, gameID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET ended_by_entry_id = ? WHERE id = ?`, entryID, id); err != nil {
		return fmt.Errorf("end thread: %w", err)
	}
	return nil
}

// Require fails with game.ErrNotFound or ErrNotFound unless the thread exists
// in the game. Stores that own per-thread rows call it.
func Require(ctx context.Context, q game.Querier, gameID, threadID string) error {
	_, err := lookup(ctx, q, gameID, threadID)
	return err
}

// RequireOpen is Require for play: an ended thread fails with ErrEnded.
func RequireOpen(ctx context.Context, q game.Querier, gameID, threadID string) error {
	ended, err := lookup(ctx, q, gameID, threadID)
	if err != nil {
		return err
	}
	if ended {
		return fmt.Errorf("%w: thread %s", ErrEnded, threadID)
	}
	return nil
}

func lookup(ctx context.Context, q game.Querier, gameID, threadID string) (ended bool, err error) {
	if err := game.Require(ctx, q, gameID); err != nil {
		return false, err
	}
	err = q.QueryRowContext(ctx, `SELECT ended_by_entry_id IS NOT NULL FROM threads WHERE id = ? AND game_id = ?`, threadID, gameID).Scan(&ended)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: thread %s in game %s", ErrNotFound, threadID, gameID)
	}
	if err != nil {
		return false, fmt.Errorf("look up thread: %w", err)
	}
	return ended, nil
}
