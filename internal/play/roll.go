// Package play drives turns: it renders what an actor can see as a script,
// asks the active model for the actor's next entry, and records it.
package play

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/llm"
	"aidventure/internal/model"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

var (
	ErrNoModel = errors.New("no active model")
	ErrEmpty   = errors.New("model produced no entry")
	ErrTooLong = errors.New("model took too long")
)

// callLimit bounds one model call. A healthy turn takes seconds; a model
// stuck mulling runs for minutes, and dropping the call stops it server-side.
const callLimit = 3 * time.Minute

// complete runs one model call under the time limit.
func complete(ctx context.Context, eng llm.Engine, req llm.Request) (llm.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	res, err := eng.Complete(ctx, req)
	if errors.Is(err, context.DeadlineExceeded) {
		return llm.Response{}, fmt.Errorf("%w: no answer within %s", ErrTooLong, callLimit)
	}
	return res, err
}

type Roller struct {
	// DB is for transitions, which span stores in one transaction.
	DB       *sql.DB
	Models   *model.Store
	Threads  *thread.Store
	Subjects *subject.Store
	Scenes   *scene.Store
	Entries  *entry.Store
	// Engine builds the client for a model entry; tests substitute a fake.
	Engine func(model.Model) (llm.Engine, error)
	Log    *slog.Logger
}

// Preview is everything about a turn short of asking for it: the request the
// roller builds, and what the engine would send for it.
type Preview struct {
	Model    string       `json:"model"`
	Request  llm.Request  `json:"request"`
	Rendered llm.Rendered `json:"rendered"`
}

// Preview shows what the active model would see for actorID's next entry in the scene.
func (r *Roller) Preview(ctx context.Context, gameID, threadID, sceneID, actorID string) (Preview, error) {
	a, err := r.prepare(ctx, gameID, threadID, sceneID, actorID, Direction{Type: entry.Narrative})
	if err != nil {
		return Preview{}, err
	}
	rendered, err := a.eng.Render(ctx, a.req)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Model: a.model.Name, Request: a.req, Rendered: rendered}, nil
}

// Event is a piece of a turn as it is produced: phase "direct", "pick" or
// "entry", part "think" or "text".
type Event struct {
	Phase string `json:"phase"`
	Part  string `json:"part"`
	Delta string `json:"delta"`
}

// Sink receives Events; nil means nobody is watching.
type Sink func(Event)

func (s Sink) stream(phase string) func(part, delta string) {
	if s == nil {
		return nil
	}
	return func(part, delta string) { s(Event{Phase: phase, Part: part, Delta: delta}) }
}

// Roll has the active model write actorID's next entry in the scene and appends it.
func (r *Roller) Roll(ctx context.Context, gameID, threadID, sceneID, actorID string, sink Sink) (entry.Entry, error) {
	res, err := r.perform(ctx, gameID, threadID, sceneID, actorID, Direction{Type: entry.Narrative}, sink)
	return res.Entry, err
}

// perform has the active model write actorID's entry under a direction and
// records it where the direction lands it.
func (r *Roller) perform(ctx context.Context, gameID, threadID, sceneID, actorID string, dir Direction, sink Sink) (Result, error) {
	a, err := r.prepare(ctx, gameID, threadID, sceneID, actorID, dir)
	if err != nil {
		return Result{}, err
	}
	a.req.Stream = sink.stream("entry")
	res, err := complete(ctx, a.eng, a.req)
	if err != nil {
		return Result{}, err
	}
	r.Log.Info("rolled",
		"actor", a.actor.Name,
		"direction", dir.Type,
		"think_chars", len(res.Think),
		"prompt_tokens", res.PromptTokens,
		"cached_tokens", res.CachedTokens,
		"tokens", res.Tokens,
		"prompt_time", res.PromptTime,
		"generate_time", res.GenerateTime,
	)
	var segments []entry.Segment
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Text)), &segments); err != nil {
		return Result{}, fmt.Errorf("%s answered outside the schema: %v: %q", a.actor.Name, err, res.Text)
	}
	if len(segments) == 0 {
		return Result{}, fmt.Errorf("%w: %s", ErrEmpty, a.actor.Name)
	}
	var out Result
	if dir.Type == entry.Narrative {
		e, err := r.Entries.Append(ctx, gameID, threadID, sceneID, actorID, res.Think, segments)
		if err != nil {
			return Result{}, err
		}
		out = Result{Entry: e, Scene: a.scene, ThreadID: threadID}
	} else {
		if out, err = DoTransition(ctx, r.DB, gameID, threadID, Transition{Direction: dir, ActorID: actorID, Think: res.Think, Segments: segments}); err != nil {
			return Result{}, err
		}
	}
	return out, nil
}

// asking is a turn ready to be asked for: the model and engine to ask, the
// request, and the actor and scene it is for.
type asking struct {
	model model.Model
	eng   llm.Engine
	req   llm.Request
	actor subject.Subject
	scene scene.Scene
}

// engine resolves the active model and its client.
func (r *Roller) engine(ctx context.Context) (model.Model, llm.Engine, error) {
	id, err := r.Models.Current(ctx)
	if err != nil {
		return model.Model{}, nil, err
	}
	if id == "" {
		return model.Model{}, nil, ErrNoModel
	}
	m, err := r.Models.Get(ctx, id)
	if err != nil {
		return model.Model{}, nil, err
	}
	eng, err := r.Engine(m)
	if err != nil {
		return model.Model{}, nil, err
	}
	return m, eng, nil
}

