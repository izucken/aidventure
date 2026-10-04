package play

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aidventure/internal/entry"
	"aidventure/internal/thread"
)

var (
	ErrNoScene = errors.New("thread has no scene")
	ErrNoCast  = errors.New("scene has nobody present")
	ErrNoTurn  = errors.New("no turn is open")
)

// Turn is a direction nothing has followed yet: whose entry the thread
// awaits, and under what.
type Turn struct {
	EntryID string `json:"entryId"`
	ActorID string `json:"actorId"`
	// User says a human answers for the actor; the UI hands over, the model stays out.
	User bool `json:"user"`
	// Think is the director's reasoning behind the turn, when a thinking model directed.
	Think     string     `json:"think,omitempty"`
	Direction *Direction `json:"direction,omitempty"`
}

// Outcome of one advance: the direction recorded, if this advance asked for
// one, then either a turn now waiting on a user or the entry a model actor
// wrote and where it landed.
type Outcome struct {
	Directed *entry.Entry `json:"directed,omitempty"`
	Turn     *Turn        `json:"turn,omitempty"`
	*Result
}

// Advance moves the thread by one turn. A direction nothing has followed is
// taken up: a model actor's entry is written at once, a user-controlled
// actor's turn is handed back. Without one, the director decides what happens
// next and who acts first, the decision is recorded, and it is taken up the same way.
func (r *Roller) Advance(ctx context.Context, gameID, threadID string, sink Sink) (Outcome, error) {
	if err := thread.RequireOpen(ctx, r.DB, gameID, threadID); err != nil {
		return Outcome{}, err
	}
	st, err := r.stage(ctx, gameID, threadID)
	if err != nil {
		return Outcome{}, err
	}
	var out Outcome
	open, dir, ok, err := st.open()
	if err != nil {
		return Outcome{}, err
	}
	if !ok {
		if len(st.present()) == 0 {
			return Outcome{}, fmt.Errorf("%w: scene %s", ErrNoCast, st.scene.ID)
		}
		if dir, err = r.direct(ctx, st, sink); err != nil {
			return Outcome{}, err
		}
		if open, err = r.record(ctx, gameID, threadID, st.scene.ID, dir); err != nil {
			return Outcome{}, err
		}
		out.Directed = &open
	}
	if st.byID[dir.ActorID].User {
		out.Turn = st.turn(open, dir)
		return out, nil
	}
	res, err := r.perform(ctx, gameID, threadID, st.scene.ID, dir.ActorID, dir, sink)
	if err != nil {
		return Outcome{}, err
	}
	out.Result = &res
	return out, nil
}

// record writes the director's decision as an entry. The think goes on the
// entry, as every entry's does, not inside the decision.
func (r *Roller) record(ctx context.Context, gameID, threadID, sceneID string, d Direction) (entry.Entry, error) {
	think := d.Think
	d.Think = ""
	raw, err := json.Marshal(d)
	if err != nil {
		return entry.Entry{}, fmt.Errorf("record direction: %w", err)
	}
	return r.Entries.Direct(ctx, gameID, threadID, sceneID, think, raw)
}

// open is the direction at the end of the scene that nothing has followed,
// read back, when there is one.
func (st stage) open() (entry.Entry, Direction, bool, error) {
	if len(st.entries) == 0 {
		return entry.Entry{}, Direction{}, false, nil
	}
	last := st.entries[len(st.entries)-1]
	if last.Type != entry.Direction {
		return entry.Entry{}, Direction{}, false, nil
	}
	var d Direction
	if err := json.Unmarshal(last.Direction, &d); err != nil {
		return entry.Entry{}, Direction{}, false, fmt.Errorf("read direction of entry %s: %w", last.ID, err)
	}
	return last, d, true, nil
}

func (st stage) turn(e entry.Entry, d Direction) *Turn {
	return &Turn{EntryID: e.ID, ActorID: d.ActorID, User: st.byID[d.ActorID].User, Think: e.Think, Direction: &d}
}

// Turn reports the open turn, or nil.
func (r *Roller) Turn(ctx context.Context, gameID, threadID string) (*Turn, error) {
	st, err := r.stage(ctx, gameID, threadID)
	if errors.Is(err, ErrNoScene) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e, d, ok, err := st.open()
	if err != nil || !ok {
		return nil, err
	}
	return st.turn(e, d), nil
}

// Pass drops the open turn: the direction was not taken up, and goes.
func (r *Roller) Pass(ctx context.Context, gameID, threadID string) error {
	st, err := r.stage(ctx, gameID, threadID)
	if err != nil {
		return err
	}
	e, _, ok, err := st.open()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: thread %s", ErrNoTurn, threadID)
	}
	return r.Entries.Delete(ctx, gameID, threadID, st.scene.ID, e.ID)
}

func alternatives(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(n) + `"`
	}
	return strings.Join(quoted, " | ")
}
