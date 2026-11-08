// Package entry holds what happened in a thread, in order. Most entries are
// beats: one actor's contribution to a scene, as an ordered run of typed
// segments, from that actor's point of view. A direction is an entry too: the
// director's decision about what happens next, recorded where it was made.
// So are comings and goings: who is there as a scene opens, who enters, who
// exits, which is what presence in a scene is read from.
package entry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

var (
	ErrNotFound = errors.New("entry not found")
	// ErrOpening is an attempt to delete the entry a scene opened with, or its head; delete the scene instead.
	ErrOpening = errors.New("entry opens its scene")
	// ErrEnding is an attempt to delete the entry another thread converged on.
	ErrEnding   = errors.New("entry ends a thread")
	ErrNotActor = errors.New("subject cannot take turns")
	// ErrNotPresent is an actor acting in a scene it is not cast in.
	ErrNotPresent = errors.New("actor is not present in the scene")
	ErrInvalid    = errors.New("invalid entry")
	// ErrInUse is an attempt to delete an enter line that a later beat relies on.
	ErrInUse = errors.New("entry is relied on")
)

// Type is what an entry is. A beat moves the play: on in its scene, or into
// another scene or thread, where a cut or split entry opens the scene it is
// in and a converge entry is where another thread's cast walked into this
// one. A direction is the director's decision that the next beat carries out.
// Present, at the head of a scene, is who is there as it opens; enter and
// exit are who came and went since.
type Type string

const (
	Narrative Type = "narrative"
	Cut       Type = "cut"
	Split     Type = "split"
	Converge  Type = "converge"
	Direction Type = "direction"
	Present   Type = "present"
	Enter     Type = "enter"
	Exit      Type = "exit"
)

func (t Type) beat() bool {
	switch t {
	case Narrative, Cut, Split, Converge:
		return true
	}
	return false
}

func (t Type) presence() bool {
	switch t {
	case Present, Enter, Exit:
		return true
	}
	return false
}

// Kind is what a segment is: the typing that loose text markup gets wrong.
type Kind string

const (
	Narration Kind = "narration"
	Action    Kind = "action"
	Speech    Kind = "speech"
	Thought   Kind = "thought"
)

func (k Kind) valid() bool {
	switch k {
	case Narration, Action, Speech, Thought:
		return true
	}
	return false
}

type Segment struct {
	Kind Kind   `json:"kind"`
	Text string `json:"text"`
	// To is the subject addressed, when speech has a clear addressee.
	To *string `json:"to,omitempty"`
}

type Entry struct {
	ID      string `json:"id"`
	SceneID string `json:"sceneId"`
	// ActorID is who wrote a beat; a direction has none, the director being no subject.
	ActorID   string    `json:"actorId,omitempty"`
	Seq       int       `json:"seq"`
	Type      Type      `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
	// Think is the reasoning behind a model-written entry. Kept for the author, never rendered into a prompt.
	Think string `json:"think,omitempty"`
	// Direction is the decision a direction entry records, in the form play gives it; opaque here.
	Direction json.RawMessage `json:"direction,omitempty"`
	// Actors is who a present, enter or exit entry is about.
	Actors   []string  `json:"actors,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Append adds a narrative beat at the end of the scene.
func (s *Store) Append(ctx context.Context, gameID, threadID, sceneID, actorID, think string, segments []Segment) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("append entry: %w", err)
	}
	defer tx.Rollback()
	if err := scene.Require(ctx, tx, gameID, threadID, sceneID); err != nil {
		return Entry{}, err
	}
	e, err := Insert(ctx, tx, gameID, sceneID, actorID, think, Narrative, segments)
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// AppendAfter adds a narrative beat right after another entry of the scene;
// what followed moves down one.
func (s *Store) AppendAfter(ctx context.Context, gameID, threadID, sceneID, afterID, actorID, think string, segments []Segment) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("insert entry: %w", err)
	}
	defer tx.Rollback()
	if err := scene.Require(ctx, tx, gameID, threadID, sceneID); err != nil {
		return Entry{}, err
	}
	at, err := shiftAfter(ctx, tx, sceneID, afterID)
	if err != nil {
		return Entry{}, err
	}
	if err := validate(segments); err != nil {
		return Entry{}, err
	}
	if err := requireCast(ctx, tx, gameID, actorID, segments); err != nil {
		return Entry{}, err
	}
	if err := requirePresent(ctx, tx, sceneID, actorID, at); err != nil {
		return Entry{}, err
	}
	e := Entry{SceneID: sceneID, ActorID: actorID, Seq: at + 1, Type: Narrative, Think: think, Segments: segments}
	if err := insert(ctx, tx, &e); err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// shiftAfter makes room right after an entry of the scene: what followed moves
