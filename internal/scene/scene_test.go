package scene_test

import (
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
	g, _ := game.NewStore(db).Create(ctx, "game")
	th, _ := thread.NewStore(db).Create(ctx, g.ID, "thread")
	subjects := subject.NewStore(db)
	tavern, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Tavern", Scene: true})
	barrel, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Barrel"})
	mara, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Actor: true})
	fen, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Fen", Actor: true})
	scenes, entries := scene.NewStore(db), entry.NewStore(db)

	insert := func(threadID, placeID string) (scene.Scene, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		sc, err := scene.Insert(ctx, tx, g.ID, threadID, placeID)
		if err != nil {
			return sc, err
		}
		return sc, tx.Commit()
	}

	if _, err := insert("nope", tavern.ID); !errors.Is(err, thread.ErrNotFound) {
		t.Fatalf("unknown thread: err = %v", err)
	}
	if _, err := insert(th.ID, "nope"); !errors.Is(err, scene.ErrNoPlace) {
		t.Fatalf("unknown place: err = %v", err)
	}
	if _, err := insert(th.ID, barrel.ID); !errors.Is(err, scene.ErrNotScene) {
		t.Fatalf("barrel as place: err = %v", err)
	}
	if _, err := scene.Latest(ctx, db, g.ID, th.ID); !errors.Is(err, scene.ErrNotFound) {
		t.Fatalf("latest of none: err = %v", err)
	}
	first, err := insert(th.ID, tavern.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := insert(th.ID, tavern.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest, err := scene.Latest(ctx, db, g.ID, th.ID); err != nil || latest.ID != second.ID || len(latest.Cast) != 0 {
		t.Fatalf("latest = %+v, %v", latest, err)
	}

	// Who is present is the fold of the scene's presence entries, at the end or at a point.
	if _, err := entries.Presence(ctx, g.ID, th.ID, first.ID, entry.Present, []string{mara.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, second.ID, entry.Present, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, second.ID, entry.Enter, []string{mara.ID, fen.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := entries.Presence(ctx, g.ID, th.ID, second.ID, entry.Exit, []string{mara.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := scenes.List(ctx, g.ID, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID || len(list[0].Cast) != 1 || list[0].Cast[0] != mara.ID || len(list[1].Cast) != 1 || list[1].Cast[0] != fen.ID {
		t.Fatalf("list = %+v", list)
	}
	if at, err := scene.Cast(ctx, db, second.ID, 2); err != nil || len(at) != 2 {
		t.Fatalf("cast after the enter = %v, %v", at, err)
	}

	if err := scenes.Retitle(ctx, g.ID, th.ID, "nope", "x"); !errors.Is(err, scene.ErrNotFound) {
		t.Fatalf("retitle unknown: err = %v", err)
	}
	if err := scenes.Retitle(ctx, g.ID, th.ID, second.ID, "Later"); err != nil {
		t.Fatal(err)
	}
	if list, _ := scenes.List(ctx, g.ID, th.ID); list[1].Title != "Later" || list[0].Title != "" {
		t.Fatalf("after retitle = %+v", list)
	}
	if err := scenes.Delete(ctx, g.ID, th.ID, "nope"); !errors.Is(err, scene.ErrNotFound) {
		t.Fatalf("delete unknown: err = %v", err)
	}
	if err := scenes.Delete(ctx, g.ID, th.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	list, err = scenes.List(ctx, g.ID, th.ID)
	if err != nil || len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("after delete = %+v, %v", list, err)
	}
	if err := subjects.Delete(ctx, g.ID, fen.ID); !errors.Is(err, subject.ErrInUse) {
		t.Fatalf("delete present actor: err = %v", err)
	}
}
