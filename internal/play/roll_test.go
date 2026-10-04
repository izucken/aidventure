package play_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/llm"
	"aidventure/internal/model"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/store"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

type fakeEngine struct {
	got   llm.Request
	think string
	reply string
}

func (f *fakeEngine) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	f.got = req
	return llm.Response{Think: f.think, Text: f.reply, PromptTokens: 10, Tokens: 3}, nil
}

func (f *fakeEngine) Render(_ context.Context, req llm.Request) (llm.Rendered, error) {
	return llm.Rendered{Prompt: "<" + req.User + ">", Grammar: req.Grammar}, nil
}

func TestRoll(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	models, threads, subjects, scenes, entries := model.NewStore(db), thread.NewStore(db), subject.NewStore(db), scene.NewStore(db), entry.NewStore(db)
	g, _ := game.NewStore(db).Create(ctx, "game")
	th, _ := threads.Create(ctx, g.ID, "Docks")
	tavern, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Tavern", Description: "Low beams.", Scene: true})
	innkeeper, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Innkeeper", Description: "Tired.", Actor: true})
	mara, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Description: "Owes.", Actor: true, Tracked: true})
	fen, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Fen", Description: "Wet.", Actor: true, Tracked: true})
	move := func(threadID string, d play.Direction, actor, line string) play.Result {
		t.Helper()
		res, err := play.DoTransition(ctx, db, g.ID, threadID, play.Transition{Direction: d, ActorID: actor, Segments: []entry.Segment{{Kind: entry.Narration, Text: line}}})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	sc := move(th.ID, play.Direction{Type: entry.Cut, PlaceID: tavern.ID, Cast: []string{innkeeper.ID, mara.ID}}, innkeeper.ID, "You said three days.").Scene

	eng := &fakeEngine{think: "Stall her.", reply: "MARA:\n(does not look up)\n\"I know what I said.\"\n"}
	roller := &play.Roller{
		DB: db, Models: models, Threads: threads, Subjects: subjects, Scenes: scenes, Entries: entries,
		Engine: func(model.Model) (llm.Engine, error) { return eng, nil },
		Log:    slog.Default(),
	}

	if _, err := roller.Roll(ctx, g.ID, th.ID, sc.ID, mara.ID, nil); !errors.Is(err, play.ErrNoModel) {
		t.Fatalf("no model: err = %v", err)
	}
	m, err := models.Create(ctx, model.Input{Name: "m", Engine: model.LlamaCpp, Endpoint: "http://h:1", Model: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := models.SetCurrent(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc.ID, tavern.ID, nil); !errors.Is(err, entry.ErrNotActor) {
		t.Fatalf("tavern rolls: err = %v", err)
	}

	e, err := roller.Roll(ctx, g.ID, th.ID, sc.ID, mara.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 2 || e.ActorID != mara.ID || e.Think != "Stall her." || e.Type != entry.Narrative || len(e.Segments) != 2 || e.Segments[0].Kind != entry.Action {
		t.Fatalf("entry = %+v", e)
	}
	sc2 := move(th.ID, play.Direction{Type: entry.Cut, PlaceID: tavern.ID, Cast: []string{mara.ID}}, mara.ID, "Later, the same room.").Scene
	eng.reply = "(does not look up)\n\"I know what I said.\"\n"
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc2.ID, mara.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := subjects.AddLink(ctx, g.ID, subject.LinkInput{FromID: innkeeper.ID, ToID: tavern.ID, Relation: "keeps"}); err != nil {
		t.Fatal(err)
	}
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc2.ID, mara.ID, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PLACES\nTavern: Low beams.", "Innkeeper: Tired.", "Mara (tracked): Owes.", "\nLINKS\nInnkeeper keeps Tavern.\n\nSCRIPT\n",
		"TAVERN\nPresent: Innkeeper, Mara\n", "INNKEEPER:\nYou said three days.",
		"I know what I said.", "TAVERN\nPresent: Mara\n", "Later, the same room.", "Next entry for Mara.",
	} {
		if !strings.Contains(eng.got.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, eng.got.User)
		}
	}
	if eng.got.Prefill != "MARA:\n" || eng.got.Grammar != entry.Grammar {
		t.Fatalf("request = %+v", eng.got)
	}
	if err := game.NewStore(db).SetSettings(ctx, g.ID, game.Settings{Voice: "third person, past, spare"}); err != nil {
		t.Fatal(err)
	}
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc2.ID, mara.ID, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(eng.got.System, "\nNarration lines are written in this voice: third person, past, spare") {
		t.Fatalf("actor brief = %q", eng.got.System)
	}
	if strings.Index(eng.got.User, "You said three days.") > strings.Index(eng.got.User, "Later, the same room.") {
		t.Fatalf("scenes out of order:\n%s", eng.got.User)
	}

	// Another thread converges on the scene, then one of its cast splits off
	// again. The script shows both where they happened, and who opened the scene.
	harbour, _ := threads.Create(ctx, g.ID, "Harbour")
	quay := move(harbour.ID, play.Direction{Type: entry.Cut, NewPlace: &subject.Input{Name: "The quay"}, Cast: []string{fen.ID}}, fen.ID, "Gulls.").Scene
	move(harbour.ID, play.Direction{Type: entry.Converge, IntoThreadID: th.ID}, fen.ID, "Back inside.")
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc2.ID, mara.ID, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TAVERN\nPresent: Mara\n", "\nEnter Fen.\n\nFEN:\nBack inside.\n"} {
		if !strings.Contains(eng.got.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, eng.got.User)
		}
	}
	move(th.ID, play.Direction{Type: entry.Split, PlaceID: quay.PlaceID, Cast: []string{fen.ID}}, fen.ID, "Out again.")
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc2.ID, mara.ID, nil); err != nil {
		t.Fatal(err)
	}
	// The split came after every entry so far; the exit falls at the end.
	for _, want := range []string{"TAVERN\nPresent: Mara\n", "\nExit Fen.\n\nNext entry for Mara."} {
		if !strings.Contains(eng.got.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, eng.got.User)
		}
	}
	if strings.Contains(eng.got.User, "Out again.") {
		t.Fatalf("other thread's scene in the script:\n%s", eng.got.User)
	}

	p, err := roller.Preview(ctx, g.ID, th.ID, sc2.ID, mara.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Model != "m" || p.Request.Prefill != "MARA:\n" || !strings.HasPrefix(p.Rendered.Prompt, "<PLACES") {
		t.Fatalf("preview = %+v", p)
	}

	eng.reply = "MARA:\n"
	if _, err := roller.Roll(ctx, g.ID, th.ID, sc2.ID, mara.ID, nil); !errors.Is(err, play.ErrEmpty) {
		t.Fatalf("empty reply: err = %v", err)
	}
}