// prepare renders the request for an actor's turn in the scene, under a direction.
func (r *Roller) prepare(ctx context.Context, gameID, threadID, sceneID, actorID string, dir Direction) (asking, error) {
	m, eng, err := r.engine(ctx)
	if err != nil {
		return asking{}, err
	}
	subjects, err := r.Subjects.List(ctx, gameID)
	if err != nil {
		return asking{}, err
	}
	byID := map[string]subject.Subject{}
	for _, s := range subjects {
		byID[s.ID] = s
	}
	actor, ok := byID[actorID]
	if !ok {
		return asking{}, fmt.Errorf("%w: actor %s in game %s", subject.ErrNotFound, actorID, gameID)
	}
	if !actor.Actor {
		return asking{}, fmt.Errorf("%w: %s is not flagged as an actor", entry.ErrNotActor, actor.Name)
	}
	scenes, err := r.Scenes.List(ctx, gameID, threadID)
	if err != nil {
		return asking{}, err
	}
	// The script runs from the thread's first scene to the one being played;
	// scenes after it, if any, are not this turn's past.
	at := slices.IndexFunc(scenes, func(sc scene.Scene) bool { return sc.ID == sceneID })
	if at < 0 {
		return asking{}, fmt.Errorf("%w: scene %s in thread %s", scene.ErrNotFound, sceneID, threadID)
	}
	scenes = scenes[:at+1]
	entries, err := r.Entries.ListThread(ctx, gameID, threadID)
	if err != nil {
		return asking{}, err
	}
	links, err := r.Subjects.Links(ctx, gameID)
	if err != nil {
		return asking{}, err
	}
	told, err := game.ReadSettings(ctx, r.DB, gameID)
	if err != nil {
		return asking{}, err
	}
	ask := "Next entry for " + actor.Name + "."
	if dir.Type != entry.Narrative {
		var into *scene.Scene
		if dir.Type == entry.Converge {
			sc, err := scene.Latest(ctx, r.DB, gameID, dir.IntoThreadID)
			if err != nil {
				return asking{}, err
			}
			into = &sc
		}
		ask = describe(dir, byID, byID[scenes[at].PlaceID].Name, into) + "Next entry for " + actor.Name + ", now there."
	}
	return asking{model: m, eng: eng, req: render(actor, ask, scenes, subjects, entries, links, byID, told.Voice), actor: actor, scene: scenes[at]}, nil
}

const system = `You write for one actor, the one named, and for nobody else. Write only the next entry, from that actor's point of view, as JSON in the schema given: one to five beats, each a kind and a text.

The kinds:
action     what the actor does
speech     what the actor says aloud
thought    what the actor thinks, unheard by anyone
narration  what is seen, from the actor's point of view

Write what the moment needs, usually a line or three, and stop. Do not plan or count lines.
Never write for other actors, and never decide what they do or say.
When the entry follows a move, the actor is already there: write the arrival.
Think briefly, a few sentences at most, then write.`

// render lays out the thread by stability: places and characters first, then
// the script scene by scene, then the ask. Cache hits depend on that order.
func render(actor subject.Subject, ask string, scenes []scene.Scene, subjects []subject.Subject, entries []entry.Entry, links []subject.Link, byID map[string]subject.Subject, voice string) llm.Request {
	var b strings.Builder
	roster(&b, subjects, links)
	b.WriteString("\nSCRIPT\n")
	for _, sc := range scenes {
		writeScene(&b, sc, entries, byID)
	}
	b.WriteString("\n" + ask)
	brief := system
	if voice != "" {
		brief += "\nNarration lines are written in this voice: " + voice
	}
	return llm.Request{
		System: brief,
		User:   b.String(),
		Schema: entry.Schema(),
	}
}

// roster lists the places and characters of the game, and how they stand to each other.
func roster(b *strings.Builder, subjects []subject.Subject, links []subject.Link) {
	b.WriteString("PLACES\n")
	for _, s := range subjects {
		if s.Scene {
			b.WriteString(s.Name + ": " + s.Description + "\n")
		}
	}
	b.WriteString("\nCHARACTERS\n")
	for _, s := range subjects {
		if s.Actor {
			name := s.Name
			if s.Tracked {
				name += " (tracked)"
			}
			b.WriteString(name + ": " + s.Description + "\n")
		}
	}
	if len(links) == 0 {
		return
	}
	byID := map[string]string{}
	for _, s := range subjects {
		byID[s.ID] = s.Name
	}
	b.WriteString("\nLINKS\n")
	for _, l := range links {
		b.WriteString(byID[l.FromID] + " " + l.Relation + " " + byID[l.ToID] + ".\n")
	}
}

// writeScene lays out one scene: its slug, then its entries as they are: who
// is there as it opens, the beats, and who came and went between them.
// Directions are the camera, not the story, and stay out.
func writeScene(b *strings.Builder, sc scene.Scene, entries []entry.Entry, byID map[string]subject.Subject) {
	names := func(ids []string) string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, byID[id].Name)
		}
		return strings.Join(out, ", ")
	}
	heading := strings.ToUpper(byID[sc.PlaceID].Name)
	if sc.Title != "" {
		heading += ": " + sc.Title
	}
	b.WriteString("\n" + heading + "\n")
	for _, e := range entries {
		if e.SceneID != sc.ID {
			continue
		}
		switch e.Type {
		case entry.Direction:
		case entry.Present:
			if len(e.Actors) > 0 {
				b.WriteString("Present: " + names(e.Actors) + "\n")
			}
		case entry.Enter:
			b.WriteString("\nEnter " + names(e.Actors) + ".\n")
		case entry.Exit:
			b.WriteString("\nExit " + names(e.Actors) + ".\n")
		default:
			b.WriteString("\n" + cue(byID[e.ActorID]) + entry.FormatScript(e.Segments) + "\n")
		}
	}
}

func cue(s subject.Subject) string {
	name := s.Name
	if name == "" {
		name = "?"
	}
	return strings.ToUpper(name) + ":\n"
}
