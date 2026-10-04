package play

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"aidventure/internal/entry"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

var ErrBadTransition = errors.New("invalid transition")

// Direction is where the play goes next, as the director decides it: on in
// the scene, or out of it by a cut, a split or a converge.
type Direction struct {
	Type entry.Type `json:"type"`
	// PlaceID or NewPlace is where a cut or split goes: a place that exists, or one made on the spot.
	PlaceID  string         `json:"placeId,omitempty"`
	NewPlace *subject.Input `json:"newPlace,omitempty"`
	// Cast is who is present after a cut, or who leaves in a split.
	Cast []string `json:"cast,omitempty"`
	// Link is how a new place stands to the one being left, in the author's words;
	// required with NewPlace whenever there is a scene to leave.
	Link string `json:"link,omitempty"`
	// IntoThreadID is the thread a converge walks into, at its current scene.
	IntoThreadID string `json:"intoThreadId,omitempty"`
	// ActorID is who acts first under the direction.
	ActorID string `json:"actorId,omitempty"`
	// Think is the director's reasoning, when a thinking model directed.
	Think string `json:"think,omitempty"`
}

// Transition is a direction out of the scene carried out: the entry that
// arrives, by an actor who is there after the move.
type Transition struct {
	Direction Direction       `json:"direction"`
	ActorID   string          `json:"actorId"`
	Think     string          `json:"think,omitempty"`
	Segments  []entry.Segment `json:"segments"`
}

// Result of an entry: the entry, the scene it is in, and that scene's thread,
// which a split or converge makes other than the one the play came from.
type Result struct {
	Entry    entry.Entry `json:"entry"`
	Scene    scene.Scene `json:"scene"`
	ThreadID string      `json:"threadId"`
}

// DoTransition carries out a cut, split or converge from the thread's current
// scene in one transaction.
func DoTransition(ctx context.Context, db *sql.DB, gameID, threadID string, t Transition) (Result, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("transition: %w", err)
	}
	defer tx.Rollback()
	if err := thread.RequireOpen(ctx, tx, gameID, threadID); err != nil {
		return Result{}, err
	}
	var res Result
	switch t.Direction.Type {
	case entry.Cut:
		res, err = cut(ctx, tx, gameID, threadID, t)
	case entry.Split:
		res, err = split(ctx, tx, gameID, threadID, t)
	case entry.Converge:
		res, err = converge(ctx, tx, gameID, threadID, t)
	default:
		err = fmt.Errorf("%w: %q does not leave the scene", ErrBadTransition, t.Direction.Type)
	}
	if err != nil {
		return Result{}, err
	}
	return res, tx.Commit()
}

// cut opens the thread's next scene: where, who is present, and the entry that opens it.
func cut(ctx context.Context, tx *sql.Tx, gameID, threadID string, t Transition) (Result, error) {
	if len(t.Direction.Cast) == 0 {
		return Result{}, fmt.Errorf("%w: cast is empty", ErrBadTransition)
	}
	// The first scene of a thread leaves nothing; later ones leave the current
	// place, and everyone in it exits.
	var left scene.Scene
	if sc, err := scene.Latest(ctx, tx, gameID, threadID); err == nil {
		left = sc
	} else if !errors.Is(err, scene.ErrNotFound) {
		return Result{}, err
	}
	placeID, err := place(ctx, tx, gameID, t.Direction, left.PlaceID)
	if err != nil {
		return Result{}, err
	}
	if len(left.Cast) > 0 {
		if _, err := entry.InsertPresence(ctx, tx, gameID, left.ID, entry.Exit, left.Cast); err != nil {
			return Result{}, err
		}
	}
	sc, e, err := open(ctx, tx, gameID, threadID, placeID, t.Direction.Cast, t, entry.Cut)
	if err != nil {
		return Result{}, err
	}
	return Result{Entry: e, Scene: sc, ThreadID: threadID}, nil
}

// split takes some of those present off to a scene of their own, in a new
// thread named after them; the rest stay.
func split(ctx context.Context, tx *sql.Tx, gameID, threadID string, t Transition) (Result, error) {
	from, err := current(ctx, tx, gameID, threadID)
	if err != nil {
		return Result{}, err
	}
	leavers := slices.Compact(slices.Sorted(slices.Values(t.Direction.Cast)))
	if len(leavers) == 0 {
		return Result{}, fmt.Errorf("%w: nobody leaves", ErrBadTransition)
	}
	for _, id := range leavers {
		if !slices.Contains(from.Cast, id) {
			return Result{}, fmt.Errorf("%w: %s is not present to leave", ErrBadTransition, id)
		}
	}
	tracked, err := trackedAmong(ctx, tx, gameID, from.Cast)
	if err != nil {
		return Result{}, err
	}
	var leaving, staying int
	for _, id := range tracked {
		if slices.Contains(leavers, id) {
			leaving++
		} else {
			staying++
		}
	}
	switch {
	case len(tracked) == 0:
		return Result{}, fmt.Errorf("%w: a split follows tracked actors, and none is present in scene %s", ErrBadTransition, from.ID)
	case leaving == 0:
		return Result{}, fmt.Errorf("%w: no tracked actor leaves; a split follows tracked actors who part", ErrBadTransition)
	case staying == 0:
		return Result{}, fmt.Errorf("%w: every tracked actor leaves; cut instead", ErrBadTransition)
	}
	placeID, err := place(ctx, tx, gameID, t.Direction, from.PlaceID)
	if err != nil {
		return Result{}, err
	}
	who, err := names(ctx, tx, gameID, leavers)
	if err != nil {
		return Result{}, err
	}
	th, err := thread.Insert(ctx, tx, gameID, strings.Join(who, ", "), from.ID)
	if err != nil {
		return Result{}, err
	}
	sc, e, err := open(ctx, tx, gameID, th.ID, placeID, leavers, t, entry.Split)
	if err != nil {
		return Result{}, err
	}
	if _, err := entry.InsertPresence(ctx, tx, gameID, from.ID, entry.Exit, leavers); err != nil {
		return Result{}, err
	}
	return Result{Entry: e, Scene: sc, ThreadID: th.ID}, nil
}

