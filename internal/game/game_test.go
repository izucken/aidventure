package game_test

import (
	"errors"
	"path/filepath"
	"testing"

	"aidventure/internal/game"
	"aidventure/internal/store"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	games := game.NewStore(db)

	if id, err := games.Current(ctx); err != nil || id != "" {
		t.Fatalf("fresh store: current = %q, %v; want none", id, err)
	}
	if err := games.SetCurrent(ctx, "nope"); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("select unknown: err = %v; want ErrNotFound", err)
	}

	first, err := games.Create(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := games.Create(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}

	list, err := games.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("list = %+v; want [first second] in creation order", list)
	}

	if err := games.SetCurrent(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := games.Current(ctx); err != nil || id != second.ID {
		t.Fatalf("current = %q, %v; want %q", id, err, second.ID)
	}

	if err := games.Rename(ctx, "nope", "x"); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("rename unknown: err = %v", err)
	}
	if err := games.Rename(ctx, first.ID, "first, renamed"); err != nil {
		t.Fatal(err)
	}
	if list, _ := games.List(ctx); list[0].Name != "first, renamed" {
		t.Fatalf("after rename = %+v", list)
	}
	if err := games.SetSettings(ctx, "nope", game.Settings{}); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("settings of unknown: err = %v", err)
	}
	if err := games.SetSettings(ctx, first.ID, game.Settings{Director: " dungeon master ", Voice: "third person, past"}); err != nil {
		t.Fatal(err)
	}
	if got, err := game.ReadSettings(ctx, db, first.ID); err != nil || got.Director != "dungeon master" || got.Voice != "third person, past" {
		t.Fatalf("settings = %+v, %v", got, err)
	}
	if list, _ := games.List(ctx); list[0].Voice != "third person, past" || list[1].Director != "" {
		t.Fatalf("list after settings = %+v", list)
	}
}
