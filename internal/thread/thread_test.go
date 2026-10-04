package thread_test

import (
	"errors"
	"path/filepath"
	"testing"

	"aidventure/internal/game"
	"aidventure/internal/store"
	"aidventure/internal/thread"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	games := game.NewStore(db)
	threads := thread.NewStore(db)

	if _, err := threads.Create(ctx, "nope", "x"); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("create in unknown game: err = %v; want game.ErrNotFound", err)
	}
	if _, err := threads.List(ctx, "nope"); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("list unknown game: err = %v; want game.ErrNotFound", err)
	}

	g, err := games.Create(ctx, "game")
	if err != nil {
		t.Fatal(err)
	}
	other, err := games.Create(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := threads.Current(ctx, g.ID); err != nil || id != "" {
		t.Fatalf("fresh game: current = %q, %v; want none", id, err)
	}

	first, err := threads.Create(ctx, g.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := threads.Create(ctx, g.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := threads.Create(ctx, other.ID, "elsewhere")
	if err != nil {
		t.Fatal(err)
	}

	list, err := threads.List(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("list = %+v; want [first second]", list)
	}

	if err := threads.SetCurrent(ctx, g.ID, foreign.ID); !errors.Is(err, thread.ErrNotFound) {
		t.Fatalf("select thread of another game: err = %v; want ErrNotFound", err)
	}
	if err := threads.SetCurrent(ctx, g.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := threads.Current(ctx, g.ID); err != nil || id != second.ID {
		t.Fatalf("current = %q, %v; want %q", id, err, second.ID)
	}

	if err := threads.Rename(ctx, other.ID, first.ID, "x"); !errors.Is(err, thread.ErrNotFound) {
		t.Fatalf("rename via another game: err = %v", err)
	}
	if err := threads.Rename(ctx, g.ID, first.ID, "first, renamed"); err != nil {
		t.Fatal(err)
	}
	if list, _ := threads.List(ctx, g.ID); list[0].Name != "first, renamed" {
		t.Fatalf("after rename = %+v", list)
	}
	if err := threads.Delete(ctx, g.ID, "nope"); !errors.Is(err, thread.ErrNotFound) {
		t.Fatalf("delete unknown: err = %v", err)
	}
	if err := threads.Delete(ctx, g.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if id, _ := threads.Current(ctx, g.ID); id != "" {
		t.Fatalf("deleted thread still current: %q", id)
	}
	if list, _ := threads.List(ctx, g.ID); len(list) != 1 || list[0].ID != first.ID {
		t.Fatalf("after delete = %+v", list)
	}

	// A split-off thread remembers its parent scene; an ended one refuses play.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := thread.Insert(ctx, tx, g.ID, "Mara", "scene-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if tx, err = db.BeginTx(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := thread.End(ctx, tx, g.ID, child.ID, "entry-9"); err != nil {
		t.Fatal(err)
	}
	if err := thread.End(ctx, tx, g.ID, child.ID, "entry-9"); !errors.Is(err, thread.ErrEnded) {
		t.Fatalf("end twice: err = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := thread.RequireOpen(ctx, db, g.ID, child.ID); !errors.Is(err, thread.ErrEnded) {
		t.Fatalf("ended thread open: err = %v", err)
	}
	if err := thread.Require(ctx, db, g.ID, child.ID); err != nil {
		t.Fatalf("ended thread gone: err = %v", err)
	}
	list, _ = threads.List(ctx, g.ID)
	if len(list) != 3 || list[2].ParentSceneID != "scene-1" || list[2].EndedByEntryID != "entry-9" || list[0].ParentSceneID != "" || list[0].EndedByEntryID != "" {
		t.Fatalf("list = %+v", list)
	}
}
