package entry_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/scene"
	"aidventure/internal/store"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, err := game.NewStore(db).Create(ctx, "game")
	if err != nil {
		t.Fatal(err)
	}
	th, err := thread.NewStore(db).Create(ctx, g.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	subjects := subject.NewStore(db)
	tavern, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Tavern", Scene: true})
	innkeeper, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Innkeeper", Actor: true})
	mara, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Actor: true})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scene.Insert(ctx, tx, g.ID, th.ID, tavern.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	entries := entry.NewStore(db)
	say := []entry.Segment{{Kind: entry.Speech, Text: "You said three days."}}
	count := func() int {
		t.Helper()
		list, err := entries.List(ctx, g.ID, th.ID, sc.ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}

	// A scene opens with who is present; nothing happens in it before that.
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", say); !errors.Is(err, entry.ErrNotPresent) {
		t.Fatalf("beat before the head: err = %v", err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Enter, []string{innkeeper.ID}); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("enter before the head: err = %v", err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Present, []string{tavern.ID}); !errors.Is(err, entry.ErrNotActor) {
		t.Fatalf("tavern present: err = %v", err)
	}
	head, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Present, []string{innkeeper.ID})
	if err != nil {
		t.Fatal(err)
	}
	if head.Seq != 1 || head.Type != entry.Present || len(head.Actors) != 1 {
		t.Fatalf("head = %+v", head)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Present, nil); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("second head: err = %v", err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Exit, nil); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("exit of nobody: err = %v", err)
	}

	if _, err := entries.Append(ctx, g.ID, th.ID, "nope", innkeeper.ID, "", say); !errors.Is(err, scene.ErrNotFound) {
		t.Fatalf("unknown scene: err = %v", err)
	}
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, tavern.ID, "", say); !errors.Is(err, entry.ErrNotActor) {
		t.Fatalf("tavern speaks: err = %v", err)
	}
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", nil); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("no segments: err = %v", err)
	}
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", []entry.Segment{{Kind: "song", Text: "la"}}); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("bad kind: err = %v", err)
	}
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", []entry.Segment{{Kind: entry.Speech, Text: "  "}}); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("blank text: err = %v", err)
	}
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, mara.ID, "", say); !errors.Is(err, entry.ErrNotPresent) {
		t.Fatalf("absent actor: err = %v", err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Enter, []string{mara.ID}); err != nil {
		t.Fatal(err)
	}
	nobody := "nope"
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", []entry.Segment{{Kind: entry.Speech, Text: "Hey.", To: &nobody}}); !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("unknown addressee: err = %v", err)
	}

	first, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", []entry.Segment{
		{Kind: entry.Action, Text: "sets a cup down"},
		{Kind: entry.Speech, Text: "You said three days.", To: &mara.ID},
		{Kind: entry.Thought, Text: "I cannot lie for her."},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := entries.Append(ctx, g.ID, th.ID, sc.ID, mara.ID, "She is stalling.", say)
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 3 || second.Seq != 4 {
		t.Fatalf("seq = %d, %d; want 3, 4", first.Seq, second.Seq)
	}

	if err := entries.Update(ctx, g.ID, th.ID, sc.ID, "nope", mara.ID, say); !errors.Is(err, entry.ErrNotFound) {
		t.Fatalf("update unknown: err = %v", err)
	}
	if err := entries.Update(ctx, g.ID, th.ID, sc.ID, second.ID, mara.ID, []entry.Segment{{Kind: entry.Speech, Text: "I know."}, {Kind: entry.Action, Text: "looks away"}}); err != nil {
		t.Fatal(err)
	}

	list, err := entries.List(ctx, g.ID, th.ID, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || list[1].Type != entry.Enter || list[1].Actors[0] != mara.ID || len(list[2].Segments) != 3 || len(list[3].Segments) != 2 || list[3].Think != "She is stalling." || list[2].Think != "" {
		t.Fatalf("list = %+v", list)
	}
	if got := list[2].Segments[1]; got.Kind != entry.Speech || got.To == nil || *got.To != mara.ID {
		t.Fatalf("segment = %+v; want speech to mara", got)
	}

	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, "nope"); !errors.Is(err, entry.ErrNotFound) {
		t.Fatalf("delete unknown: err = %v", err)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, head.ID); !errors.Is(err, entry.ErrOpening) {
		t.Fatalf("delete the head: err = %v", err)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	third, err := entries.Append(ctx, g.ID, th.ID, sc.ID, innkeeper.ID, "", say)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ = entries.List(ctx, g.ID, th.ID, sc.ID); len(list) != 4 || list[2].ID != second.ID || list[3].ID != third.ID || third.Seq != 5 {
		t.Fatalf("after delete = %+v", list)
	}

	// An entry inserted after another takes its place; what followed moves down.
	if _, err := entries.AppendAfter(ctx, g.ID, th.ID, sc.ID, "nope", innkeeper.ID, "", say); !errors.Is(err, entry.ErrNotFound) {
		t.Fatalf("insert after unknown: err = %v", err)
	}
	between, err := entries.AppendAfter(ctx, g.ID, th.ID, sc.ID, second.ID, innkeeper.ID, "", []entry.Segment{{Kind: entry.Action, Text: "leans in"}})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ = entries.List(ctx, g.ID, th.ID, sc.ID); len(list) != 5 || list[3].ID != between.ID || list[4].ID != third.ID || between.Seq != 5 || list[4].Seq != 6 {
		t.Fatalf("after insert = %+v", list)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, between.ID); err != nil {
		t.Fatal(err)
	}

	// Presence is read at the point of insertion: after Mara leaves she cannot
	// speak at the end, but can where she was still there.
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Exit, []string{mara.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, mara.ID, "", say); !errors.Is(err, entry.ErrNotPresent) {
		t.Fatalf("gone actor speaks: err = %v", err)
	}
	mid, err := entries.AppendAfter(ctx, g.ID, th.ID, sc.ID, second.ID, mara.ID, "", say)
	if err != nil {
		t.Fatalf("gone actor speaks where she was: err = %v", err)
	}
	if list, _ = entries.List(ctx, g.ID, th.ID, sc.ID); len(list) != 6 || list[3].ID != mid.ID || list[5].Type != entry.Exit {
		t.Fatalf("after placed insert = %+v", list)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, mid.ID); err != nil {
		t.Fatal(err)
	}

	// A direction is an entry with no actor and no segments, carrying its decision; it is passed, not edited.
	if _, err := entries.Direct(ctx, g.ID, th.ID, sc.ID, "why", json.RawMessage("not json")); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("direct with junk: err = %v", err)
	}
	directed, err := entries.Direct(ctx, g.ID, th.ID, sc.ID, "She was addressed.", json.RawMessage(`{"type":"narrative"}`))
	if err != nil {
		t.Fatal(err)
	}
	list, err = entries.List(ctx, g.ID, th.ID, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := list[len(list)-1]
	if len(list) != 6 || last.ID != directed.ID || last.Type != entry.Direction || last.ActorID != "" || last.Think != "She was addressed." || string(last.Direction) != `{"type":"narrative"}` || len(last.Segments) != 0 {
		t.Fatalf("direction = %+v", last)
	}
	if err := entries.Update(ctx, g.ID, th.ID, sc.ID, directed.ID, innkeeper.ID, say); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("edit direction: err = %v", err)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, directed.ID); err != nil {
		t.Fatal(err)
	}
	if count() != 5 {
		t.Fatalf("after passing = %d entries", count())
	}
}
