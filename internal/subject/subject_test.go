package subject_test

import (
	"errors"
	"path/filepath"
	"testing"

	"aidventure/internal/game"
	"aidventure/internal/store"
	"aidventure/internal/subject"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	games := game.NewStore(db)
	subjects := subject.NewStore(db)

	if _, err := subjects.Create(ctx, "nope", subject.Input{Name: "x"}); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("create in unknown game: err = %v; want game.ErrNotFound", err)
	}

	g, err := games.Create(ctx, "game")
	if err != nil {
		t.Fatal(err)
	}
	other, err := games.Create(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}

	barrel, err := subjects.Create(ctx, g.ID, subject.Input{Name: "Barrel", Description: "Oak, half full."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := subjects.Create(ctx, other.ID, subject.Input{Name: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}

	if err := subjects.Update(ctx, other.ID, barrel.ID, subject.Input{Name: "x"}); !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("update via another game: err = %v; want ErrNotFound", err)
	}
	if err := subjects.Update(ctx, g.ID, barrel.ID, subject.Input{Name: "Barrel", Description: "Oak, empty.", Actor: true, Scene: true, User: true, Tracked: true}); err != nil {
		t.Fatal(err)
	}

	list, err := subjects.List(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != barrel.ID || list[0].Description != "Oak, empty." || !list[0].Actor || !list[0].Scene || !list[0].User || !list[0].Tracked {
		t.Fatalf("list = %+v; want the updated barrel only", list)
	}

	// Links join two subjects of the game, read from one to the other, and go with either.
	quay, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Quay", Scene: true})
	for name, in := range map[string]subject.LinkInput{
		"blank relation": {FromID: barrel.ID, ToID: quay.ID, Relation: " "},
		"self":           {FromID: barrel.ID, ToID: barrel.ID, Relation: "is"},
	} {
		if _, err := subjects.AddLink(ctx, g.ID, in); !errors.Is(err, subject.ErrBadLink) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if _, err := subjects.AddLink(ctx, g.ID, subject.LinkInput{FromID: barrel.ID, ToID: "nope", Relation: "on"}); !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("link to unknown: err = %v", err)
	}
	l, err := subjects.AddLink(ctx, g.ID, subject.LinkInput{FromID: barrel.ID, ToID: quay.ID, Relation: " stands on "})
	if err != nil {
		t.Fatal(err)
	}
	if err := subjects.UpdateLink(ctx, g.ID, "nope", subject.LinkInput{FromID: barrel.ID, ToID: quay.ID, Relation: "x"}); !errors.Is(err, subject.ErrNoLink) {
		t.Fatalf("update unknown link: err = %v", err)
	}
	if err := subjects.UpdateLink(ctx, g.ID, l.ID, subject.LinkInput{FromID: quay.ID, ToID: barrel.ID, Relation: "holds"}); err != nil {
		t.Fatal(err)
	}
	if links, err := subjects.Links(ctx, g.ID); err != nil || len(links) != 1 || links[0].FromID != quay.ID || links[0].Relation != "holds" {
		t.Fatalf("links = %+v, %v", links, err)
	}
	if err := subjects.DeleteLink(ctx, g.ID, "nope"); !errors.Is(err, subject.ErrNoLink) {
		t.Fatalf("delete unknown link: err = %v", err)
	}
	if err := subjects.Delete(ctx, g.ID, quay.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := subjects.Links(ctx, g.ID); len(links) != 0 {
		t.Fatalf("links outlive their subject: %+v", links)
	}

	if err := subjects.Delete(ctx, g.ID, "nope"); !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("delete unknown: err = %v", err)
	}
	if err := subjects.Delete(ctx, g.ID, barrel.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := subjects.List(ctx, g.ID); err != nil || len(list) != 0 {
		t.Fatalf("after delete = %+v, %v", list, err)
	}
}
