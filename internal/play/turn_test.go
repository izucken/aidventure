package play_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// crew answers the director's job with moves and the entry job with lines,
// and keeps the last directing request.
type crew struct {
	moves  string
	lines  string
	asked  []string
	direct llm.Request
}

func (c *crew) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	c.asked = append(c.asked, req.System[:20])
	if strings.HasPrefix(req.System, "You decide where") {
		c.direct = req
		return llm.Response{Think: "The scene has run its course.", Text: c.moves}, nil
	}
	return llm.Response{Text: c.lines}, nil
}

func (c *crew) Render(_ context.Context, req llm.Request) (llm.Rendered, error) {
	return llm.Rendered{Prompt: req.User, Grammar: req.Grammar}, nil
}

// directed reads the decision an advance recorded.
func directed(t *testing.T, out play.Outcome) play.Direction {
	t.Helper()
	if out.Directed == nil || out.Directed.Type != entry.Direction || out.Directed.ActorID != "" {
		t.Fatalf("no direction recorded: %+v", out)
	}
	var d play.Direction
	if err := json.Unmarshal(out.Directed.Direction, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAdvance(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	models, threads, subjects, scenes, entries := model.NewStore(db), thread.NewStore(db), subject.NewStore(db), scene.NewStore(db), entry.NewStore(db)
	g, _ := game.NewStore(db).Create(ctx, "game")
	th, _ := threads.Create(ctx, g.ID, "Docks")
	tavern, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Tavern", Scene: true})
	innkeeper, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Innkeeper", Actor: true, Tracked: true})
	mara, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Mara", Actor: true, User: true, Tracked: true})
	fen, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Fen", Actor: true})
	m, _ := models.Create(ctx, model.Input{Name: "m", Engine: model.LlamaCpp, Endpoint: "http://h:1", Model: "x"})
	_ = models.SetCurrent(ctx, m.ID)
	eng := &crew{moves: "narrative\nnext Innkeeper\n", lines: "\"Nine days.\"\n"}
	roller := &play.Roller{
		DB: db, Models: models, Threads: threads, Subjects: subjects, Scenes: scenes, Entries: entries,
		Engine: func(model.Model) (llm.Engine, error) { return eng, nil },
		Log:    slog.Default(),
	}
	rain := []entry.Segment{{Kind: entry.Narration, Text: "Rain."}}
	count := func(sceneID string) int {
		t.Helper()
		list, err := entries.List(ctx, g.ID, th.ID, sceneID)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}

	if _, err := roller.Advance(ctx, g.ID, th.ID, nil); !errors.Is(err, play.ErrNoScene) {
		t.Fatalf("no scene: err = %v", err)
	}
	if cur, err := roller.Turn(ctx, g.ID, th.ID); err != nil || cur != nil {
		t.Fatalf("turn without a scene = %+v, %v", cur, err)
	}
	first, err := play.DoTransition(ctx, db, g.ID, th.ID, play.Transition{Direction: play.Direction{Type: entry.Cut, PlaceID: tavern.ID, Cast: []string{innkeeper.ID, mara.ID}}, ActorID: innkeeper.ID, Segments: rain})
	if err != nil {
		t.Fatal(err)
	}
	sc := first.Scene

	// Model actor named: the direction is recorded, then the entry written. Director, then actor: two asks.
	out, err := roller.Advance(ctx, g.ID, th.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Turn != nil || out.Result == nil || out.Entry.ActorID != innkeeper.ID || out.Entry.Segments[0].Text != "Nine days." || out.Entry.Type != entry.Narrative {
		t.Fatalf("outcome = %+v", out)
	}
	if d := directed(t, out); d.Type != entry.Narrative || d.ActorID != innkeeper.ID || d.Think != "" || out.Directed.Think != "The scene has run its course." || out.ThreadID != th.ID || out.Scene.ID != sc.ID {
		t.Fatalf("direction = %+v entry = %+v", d, out.Directed)
	}
	if list, _ := entries.List(ctx, g.ID, th.ID, sc.ID); len(list) != 4 || list[0].Type != entry.Present || list[2].Type != entry.Direction || list[3].ID != out.Entry.ID {
		t.Fatalf("scene after advance = %+v", list)
	}
	if strings.Join(eng.asked, ",") != "You decide where thi,You write for one ac" {
		t.Fatalf("asked = %v", eng.asked)
	}
	for _, want := range []string{"THREADS\nDocks (active): at Tavern, with Innkeeper, Mara\n", "SCENE\nTAVERN\nPresent: Innkeeper, Mara\n\nINNKEEPER:\nRain.\n", "What happens next in Docks?"} {
		if !strings.Contains(eng.direct.User, want) {
			t.Fatalf("sheet missing %q:\n%s", want, eng.direct.User)
		}
	}
	if !strings.Contains(eng.direct.Grammar, `"split\n"`) || strings.Contains(eng.direct.Grammar, `"converge\n"`) || !strings.Contains(eng.direct.Grammar, `known ::= "Tavern"`) || !strings.Contains(eng.direct.Grammar, `" | "`) || !strings.Contains(eng.direct.Grammar, `tracked ::= "Innkeeper" | "Mara"`) || !strings.Contains(eng.direct.Grammar, `"next " present`) {
		t.Fatalf("grammar = %s", eng.direct.Grammar)
	}
	if cur, _ := roller.Turn(ctx, g.ID, th.ID); cur != nil {
		t.Fatalf("turn open after model entry: %+v", cur)
	}
	// The game's telling reaches the briefs: the director's role, and the voice for narration.
	if err := game.NewStore(db).SetSettings(ctx, g.ID, game.Settings{Director: "dungeon master", Voice: "second person, present"}); err != nil {
		t.Fatal(err)
	}
	if _, err := roller.Advance(ctx, g.ID, th.ID, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(eng.direct.System, "You decide where things go next in the active thread. Read the state and answer; the first line is one word:\nYou do this as the dungeon master.\n") {
		t.Fatalf("director brief = %q", eng.direct.System)
	}

	// User actor named: the direction is recorded and handed back as a turn, nothing else written.
	eng.moves = "narrative\nnext Mara\n"
	out, err = roller.Advance(ctx, g.ID, th.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Result != nil || out.Turn == nil || out.Turn.ActorID != mara.ID || !out.Turn.User || out.Turn.Think != "The scene has run its course." || out.Turn.Direction == nil || out.Turn.Direction.Type != entry.Narrative || out.Turn.EntryID != out.Directed.ID {
		t.Fatalf("outcome = %+v", out)
	}
	// Advancing again only repeats the open turn; the model is not asked and nothing is recorded twice.
	asked, n := len(eng.asked), count(sc.ID)
	if out, err = roller.Advance(ctx, g.ID, th.ID, nil); err != nil || out.Turn == nil || out.Turn.ActorID != mara.ID || out.Directed != nil || len(eng.asked) != asked || count(sc.ID) != n {
		t.Fatalf("repeat = %+v, %v, asked %d", out, err, len(eng.asked)-asked)
	}
	if cur, err := roller.Turn(ctx, g.ID, th.ID); err != nil || cur == nil || cur.ActorID != mara.ID || !cur.User || cur.Think == "" || cur.Direction == nil {
		t.Fatalf("turn = %+v, %v", cur, err)
	}
	// Mara's entry follows the direction and closes the turn; the direction stays in the record.
	if _, err := entries.Append(ctx, g.ID, th.ID, sc.ID, mara.ID, "", []entry.Segment{{Kind: entry.Speech, Text: "I know."}}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := roller.Turn(ctx, g.ID, th.ID); cur != nil || count(sc.ID) != n+1 {
		t.Fatalf("turn after answer = %+v, entries %d", cur, count(sc.ID))
	}
	// A pass drops the direction instead.
	if err := roller.Pass(ctx, g.ID, th.ID); !errors.Is(err, play.ErrNoTurn) {
		t.Fatalf("pass with no turn: err = %v", err)
	}
	if _, err := roller.Advance(ctx, g.ID, th.ID, nil); err != nil {
		t.Fatal(err)
	}
	n = count(sc.ID)
	if err := roller.Pass(ctx, g.ID, th.ID); err != nil {
		t.Fatal(err)
	}
	if cur, err := roller.Turn(ctx, g.ID, th.ID); err != nil || cur != nil || count(sc.ID) != n-1 {
		t.Fatalf("turn after pass = %+v, %v, entries %d", cur, err, count(sc.ID))
	}

	// What a model says after its answer is commentary, not answer.
	eng.moves = "narrative\nnext Innkeeper\n\nShe has been quiet; let him fill it.\n"
	if out, err := roller.Advance(ctx, g.ID, th.ID, nil); err != nil || out.Result == nil || out.Entry.ActorID != innkeeper.ID {
		t.Fatalf("with commentary = %+v, %v", out, err)
	}
	if err := roller.Pass(ctx, g.ID, th.ID); !errors.Is(err, play.ErrNoTurn) {
		t.Fatal(err)
	}
	// The director may not hand the first word to someone the move leaves out.
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Exit, []string{mara.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := roller.Advance(ctx, g.ID, th.ID, nil); err == nil || !strings.Contains(err.Error(), "Mara") {
		t.Fatalf("absent actor named: err = %v", err)
	}
	// With one actor present the director is still asked, and a refused answer records nothing.
	eng.moves = "narrative\nnext Innkeeper\n"
	asked, n = len(eng.asked), count(sc.ID)
	if out, err := roller.Advance(ctx, g.ID, th.ID, nil); err != nil || out.Result == nil || len(eng.asked) != asked+2 || count(sc.ID) != n+2 {
		t.Fatalf("single cast = %+v, %v, asked %d", out, err, len(eng.asked)-asked)
	}
	if strings.Contains(eng.direct.Grammar, `"split\n"`) {
		t.Fatalf("split offered to one: %s", eng.direct.Grammar)
	}

	// The director cuts: the direction ends the old scene, the entry opens the next.
	eng.moves = "cut\n-> Tavern\nwith Innkeeper\nnext Innkeeper\n"
	out, err = roller.Advance(ctx, g.ID, th.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := scenes.List(ctx, g.ID, th.ID)
	if len(all) != 2 || out.Result == nil || out.Scene.ID != all[1].ID || out.Entry.Type != entry.Cut || all[1].Opening != out.Entry.ID || out.Directed.SceneID != sc.ID {
		t.Fatalf("after cut: scenes = %+v, outcome = %+v", all, out)
	}
	if d := directed(t, out); d.Type != entry.Cut || d.PlaceID != tavern.ID {
		t.Fatalf("cut direction = %+v", d)
	}
	sc = all[1]

	// The director splits a user-controlled actor off: her turn carries the direction, place, link and all.
	if _, err := entries.Presence(ctx, g.ID, th.ID, sc.ID, entry.Enter, []string{mara.ID}); err != nil {
		t.Fatal(err)
	}
	eng.moves = "split\n=> The quay: a stone landing | down the steps from\nwith Mara\nnext Mara\n"
	out, err = roller.Advance(ctx, g.ID, th.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Turn == nil || out.Turn.ActorID != mara.ID || out.Turn.Direction.Type != entry.Split || out.Turn.Direction.NewPlace == nil || out.Turn.Direction.NewPlace.Name != "The quay" || out.Turn.Direction.Link != "down the steps from" || len(out.Turn.Direction.Cast) != 1 {
		t.Fatalf("split turn = %+v", out.Turn)
	}
	if again, err := roller.Advance(ctx, g.ID, th.ID, nil); err != nil || again.Turn.Direction.NewPlace == nil || again.Turn.Direction.NewPlace.Description != "a stone landing" || again.Turn.Direction.Link == "" {
		t.Fatalf("kept direction = %+v, %v", again, err)
	}
	if err := roller.Pass(ctx, g.ID, th.ID); err != nil {
		t.Fatal(err)
	}
	// A split that takes every tracked actor is a cut.
	eng.moves = "split\n-> Tavern\nwith Innkeeper, Mara\nnext Innkeeper\n"
	if out, err = roller.Advance(ctx, g.ID, th.ID, nil); err != nil || out.Entry.Type != entry.Cut {
		t.Fatalf("split of all = %+v, %v", out, err)
	}
	if d := directed(t, out); d.Type != entry.Cut || len(d.Cast) != 2 {
		t.Fatalf("split of all direction = %+v", d)
	}

	// Another thread's director converges on this one; its thread ends.
	harbour, _ := threads.Create(ctx, g.ID, "Harbour")
	if _, err := play.DoTransition(ctx, db, g.ID, harbour.ID, play.Transition{Direction: play.Direction{Type: entry.Cut, NewPlace: &subject.Input{Name: "The quay"}, Cast: []string{fen.ID}}, ActorID: fen.ID, Segments: rain}); err != nil {
		t.Fatal(err)
	}
	eng.moves = "converge\ninto Docks\nnext Fen\n"
	out, err = roller.Advance(ctx, g.ID, harbour.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eng.direct.Grammar, `thread ::= "Docks"`) || !strings.Contains(eng.direct.User, "Harbour (active): at The quay, with Fen\n") {
		t.Fatalf("converge offer: %+v", eng.direct)
	}
	all, _ = scenes.List(ctx, g.ID, th.ID)
	if out.Result == nil || out.ThreadID != th.ID || out.Entry.Type != entry.Converge || out.Scene.ID != all[len(all)-1].ID || len(out.Scene.Cast) != 3 {
		t.Fatalf("converge = %+v", out)
	}
	if d := directed(t, out); d.IntoThreadID != th.ID {
		t.Fatalf("converge direction = %+v", d)
	}
	if _, err := roller.Advance(ctx, g.ID, harbour.ID, nil); !errors.Is(err, thread.ErrEnded) {
		t.Fatalf("advance ended thread: err = %v", err)
	}
}

// stalled never answers until the context ends.
type stalled struct{}

func (stalled) Complete(ctx context.Context, _ llm.Request) (llm.Response, error) {
	<-ctx.Done()
	return llm.Response{}, ctx.Err()
}

func (stalled) Render(context.Context, llm.Request) (llm.Rendered, error) { return llm.Rendered{}, nil }

func TestCallLimitSurfaces(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	models, threads, subjects, scenes, entries := model.NewStore(db), thread.NewStore(db), subject.NewStore(db), scene.NewStore(db), entry.NewStore(db)
	g, _ := game.NewStore(db).Create(ctx, "game")
	th, _ := threads.Create(ctx, g.ID, "thread")
	tavern, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Tavern", Scene: true})
	innkeeper, _ := subjects.Create(ctx, g.ID, subject.Input{Name: "Innkeeper", Actor: true})
	m, _ := models.Create(ctx, model.Input{Name: "m", Engine: model.LlamaCpp, Endpoint: "http://h:1", Model: "x"})
	_ = models.SetCurrent(ctx, m.ID)
	res, _ := play.DoTransition(ctx, db, g.ID, th.ID, play.Transition{Direction: play.Direction{Type: entry.Cut, PlaceID: tavern.ID, Cast: []string{innkeeper.ID}}, ActorID: innkeeper.ID, Segments: []entry.Segment{{Kind: entry.Narration, Text: "Rain."}}})
	roller := &play.Roller{
		DB: db, Models: models, Threads: threads, Subjects: subjects, Scenes: scenes, Entries: entries,
		Engine: func(model.Model) (llm.Engine, error) { return stalled{}, nil },
		Log:    slog.Default(),
	}
	// A caller's own shorter deadline hits first and still reads as too long.
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := roller.Roll(short, g.ID, th.ID, res.Scene.ID, innkeeper.ID, nil); !errors.Is(err, play.ErrTooLong) {
		t.Fatalf("err = %v; want ErrTooLong", err)
	}
}
