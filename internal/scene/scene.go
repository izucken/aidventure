// Package scene holds the bounded situations a thread moves through. A scene
// is hosted by a subject flagged for it, the place, and opens with an entry.
// Who is present is read off its entries: a present entry at the head, then
// enters and exits. Scenes are sequential within a thread: the latest one is
// where play currently is.
package scene

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"aidventure/internal/game"
	"aidventure/internal/thread"
)

var (
	ErrNotFound = errors.New("scene not found")
	ErrNotScene = errors.New("subject cannot host a scene")
)

type Scene struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	PlaceID  string `json:"placeId"`
	// Title is the author's name for the scene, if any; the place stays the place.
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Opening is the entry that cut to this scene; empty for scenes made before cuts carried entries.
	Opening string `json:"openingEntryId,omitempty"`
	// Cast is who is present now, as subject ids: the fold of the scene's presence entries.
	Cast []string `json:"cast"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Insert creates an empty scene inside a caller's transaction; whoever is
// present comes as its first entry. The thread must exist in the game and
// the place must be a scene-flagged subject of it.
func Insert(ctx context.Context, tx *sql.Tx, gameID, threadID, placeID string) (Scene, error) {
	if err := thread.Require(ctx, tx, gameID, threadID); err != nil {
		return Scene{}, err
	}
	if err := RequirePlace(ctx, tx, gameID, placeID); err != nil {
		return Scene{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Scene{}, fmt.Errorf("cut scene: %w", err)
	}
	sc := Scene{ID: id.String(), ThreadID: threadID, PlaceID: placeID, CreatedAt: time.Now().UTC().Truncate(time.Second), Cast: []string{}}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO scenes (id, thread_id, place_id, created_at) VALUES (?, ?, ?, ?)`,
		sc.ID, sc.ThreadID, sc.PlaceID, sc.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Scene{}, fmt.Errorf("cut scene: %w", err)
	}
	return sc, nil
}

// SetOpening records the entry a scene opened with.
func SetOpening(ctx context.Context, tx *sql.Tx, sceneID, entryID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE scenes SET opening_entry_id = ? WHERE id = ?`, entryID, sceneID); err != nil {
		return fmt.Errorf("cut scene: %w", err)
	}
	return nil
}

// RequirePlace fails unless placeID is a scene-flagged subject of the game.
func RequirePlace(ctx context.Context, q game.Querier, gameID, placeID string) error {
	var canHost bool
	err := q.QueryRowContext(ctx, `SELECT scene FROM subjects WHERE id = ? AND game_id = ?`, placeID, gameID).Scan(&canHost)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: subject %s in game %s", ErrNoPlace, placeID, gameID)
	}
	if err != nil {
		return fmt.Errorf("look up place: %w", err)
	}
	if !canHost {
		return fmt.Errorf("%w: subject %s is not flagged as a scene", ErrNotScene, placeID)
	}
	return nil
}

// ErrNoPlace is a cut to a subject that does not exist in the game.
var ErrNoPlace = errors.New("place not found")

// Retitle sets the scene's title; empty takes it away.
func (s *Store) Retitle(ctx context.Context, gameID, threadID, id, title string) error {
	if err := Require(ctx, s.db, gameID, threadID, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE scenes SET title = ? WHERE id = ?`, title, id); err != nil {
		return fmt.Errorf("retitle scene: %w", err)
	}
	return nil
}

// Delete removes a scene with everything in it: cast, entries and their segments.
func (s *Store) Delete(ctx context.Context, gameID, threadID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete scene: %w", err)
	}
	defer tx.Rollback()
	if err := Require(ctx, tx, gameID, threadID, id); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE scenes SET opening_entry_id = NULL WHERE id = ?`,
		`DELETE FROM segments WHERE entry_id IN (SELECT id FROM entries WHERE scene_id = ?)`,
		`DELETE FROM entry_actors WHERE entry_id IN (SELECT id FROM entries WHERE scene_id = ?)`,
		`DELETE FROM entries WHERE scene_id = ?`,
		`DELETE FROM scenes WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("delete scene: %w", err)
		}
	}
	return tx.Commit()
}