// down one, and the entry's seq comes back for the newcomer to sit at plus one.
func shiftAfter(ctx context.Context, tx *sql.Tx, sceneID, afterID string) (int, error) {
	var at int
	err := tx.QueryRowContext(ctx, `SELECT seq FROM entries WHERE id = ? AND scene_id = ?`, afterID, sceneID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: entry %s in scene %s, to insert after", ErrNotFound, afterID, sceneID)
	}
	if err != nil {
		return 0, fmt.Errorf("insert entry: %w", err)
	}
	// Two steps, through negatives, since seq is unique per scene and a plain shift collides with itself.
	for _, q := range []string{
		`UPDATE entries SET seq = -seq WHERE scene_id = ? AND seq > ?`,
		`UPDATE entries SET seq = -seq + 1 WHERE scene_id = ? AND seq < 0 AND -seq > ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, sceneID, at); err != nil {
			return 0, fmt.Errorf("insert entry: %w", err)
		}
	}
	return at, nil
}

// Presence records who entered or exited, or who is there as the scene opens:
// at the end of the scene, or right after the entry afterID. Staging has to
// agree with the script at that point: nobody enters who is there, nobody
// exits who is not.
func (s *Store) Presence(ctx context.Context, gameID, threadID, sceneID string, typ Type, actors []string, afterID string) (Entry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("record presence: %w", err)
	}
	defer tx.Rollback()
	if err := scene.Require(ctx, tx, gameID, threadID, sceneID); err != nil {
		return Entry{}, err
	}
	// at is the point staging is read at and seq the place the line takes; both are the end unless placed.
	at, seq := 0, 0
	if afterID != "" {
		if at, err = shiftAfter(ctx, tx, sceneID, afterID); err != nil {
			return Entry{}, err
		}
		seq = at + 1
	}
	if typ != Present {
		cast, err := scene.Cast(ctx, tx, sceneID, at)
		if err != nil {
			return Entry{}, err
		}
		for _, id := range actors {
			if there := slices.Contains(cast, id); there == (typ == Enter) {
				name, err := nameOf(ctx, tx, gameID, id)
				if err != nil {
					return Entry{}, err
				}
				return Entry{}, fmt.Errorf("%w: %s %s at that point, so cannot %s", ErrInvalid, name, map[bool]string{true: "is there", false: "is not there"}[there], typ)
			}
		}
	}
	e, err := insertPresence(ctx, tx, gameID, sceneID, typ, actors, seq)
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// InsertPresence adds a present, enter or exit entry at the end of a scene
// inside a caller's transaction. Present is the head of a scene and goes
// first or not at all; enter and exit name at least one actor.
func InsertPresence(ctx context.Context, tx *sql.Tx, gameID, sceneID string, typ Type, actors []string) (Entry, error) {
	return insertPresence(ctx, tx, gameID, sceneID, typ, actors, 0)
}

// insertPresence places the line at seq, or at the end when seq is 0.
func insertPresence(ctx context.Context, tx *sql.Tx, gameID, sceneID string, typ Type, actors []string, seq int) (Entry, error) {
	if !typ.presence() {
		return Entry{}, fmt.Errorf("%w: %q is not a presence", ErrInvalid, typ)
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE scene_id = ?`, sceneID).Scan(&n); err != nil {
		return Entry{}, fmt.Errorf("record presence: %w", err)
	}
	if (typ == Present) != (n == 0) {
		return Entry{}, fmt.Errorf("%w: scene %s %s", ErrInvalid, sceneID, map[bool]string{true: "already has its head", false: "has no head yet; present comes first"}[typ == Present])
	}
	if typ != Present && len(actors) == 0 {
		return Entry{}, fmt.Errorf("%w: %s names nobody", ErrInvalid, typ)
	}
	for _, id := range actors {
		if err := requireActor(ctx, tx, gameID, id); err != nil {
			return Entry{}, err
		}
	}
	e := Entry{SceneID: sceneID, Type: typ, Actors: actors, Seq: seq}
	if err := insert(ctx, tx, &e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// nameOf is what the author calls a subject, for errors.
func nameOf(ctx context.Context, q querier, gameID, id string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx, `SELECT name FROM subjects WHERE id = ? AND game_id = ?`, id, gameID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: subject %s in game %s", subject.ErrNotFound, id, gameID)
	}
	if err != nil {
		return "", fmt.Errorf("look up subject: %w", err)
	}
	return name, nil
}

// Direct records the director's decision at the end of the scene.
func (s *Store) Direct(ctx context.Context, gameID, threadID, sceneID, think string, direction json.RawMessage) (Entry, error) {
	if !json.Valid(direction) {
		return Entry{}, fmt.Errorf("%w: direction is not JSON", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("direct: %w", err)
	}
	defer tx.Rollback()
	if err := scene.Require(ctx, tx, gameID, threadID, sceneID); err != nil {
		return Entry{}, err
	}
	e := Entry{SceneID: sceneID, Type: Direction, Think: think, Direction: direction}
	if err := insert(ctx, tx, &e); err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

// Insert adds a beat at the end of a scene inside a caller's transaction.
// The scene is taken as given.
func Insert(ctx context.Context, tx *sql.Tx, gameID, sceneID, actorID, think string, typ Type, segments []Segment) (Entry, error) {
	if !typ.beat() {
		return Entry{}, fmt.Errorf("%w: %q is not a beat", ErrInvalid, typ)
	}
	if err := validate(segments); err != nil {
		return Entry{}, err
	}
	if err := requireCast(ctx, tx, gameID, actorID, segments); err != nil {
		return Entry{}, err
	}
	if err := requirePresent(ctx, tx, sceneID, actorID, 0); err != nil {
		return Entry{}, err
	}
	e := Entry{SceneID: sceneID, ActorID: actorID, Type: typ, Think: think, Segments: segments}
	if err := insert(ctx, tx, &e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// insert writes an entry of any type, filling in id and time, and seq at the
// end of its scene unless the caller placed it.
func insert(ctx context.Context, tx *sql.Tx, e *Entry) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("append entry: %w", err)
	}
	e.ID, e.CreatedAt = id.String(), time.Now().UTC().Truncate(time.Second)
	if e.Seq == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM entries WHERE scene_id = ?`, e.SceneID).Scan(&e.Seq); err != nil {
			return fmt.Errorf("append entry: %w", err)
		}
	}
	var actor, direction *string
	if e.ActorID != "" {
		actor = &e.ActorID
	}
	if e.Direction != nil {
		d := string(e.Direction)
		direction = &d
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO entries (id, scene_id, actor_id, seq, type, think, direction, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.SceneID, actor, e.Seq, e.Type, e.Think, direction, e.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("append entry: %w", err)
	}
	if err := insertSegments(ctx, tx, e.ID, e.Segments); err != nil {
		return fmt.Errorf("append entry: %w", err)
	}
	for _, id := range e.Actors {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO entry_actors (entry_id, subject_id) VALUES (?, ?)`, e.ID, id); err != nil {
			return fmt.Errorf("append entry: %w", err)
		}
	}
	return nil
}

// Update replaces the actor and segments of a beat; its place in the scene
// and its type stay. A direction is not edited: pass it and direct again.
func (s *Store) Update(ctx context.Context, gameID, threadID, sceneID, id, actorID string, segments []Segment) error {
	if err := validate(segments); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	defer tx.Rollback()
	if err := scene.Require(ctx, tx, gameID, threadID, sceneID); err != nil {
		return err
	}
	if err := requireCast(ctx, tx, gameID, actorID, segments); err != nil {
		return err
	}
	var typ Type
	var at int
	err = tx.QueryRowContext(ctx, `SELECT type, seq FROM entries WHERE id = ? AND scene_id = ?`, id, sceneID).Scan(&typ, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: entry %s in scene %s", ErrNotFound, id, sceneID)
	}
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if !typ.beat() {
		return fmt.Errorf("%w: entry %s is a %s, not a beat", ErrInvalid, id, typ)
	}
	if err := requirePresent(ctx, tx, sceneID, actorID, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET actor_id = ? WHERE id = ?`, actorID, id); err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM segments WHERE entry_id = ?`, id); err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if err := insertSegments(ctx, tx, id, segments); err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	return tx.Commit()
}

// Delete removes an entry and its segments. Later entries keep their seq;
// gaps are fine, order is what matters. An entry that anchors a scene or a
// thread stays.
func (s *Store) Delete(ctx context.Context, gameID, threadID, sceneID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	defer tx.Rollback()
	if err := scene.Require(ctx, tx, gameID, threadID, sceneID); err != nil {
		return err
	}
	var opening, ending int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM scenes WHERE id = ? AND opening_entry_id = ?) + (SELECT count(*) FROM entries WHERE id = ? AND type = 'present')`, sceneID, id, id).Scan(&opening); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if opening > 0 {
		return fmt.Errorf("%w: entry %s opens scene %s; delete the scene instead", ErrOpening, id, sceneID)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM threads WHERE ended_by_entry_id = ?`, id).Scan(&ending); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if ending > 0 {
		return fmt.Errorf("%w: another thread converged on entry %s", ErrEnding, id)
	}
	var typ Type
	var seq int
	err = tx.QueryRowContext(ctx, `SELECT type, seq FROM entries WHERE id = ? AND scene_id = ?`, id, sceneID).Scan(&typ, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: entry %s in scene %s", ErrNotFound, id, sceneID)
	}
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if typ == Enter {
		// Without the enter, the fold would have the actor absent while acting:
		// the first later beat of anyone it brought in, with no staging of theirs between.
		var name string
		var at int
		err := tx.QueryRowContext(ctx, `
			SELECT s.name, b.seq FROM entries b
			JOIN entry_actors a ON a.entry_id = ? AND a.subject_id = b.actor_id
			JOIN subjects s ON s.id = b.actor_id
			WHERE b.scene_id = ? AND b.seq > ? AND NOT EXISTS (
				SELECT 1 FROM entries x JOIN entry_actors xa ON xa.entry_id = x.id
				WHERE x.scene_id = b.scene_id AND x.type IN ('enter', 'exit') AND xa.subject_id = b.actor_id AND x.seq > ? AND x.seq < b.seq)
			ORDER BY b.seq LIMIT 1`, id, sceneID, seq, seq).Scan(&name, &at)
		if err == nil {
			return fmt.Errorf("%w: %s acts at entry %d of the scene on the strength of this enter; remove that first", ErrInUse, name, at)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("delete entry: %w", err)
		}
	}
	for _, q := range []string{
		`DELETE FROM segments WHERE entry_id IN (SELECT id FROM entries WHERE id = ? AND scene_id = ?)`,
		`DELETE FROM entry_actors WHERE entry_id IN (SELECT id FROM entries WHERE id = ? AND scene_id = ?)`,
	} {
		if _, err := tx.ExecContext(ctx, q, id, sceneID); err != nil {
			return fmt.Errorf("delete entry: %w", err)
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM entries WHERE id = ? AND scene_id = ?`, id, sceneID)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	} else if n == 0 {
		return fmt.Errorf("%w: entry %s in scene %s", ErrNotFound, id, sceneID)
	}
	return tx.Commit()
}

func validate(segments []Segment) error {
	if len(segments) == 0 {
		return fmt.Errorf("%w: no segments", ErrInvalid)
	}
	for i := range segments {
		segments[i].Text = strings.TrimSpace(segments[i].Text)
		if !segments[i].Kind.valid() {
			return fmt.Errorf("%w: segment %d: unknown kind %q", ErrInvalid, i+1, segments[i].Kind)
		}
		if segments[i].Text == "" {
			return fmt.Errorf("%w: segment %d: empty text", ErrInvalid, i+1)
		}
	}
	return nil
}

// requireCast checks the actor may take turns and every addressee exists in the game.
func requireCast(ctx context.Context, q querier, gameID, actorID string, segments []Segment) error {
	if err := requireActor(ctx, q, gameID, actorID); err != nil {
		return err
	}
	for _, seg := range segments {
		if seg.To != nil {
			if err := requireSubject(ctx, q, gameID, *seg.To); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertSegments(ctx context.Context, tx *sql.Tx, entryID string, segments []Segment) error {
	for i, seg := range segments {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO segments (entry_id, seq, kind, text, to_id) VALUES (?, ?, ?, ?, ?)`,
			entryID, i+1, seg.Kind, seg.Text, seg.To)
		if err != nil {
			return err
		}
	}
	return nil
}