// converge walks the thread's cast into another thread's current scene and
// ends the thread. A game looking at the ended thread looks at the joined one.
func converge(ctx context.Context, tx *sql.Tx, gameID, threadID string, t Transition) (Result, error) {
	into := t.Direction.IntoThreadID
	if into == "" || into == threadID {
		return Result{}, fmt.Errorf("%w: converge needs another thread", ErrBadTransition)
	}
	if err := thread.RequireOpen(ctx, tx, gameID, into); err != nil {
		return Result{}, err
	}
	from, err := current(ctx, tx, gameID, threadID)
	if err != nil {
		return Result{}, err
	}
	if len(from.Cast) == 0 {
		return Result{}, fmt.Errorf("%w: scene %s has nobody to converge", ErrNoCast, from.ID)
	}
	to, err := current(ctx, tx, gameID, into)
	if err != nil {
		return Result{}, err
	}
	// No exit is written where they came from: ending the thread is the exit,
	// and a thread reopened by its destination's deletion still has them.
	if _, err := entry.InsertPresence(ctx, tx, gameID, to.ID, entry.Enter, from.Cast); err != nil {
		return Result{}, err
	}
	for _, id := range from.Cast {
		if !slices.Contains(to.Cast, id) {
			to.Cast = append(to.Cast, id)
		}
	}
	e, err := entry.Insert(ctx, tx, gameID, to.ID, t.ActorID, t.Think, entry.Converge, t.Segments)
	if err != nil {
		return Result{}, err
	}
	if err := thread.End(ctx, tx, gameID, threadID, e.ID); err != nil {
		return Result{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE games SET current_thread_id = ? WHERE id = ? AND current_thread_id = ?`, into, gameID, threadID); err != nil {
		return Result{}, fmt.Errorf("converge: %w", err)
	}
	return Result{Entry: e, Scene: to, ThreadID: into}, nil
}

// open starts a scene: who is there as it opens, then the beat that opens it.
func open(ctx context.Context, tx *sql.Tx, gameID, threadID, placeID string, cast []string, t Transition, typ entry.Type) (scene.Scene, entry.Entry, error) {
	sc, err := scene.Insert(ctx, tx, gameID, threadID, placeID)
	if err != nil {
		return scene.Scene{}, entry.Entry{}, err
	}
	if _, err := entry.InsertPresence(ctx, tx, gameID, sc.ID, entry.Present, cast); err != nil {
		return scene.Scene{}, entry.Entry{}, err
	}
	sc.Cast = cast
	e, err := entry.Insert(ctx, tx, gameID, sc.ID, t.ActorID, t.Think, typ, t.Segments)
	if err != nil {
		return scene.Scene{}, entry.Entry{}, err
	}
	if err := scene.SetOpening(ctx, tx, sc.ID, e.ID); err != nil {
		return scene.Scene{}, entry.Entry{}, err
	}
	sc.Opening = e.ID
	return sc, e, nil
}

// place resolves where a cut or split goes, making the place when it is new
// and linking it to the place left behind, so a new place is never adrift.
func place(ctx context.Context, tx *sql.Tx, gameID string, d Direction, from string) (string, error) {
	if (d.PlaceID == "") == (d.NewPlace == nil) {
		return "", fmt.Errorf("%w: give either placeId or newPlace", ErrBadTransition)
	}
	if d.PlaceID != "" {
		return d.PlaceID, nil
	}
	in := *d.NewPlace
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return "", fmt.Errorf("%w: new place needs a name", ErrBadTransition)
	}
	in.Scene = true // a place made for a scene hosts scenes by definition
	if from != "" && strings.TrimSpace(d.Link) == "" {
		return "", fmt.Errorf("%w: new place needs its link to the place left", ErrBadTransition)
	}
	made, err := subject.Insert(ctx, tx, gameID, in)
	if err != nil {
		return "", err
	}
	if from != "" {
		if _, err := subject.InsertLink(ctx, tx, gameID, subject.LinkInput{FromID: made.ID, ToID: from, Relation: d.Link}); err != nil {
			return "", err
		}
	}
	return made.ID, nil
}

// current is the thread's scene in play; a thread without one has nothing to leave.
func current(ctx context.Context, tx *sql.Tx, gameID, threadID string) (scene.Scene, error) {
	sc, err := scene.Latest(ctx, tx, gameID, threadID)
	if errors.Is(err, scene.ErrNotFound) {
		return scene.Scene{}, ErrNoScene
	}
	return sc, err
}

// trackedAmong is which of the subjects the story follows.
func trackedAmong(ctx context.Context, tx *sql.Tx, gameID string, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		var tracked bool
		if err := tx.QueryRowContext(ctx, `SELECT tracked FROM subjects WHERE id = ? AND game_id = ?`, id, gameID).Scan(&tracked); err != nil {
			return nil, fmt.Errorf("look up tracked: %w", err)
		}
		if tracked {
			out = append(out, id)
		}
	}
	return out, nil
}

func names(ctx context.Context, tx *sql.Tx, gameID string, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		var name string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM subjects WHERE id = ? AND game_id = ?`, id, gameID).Scan(&name); err != nil {
			return nil, fmt.Errorf("look up name: %w", err)
		}
		out = append(out, name)
	}
	return out, nil
}