// List returns the thread's scenes in order, with who is present now; the last one is current.
func (s *Store) List(ctx context.Context, gameID, threadID string) ([]Scene, error) {
	if err := thread.Require(ctx, s.db, gameID, threadID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, thread_id, place_id, title, created_at, COALESCE(opening_entry_id, '')
		FROM scenes WHERE thread_id = ? ORDER BY id`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list scenes: %w", err)
	}
	defer rows.Close()
	scenes := []Scene{}
	for rows.Next() {
		var sc Scene
		var created string
		if err := rows.Scan(&sc.ID, &sc.ThreadID, &sc.PlaceID, &sc.Title, &created, &sc.Opening); err != nil {
			return nil, fmt.Errorf("list scenes: %w", err)
		}
		if sc.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("list scenes: %w", err)
		}
		scenes = append(scenes, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range scenes {
		if scenes[i].Cast, err = Cast(ctx, s.db, scenes[i].ID, 0); err != nil {
			return nil, err
		}
	}
	return scenes, nil
}

// Cast is who is present in the scene up to and including the entry at seq,
// or now when seq is 0: the present entry at the head, then every enter and
// exit, folded. Order is first appearance.
func Cast(ctx context.Context, q Reader, sceneID string, seq int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT e.type, a.subject_id FROM entries e JOIN entry_actors a ON a.entry_id = e.id
		WHERE e.scene_id = ? AND (? = 0 OR e.seq <= ?) ORDER BY e.seq, a.subject_id`, sceneID, seq, seq)
	if err != nil {
		return nil, fmt.Errorf("read cast: %w", err)
	}
	defer rows.Close()
	cast := []string{}
	for rows.Next() {
		var typ, id string
		if err := rows.Scan(&typ, &id); err != nil {
			return nil, fmt.Errorf("read cast: %w", err)
		}
		if typ == "exit" {
			cast = slices.DeleteFunc(cast, func(c string) bool { return c == id })
		} else if !slices.Contains(cast, id) {
			cast = append(cast, id)
		}
	}
	return cast, rows.Err()
}

// Require fails unless the scene exists on the thread in the game.
func Require(ctx context.Context, q game.Querier, gameID, threadID, sceneID string) error {
	if err := thread.Require(ctx, q, gameID, threadID); err != nil {
		return err
	}
	var exists int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM scenes WHERE id = ? AND thread_id = ?`, sceneID, threadID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: scene %s in thread %s", ErrNotFound, sceneID, threadID)
	}
	if err != nil {
		return fmt.Errorf("look up scene: %w", err)
	}
	return nil
}

// Latest returns the thread's current scene, the last one, with its cast.
// ErrNotFound when the thread has none yet.
func Latest(ctx context.Context, q Reader, gameID, threadID string) (Scene, error) {
	if err := thread.Require(ctx, q, gameID, threadID); err != nil {
		return Scene{}, err
	}
	var sc Scene
	var created string
	err := q.QueryRowContext(ctx, `
		SELECT id, thread_id, place_id, title, created_at, COALESCE(opening_entry_id, '')
		FROM scenes WHERE thread_id = ? ORDER BY id DESC LIMIT 1`, threadID).Scan(&sc.ID, &sc.ThreadID, &sc.PlaceID, &sc.Title, &created, &sc.Opening)
	if errors.Is(err, sql.ErrNoRows) {
		return Scene{}, fmt.Errorf("%w: thread %s has no scene yet", ErrNotFound, threadID)
	}
	if err != nil {
		return Scene{}, fmt.Errorf("latest scene: %w", err)
	}
	if sc.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return Scene{}, fmt.Errorf("latest scene: %w", err)
	}
	if sc.Cast, err = Cast(ctx, q, sc.ID, 0); err != nil {
		return Scene{}, err
	}
	return sc, nil
}

// Reader is the slice of *sql.DB and *sql.Tx that reads need.
type Reader interface {
	game.Querier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}