// List returns the scene's entries in order, each with its segments.
func (s *Store) List(ctx context.Context, gameID, threadID, sceneID string) ([]Entry, error) {
	if err := scene.Require(ctx, s.db, gameID, threadID, sceneID); err != nil {
		return nil, err
	}
	return s.query(ctx, `WHERE e.scene_id = ?`, sceneID)
}

// ListThread returns every entry of the thread in script order: scene by
// scene, each scene's entries in sequence.
func (s *Store) ListThread(ctx context.Context, gameID, threadID string) ([]Entry, error) {
	if err := thread.Require(ctx, s.db, gameID, threadID); err != nil {
		return nil, err
	}
	return s.query(ctx, `JOIN scenes sc ON sc.id = e.scene_id WHERE sc.thread_id = ?`, threadID)
}

// query reads entries with their segments; where selects them and must keep
// the alias e for entries. Scenes order by id, which is time-ordered.
func (s *Store) query(ctx context.Context, where string, arg any) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.scene_id, COALESCE(e.actor_id, ''), e.seq, e.type, e.think, e.direction, e.created_at, s.kind, s.text, s.to_id, a.subject_id
		FROM entries e LEFT JOIN segments s ON s.entry_id = e.id LEFT JOIN entry_actors a ON a.entry_id = e.id `+where+`
		ORDER BY e.scene_id, e.seq, s.seq, a.subject_id`, arg)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var id, sceneID, actor, think, created string
		var typ Type
		var seq int
		var direction, kind, text, member sql.NullString
		var to *string
		if err := rows.Scan(&id, &sceneID, &actor, &seq, &typ, &think, &direction, &created, &kind, &text, &to, &member); err != nil {
			return nil, fmt.Errorf("list entries: %w", err)
		}
		if n := len(entries); n == 0 || entries[n-1].ID != id {
			at, err := time.Parse(time.RFC3339, created)
			if err != nil {
				return nil, fmt.Errorf("list entries: %w", err)
			}
			e := Entry{ID: id, SceneID: sceneID, ActorID: actor, Seq: seq, Type: typ, CreatedAt: at, Think: think}
			if direction.Valid {
				e.Direction = json.RawMessage(direction.String)
			}
			entries = append(entries, e)
		}
		last := &entries[len(entries)-1]
		if kind.Valid {
			last.Segments = append(last.Segments, Segment{Kind: Kind(kind.String), Text: text.String, To: to})
		}
		if member.Valid {
			last.Actors = append(last.Actors, member.String)
		}
	}
	return entries, rows.Err()
}

func requireActor(ctx context.Context, q querier, gameID, id string) error {
	var actor bool
	err := q.QueryRowContext(ctx, `SELECT actor FROM subjects WHERE id = ? AND game_id = ?`, id, gameID).Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: subject %s in game %s", subject.ErrNotFound, id, gameID)
	}
	if err != nil {
		return fmt.Errorf("look up actor: %w", err)
	}
	if !actor {
		return fmt.Errorf("%w: subject %s is not flagged as an actor", ErrNotActor, id)
	}
	return nil
}

// requirePresent checks the actor is in the scene at seq, or now when seq is 0.
func requirePresent(ctx context.Context, q scene.Reader, sceneID, actorID string, seq int) error {
	cast, err := scene.Cast(ctx, q, sceneID, seq)
	if err != nil {
		return err
	}
	if !slices.Contains(cast, actorID) {
		return fmt.Errorf("%w: actor %s is not in scene %s at that point", ErrNotPresent, actorID, sceneID)
	}
	return nil
}

func requireSubject(ctx context.Context, q querier, gameID, id string) error {
	var exists int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM subjects WHERE id = ? AND game_id = ?`, id, gameID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: addressee %s in game %s", subject.ErrNotFound, id, gameID)
	}
	if err != nil {
		return fmt.Errorf("look up subject: %w", err)
	}
	return nil
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
