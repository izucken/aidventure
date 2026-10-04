package play_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/store"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

func TestDoTransition(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	threads, subjects, scenes, entries := thread.NewStore(db), subject.NewStore(db), scene.NewStore(db), entry.NewStore(db)
	g, _ := game.NewStore(db).Create(ctx, "game")
	th, _ := threads.Create(ctx, g.ID, "Docks")
	tavern, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Tavern", Scene: true})
	barrel, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Barrel"})
	narrator, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Narrator", Actor: true})
	mara, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Actor: true, Tracked: true})
	fen, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Fen", Actor: true, Tracked: true})
	open := []entry.Segment{{Kind: entry.Narration, Text: "Rain on the shutters."}}
	cutTo := func(place string, cast ...string) play.Direction {
		return play.Direction{Type: entry.Cut, PlaceID: place, Cast: cast}
	}
	do := func(threadID string, d play.Direction, actor string, segments []entry.Segment) (play.Result, error) {
		return play.DoTransition(ctx, db, g.ID, threadID, play.Transition{Direction: d, ActorID: actor, Segments: segments})
	}

	bad := map[string]play.Direction{
		"stays":          {Type: entry.Narrative},
		"no place":       {Type: entry.Cut, Cast: []string{mara.ID}},
		"both places":    {Type: entry.Cut, PlaceID: tavern.ID, NewPlace: &subject.Input{Name: "x"}, Cast: []string{mara.ID}},
		"empty cast":     cutTo(tavern.ID),
		"nameless place": {Type: entry.Cut, NewPlace: &subject.Input{}, Cast: []string{mara.ID}},
	}
	for name, d := range bad {
		if _, err := do(th.ID, d, narrator.ID, open); !errors.Is(err, play.ErrBadTransition) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if _, err := do(th.ID, cutTo(barrel.ID, mara.ID), narrator.ID, open); !errors.Is(err, scene.ErrNotScene) {
		t.Fatalf("cut to barrel: err = %v", err)
	}
	if _, err := do(th.ID, cutTo(tavern.ID, barrel.ID), narrator.ID, open); !errors.Is(err, entry.ErrNotActor) {
		t.Fatalf("barrel in cast: err = %v", err)
	}
	if _, err := do(th.ID, cutTo(tavern.ID, mara.ID), narrator.ID, nil); !errors.Is(err, entry.ErrInvalid) {
		t.Fatalf("empty opening: err = %v", err)
	}
	if _, err := do(th.ID, play.Direction{Type: entry.Split, PlaceID: tavern.ID, Cast: []string{mara.ID}}, mara.ID, open); !errors.Is(err, play.ErrNoScene) {
		t.Fatalf("split from nothing: err = %v", err)
	}

	first, err := do(th.ID, cutTo(tavern.ID, mara.ID, narrator.ID), narrator.ID, open)
	if err != nil {
		t.Fatal(err)
	}
	sc, e := first.Scene, first.Entry
	if sc.PlaceID != tavern.ID || sc.Opening != e.ID || len(sc.Cast) != 2 || e.Type != entry.Cut || e.Seq != 1 || first.ThreadID != th.ID {
		t.Fatalf("cut = %+v", first)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, sc.ID, e.ID); !errors.Is(err, entry.ErrOpening) {
		t.Fatalf("delete opening entry: err = %v", err)
	}

	// A cut exits those who cross into the new scene; whoever stays behind is left unsaid.
	if _, err := do(th.ID, cutTo(tavern.ID, mara.ID, fen.ID), mara.ID, open); err != nil {
		t.Fatal(err)
	}
	if left, _ := entries.List(ctx, g.ID, th.ID, sc.ID); left[len(left)-1].Type != entry.Exit || len(left[len(left)-1].Actors) != 1 || left[len(left)-1].Actors[0] != mara.ID {
		t.Fatalf("after cut, first scene ends with %+v", left[len(left)-1])
	}
	if now, _ := scenes.List(ctx, g.ID, th.ID); len(now[0].Cast) != 1 || now[0].Cast[0] != narrator.ID {
		t.Fatalf("left behind = %+v", now[0].Cast)
	}

	// A place made on the way is linked to the place left, in the given words.
	fresh := play.Direction{Type: entry.Cut, NewPlace: &subject.Input{Name: " The quay ", Description: "Stone landing."}, Cast: []string{mara.ID, narrator.ID, fen.ID}}
	if _, err := do(th.ID, fresh, mara.ID, open); !errors.Is(err, play.ErrBadTransition) {
		t.Fatalf("new place adrift: err = %v", err)
	}
	fresh.Link = " down the steps from "
	second, err := do(th.ID, fresh, mara.ID, open)
	if err != nil {
		t.Fatal(err)
	}
	list, err := subjects.List(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	var quay *subject.Subject
	for i := range list {
		if list[i].ID == second.Scene.PlaceID {
			quay = &list[i]
		}
	}
	if quay == nil || quay.Name != "The quay" || !quay.Scene || quay.Description != "Stone landing." {
		t.Fatalf("new place = %+v", quay)
	}
	if links, err := subjects.Links(ctx, g.ID); err != nil || len(links) != 1 || links[0].FromID != quay.ID || links[0].ToID != tavern.ID || links[0].Relation != "down the steps from" {
		t.Fatalf("links = %+v, %v", links, err)
	}
	if all, err := scenes.List(ctx, g.ID, th.ID); err != nil || len(all) != 3 || all[2].ID != second.Scene.ID || len(all[2].Cast) != 3 {
		t.Fatalf("scenes = %+v, %v", all, err)
	}
	if err := subjects.Delete(ctx, g.ID, mara.ID); !errors.Is(err, subject.ErrInUse) {
		t.Fatalf("delete cast member: err = %v", err)
	}

	// A split follows tracked actors who part: one must leave and one must stay; the untracked go either way.
	splitTo := func(place string, cast ...string) play.Direction {
		return play.Direction{Type: entry.Split, PlaceID: place, Cast: cast}
	}
	for name, d := range map[string]play.Direction{
		"nobody leaves":         splitTo(tavern.ID),
		"absent leaves":         splitTo(tavern.ID, barrel.ID),
		"only the crowd leaves": splitTo(tavern.ID, narrator.ID),
		"every tracked leaves":  splitTo(tavern.ID, mara.ID, fen.ID, fen.ID),
		"everyone leaves":       splitTo(tavern.ID, mara.ID, narrator.ID, fen.ID),
	} {
		if _, err := do(th.ID, d, mara.ID, open); !errors.Is(err, play.ErrBadTransition) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if _, err := do(th.ID, splitTo(tavern.ID, mara.ID), narrator.ID, open); !errors.Is(err, entry.ErrNotPresent) {
		t.Fatalf("one who stays writes the arrival: err = %v", err)
	}
	off, err := do(th.ID, splitTo(tavern.ID, mara.ID, narrator.ID), mara.ID, open)
	if err != nil {
		t.Fatal(err)
	}
	if off.ThreadID == th.ID || off.Entry.Type != entry.Split || off.Scene.ThreadID != off.ThreadID || off.Scene.Opening != off.Entry.ID || len(off.Scene.Cast) != 2 {
		t.Fatalf("split = %+v", off)
	}
	all, _ := threads.List(ctx, g.ID)
	if len(all) != 2 || all[1].ID != off.ThreadID || all[1].Name != "Narrator, Mara" || all[1].ParentSceneID != second.Scene.ID {
		t.Fatalf("threads = %+v", all)
	}
	if left, _ := scenes.List(ctx, g.ID, th.ID); len(left[2].Cast) != 1 || left[2].Cast[0] != fen.ID {
		t.Fatalf("source cast after split = %+v", left[2].Cast)
	}

	// A converge walks a thread's cast into another's current scene and ends it.
	converge := func(into string) play.Direction { return play.Direction{Type: entry.Converge, IntoThreadID: into} }
	if _, err := do(off.ThreadID, converge(off.ThreadID), mara.ID, open); !errors.Is(err, play.ErrBadTransition) {
		t.Fatalf("converge into self: err = %v", err)
	}
	if _, err := do(off.ThreadID, converge("nope"), mara.ID, open); !errors.Is(err, thread.ErrNotFound) {
		t.Fatalf("converge into unknown: err = %v", err)
	}
	if err := threads.SetCurrent(ctx, g.ID, off.ThreadID); err != nil {
		t.Fatal(err)
	}
	back, err := do(off.ThreadID, converge(th.ID), fen.ID, open)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThreadID != th.ID || back.Scene.ID != second.Scene.ID || back.Entry.Type != entry.Converge || back.Entry.Seq != 2 || len(back.Scene.Cast) != 3 {
		t.Fatalf("converge = %+v", back)
	}
	all, _ = threads.List(ctx, g.ID)
	if all[1].EndedByEntryID != back.Entry.ID {
		t.Fatalf("ended thread = %+v", all[1])
	}
	if cur, _ := threads.Current(ctx, g.ID); cur != th.ID {
		t.Fatalf("current thread after converge = %q", cur)
	}
	if joined, _ := scenes.List(ctx, g.ID, th.ID); len(joined[2].Cast) != 3 {
		t.Fatalf("joined cast = %+v", joined[2].Cast)
	}
	if err := entries.Delete(ctx, g.ID, th.ID, second.Scene.ID, back.Entry.ID); !errors.Is(err, entry.ErrEnding) {
		t.Fatalf("delete converge entry: err = %v", err)
	}
	if _, err := do(off.ThreadID, converge(th.ID), fen.ID, open); !errors.Is(err, thread.ErrEnded) {
		t.Fatalf("play on in ended thread: err = %v", err)
	}
	if _, err := do(th.ID, converge(off.ThreadID), fen.ID, open); !errors.Is(err, thread.ErrEnded) {
		t.Fatalf("converge into ended thread: err = %v", err)
	}
	if _, err := do(th.ID, cutTo(tavern.ID, narrator.ID), narrator.ID, open); err != nil {
		t.Fatal(err)
	}
	if all, err := entries.ListThread(ctx, g.ID, th.ID); err != nil {
		t.Fatal(err)
	} else if beats := slices.DeleteFunc(slices.Clone(all), func(e entry.Entry) bool { return e.ActorID == "" }); len(beats) != 5 || beats[3].Type != entry.Converge || beats[4].Type != entry.Cut {
		t.Fatalf("thread beats = %+v", beats)
	}

	// Deleting the thread takes its play with it and reopens the thread that converged into it.
	if err := threads.Delete(ctx, g.ID, th.ID); err != nil {
		t.Fatal(err)
	}
	all, _ = threads.List(ctx, g.ID)
	if len(all) != 1 || all[0].ID != off.ThreadID || all[0].EndedByEntryID != "" || all[0].ParentSceneID != "" {
		t.Fatalf("after delete = %+v", all)
	}
	if err := thread.RequireOpen(ctx, db, g.ID, off.ThreadID); err != nil {
		t.Fatalf("reopened thread: err = %v", err)
	}
	if left, err := scenes.List(ctx, g.ID, off.ThreadID); err != nil || len(left) != 1 || len(left[0].Cast) != 2 {
		t.Fatalf("surviving thread = %+v, %v", left, err)
	}
}
