package play

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"aidventure/internal/attribute"
	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/llm"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

// The director's brief: the moves a scene has, and how to answer with one.
const directing = `You decide where things go next in the active thread. Read the state and answer in the schema given. The move is one of:
narrative   the scene goes on where it is
cut         actors can move on to another place in this narrative thread, and a new scene begins there with the cut
split       some tracked actors leave for another place and go on in a new thread; the rest stay here. Only tracked actors, the ones the story follows, are followed by a split; others may go along or stay
converge    those present go to where another thread is into it's scene, and the threads become one

Decision should be guided by narrative coherence - what makes sense to happen next.

The rest of the answer:
place      for a cut or split to a place from PLACES, its name; otherwise null
newPlace   for a cut or split to a place that does not exist yet: its name, what it is, and link, how it stands to the current place; otherwise null
with       after a cut, who is present there; after a split, who leaves; otherwise empty
into       for a converge, the thread; otherwise null
next       who acts first, one of those present after the move. Do not let one actor act twice in a row without reason.

Who is present is a matter of staging.
The cast is who can act in the scene.
An actor left out has not gone anywhere, and may be present in any later scene.
Story follows tracked actors, and untracked actors are not important.

Think briefly, a few sentences at most, then answer.`

// roleOf gives the director's brief the role the game casts it in, when the
// game names one: the first line says what to do, the role says as whom.
func roleOf(brief, role string) string {
	if role == "" {
		return brief
	}
	first, rest, _ := strings.Cut(brief, "\n")
	return first + "\nYou do this as the " + role + ".\n" + rest
}

// stage is the state of play as the director reads it: every open thread at
// its current scene, and the active scene in full.
type stage struct {
	threadID string
	subjects []subject.Subject
	byID     map[string]subject.Subject
	// threads are the open ones, the active among them; at is each one's current scene.
	threads []thread.Thread
	at      map[string]scene.Scene
	scene   scene.Scene
	entries []entry.Entry
	links   []subject.Link
	attrs   []attribute.Attribute
	told    game.Settings
}

// stage reads the state of play around the thread's current scene.
func (r *Roller) stage(ctx context.Context, gameID, threadID string) (stage, error) {
	st := stage{threadID: threadID, byID: map[string]subject.Subject{}, at: map[string]scene.Scene{}}
	var err error
	if st.told, err = game.ReadSettings(ctx, r.DB, gameID); err != nil {
		return stage{}, err
	}
	if st.subjects, err = r.Subjects.List(ctx, gameID); err != nil {
		return stage{}, err
	}
	for _, s := range st.subjects {
		st.byID[s.ID] = s
	}
	if st.links, err = r.Subjects.Links(ctx, gameID); err != nil {
		return stage{}, err
	}
	if st.attrs, err = attribute.Read(ctx, r.DB, gameID); err != nil {
		return stage{}, err
	}
	threads, err := r.Threads.List(ctx, gameID)
	if err != nil {
		return stage{}, err
	}
	for _, t := range threads {
		if t.EndedByEntryID != "" {
			continue
		}
		scenes, err := r.Scenes.List(ctx, gameID, t.ID)
		if err != nil {
			return stage{}, err
		}
		if len(scenes) == 0 {
			continue
		}
		st.threads = append(st.threads, t)
		st.at[t.ID] = scenes[len(scenes)-1]
	}
	var ok bool
	if st.scene, ok = st.at[threadID]; !ok {
		return stage{}, fmt.Errorf("%w: thread %s", ErrNoScene, threadID)
	}
	if st.entries, err = r.Entries.List(ctx, gameID, threadID, st.scene.ID); err != nil {
		return stage{}, err
	}
	return st, nil
}

// present is who in the scene can act.
func (st stage) present() []subject.Subject {
	var cast []subject.Subject
	for _, id := range st.scene.Cast {
		if s, ok := st.byID[id]; ok && s.Actor {
			cast = append(cast, s)
		}
	}
	return cast
}

// tracked is who in the scene the story follows.
func (st stage) tracked() []subject.Subject {
	var cast []subject.Subject
	for _, s := range st.present() {
		if s.Tracked {
			cast = append(cast, s)
		}
	}
	return cast
}

// sheet lays out the state of play for the director and the pick.
func (st stage) sheet() string {
	var b strings.Builder
	roster(&b, st.attrs, st.subjects, st.links)
	b.WriteString("\nTHREADS\n")
	for _, t := range st.threads {
		sc := st.at[t.ID]
		b.WriteString(t.Name)
		if t.ID == st.threadID {
			b.WriteString(" (active)")
		}
		b.WriteString(": at " + st.byID[sc.PlaceID].Name + ", with " + strings.Join(st.names(sc.Cast), ", ") + "\n")
	}
	b.WriteString("\nSCENE")
	writeScene(&b, st.scene, st.entries, st.byID)
	return b.String()
}

func (st stage) names(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, st.byID[id].Name)
	}
	return out
}

// schema is the shape of the director's answer, offering only the moves the
// scene has and only real names: a split needs two tracked actors to part, a
// converge needs another thread to join. Every field is present, null or
// empty when the move has no use for it, which is what strict structured
// output asks for.
func (st stage) schema() map[string]any {
	var places, actors, others []string
	for _, s := range st.subjects {
		if s.Scene {
			places = append(places, s.Name)
		}
		if s.Actor {
			actors = append(actors, s.Name)
		}
	}
	for _, t := range st.threads {
		if t.ID != st.threadID {
			others = append(others, t.Name)
		}
	}
	moves := []string{string(entry.Narrative), string(entry.Cut)}
	if len(st.tracked()) > 1 {
		moves = append(moves, string(entry.Split))
	}
	if len(others) > 0 {
		moves = append(moves, string(entry.Converge))
	}
	oneOf := func(names []string) map[string]any {
		if len(names) == 0 {
			return map[string]any{"type": "null"}
		}
		return map[string]any{"anyOf": []any{map[string]any{"enum": names}, map[string]any{"type": "null"}}}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"move":  map[string]any{"enum": moves},
			"place": oneOf(places),
			"newPlace": map[string]any{"anyOf": []any{
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"link":        map[string]any{"type": "string"},
					},
					"required":             []string{"name", "description", "link"},
					"additionalProperties": false,
				},
				map[string]any{"type": "null"},
			}},
			"with": map[string]any{"type": "array", "items": map[string]any{"enum": actors}},
			"into": oneOf(others),
			"next": map[string]any{"enum": actors},
		},
		"required":             []string{"move", "place", "newPlace", "with", "into", "next"},
		"additionalProperties": false,
	}
}

// direct asks the model what happens next in the active thread.
func (r *Roller) direct(ctx context.Context, st stage, sink Sink) (Direction, error) {
	_, eng, err := r.engine(ctx)
	if err != nil {
		return Direction{}, err
	}
	active := ""
	for _, t := range st.threads {
		if t.ID == st.threadID {
			active = t.Name
		}
	}
	req := llm.Request{
		System: roleOf(directing, st.told.Director),
		User:   st.sheet() + "\nWhat happens next in " + active + "?",
		Schema: st.schema(),
		Stream: sink.stream("direct"),
	}
	res, err := complete(ctx, eng, req)
	if err != nil {
		return Direction{}, err
	}
	d, err := st.direction(res.Text)
	if err != nil {
		return Direction{}, err
	}
	d.Think = res.Think
	r.Log.Info("directed", "type", d.Type, "tokens", res.Tokens, "think_chars", len(res.Think), "generate_time", res.GenerateTime)
	return d, nil
}

// answer is the director's decision as the schema shapes it.
type answer struct {
	Move     string  `json:"move"`
	Place    *string `json:"place"`
	NewPlace *struct {
		Name, Description, Link string
	} `json:"newPlace"`
	With []string `json:"with"`
	Into *string  `json:"into"`
	Next string   `json:"next"`
}

// direction reads the director's answer back into ids: the move, then who acts
// first under it, who must be one of those the move allows. The schema only
// offers real names, so a miss is a bug, not a model error. A split that
// takes every tracked actor is a cut: nobody left behind is being followed.
func (st stage) direction(text string) (Direction, error) {
	var a answer
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &a); err != nil {
		return Direction{}, fmt.Errorf("director answered outside the schema: %v: %q", err, text)
	}
	d := Direction{Type: entry.Type(a.Move)}
	switch d.Type {
	case entry.Narrative:
	case entry.Cut, entry.Split:
		switch {
		case a.Place != nil && a.NewPlace == nil:
			id, ok := st.byName(*a.Place, func(s subject.Subject) bool { return s.Scene })
			if !ok {
				return Direction{}, fmt.Errorf("director %s to unknown place %q", d.Type, *a.Place)
			}
			d.PlaceID = id
		case a.NewPlace != nil && a.Place == nil:
			d.NewPlace = &subject.Input{Name: strings.TrimSpace(a.NewPlace.Name), Description: strings.TrimSpace(a.NewPlace.Description), Scene: true}
			d.Link = strings.TrimSpace(a.NewPlace.Link)
		default:
			return Direction{}, fmt.Errorf("director gave a %s with no place, or both a place and a new one", d.Type)
		}
		for _, name := range a.With {
			id, ok := st.byName(name, func(s subject.Subject) bool { return s.Actor })
			if !ok {
				return Direction{}, fmt.Errorf("director cast unknown actor %q", name)
			}
			if !slices.Contains(d.Cast, id) {
				d.Cast = append(d.Cast, id)
			}
		}
		if len(d.Cast) == 0 {
			return Direction{}, fmt.Errorf("director gave a %s with nobody in it", d.Type)
		}
		if d.Type == entry.Split && !slices.ContainsFunc(st.tracked(), func(s subject.Subject) bool { return !slices.Contains(d.Cast, s.ID) }) {
			d.Type = entry.Cut
		}
	case entry.Converge:
		if a.Into == nil {
			return Direction{}, fmt.Errorf("director converged without a thread")
		}
		for _, t := range st.threads {
			if t.ID != st.threadID && strings.EqualFold(t.Name, *a.Into) {
				d.IntoThreadID = t.ID
			}
		}
		if d.IntoThreadID == "" {
			return Direction{}, fmt.Errorf("director converged into unknown thread %q", *a.Into)
		}
	default:
		return Direction{}, fmt.Errorf("director answered with move %q", a.Move)
	}
	for _, s := range st.candidates(d) {
		if strings.EqualFold(s.Name, a.Next) {
			d.ActorID = s.ID
			return d, nil
		}
	}
	return Direction{}, fmt.Errorf("director had %q act first, who is not among those the %s allows", a.Next, d.Type)
}

func (st stage) byName(name string, want func(subject.Subject) bool) (string, bool) {
	for _, s := range st.subjects {
		if want(s) && strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(name)) {
			return s.ID, true
		}
	}
	return "", false
}

// candidates is who may write the entry a direction calls for: after a cut,
// anyone present there; after a split, one who leaves; otherwise anyone here.
func (st stage) candidates(d Direction) []subject.Subject {
	switch d.Type {
	case entry.Cut, entry.Split:
		var cast []subject.Subject
		for _, id := range d.Cast {
			if s, ok := st.byID[id]; ok && s.Actor {
				cast = append(cast, s)
			}
		}
		return cast
	}
	return st.present()
}

// describe says what a direction does, for whoever writes under it. A new
// place is said in terms of the place left, from; a converge needs the scene
// being joined, into.
func describe(d Direction, byID map[string]subject.Subject, from string, into *scene.Scene) string {
	names := func(ids []string) string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, byID[id].Name)
		}
		return strings.Join(out, ", ")
	}
	where := ""
	if d.NewPlace != nil {
		where = d.NewPlace.Name
		if d.NewPlace.Description != "" {
			where += ", " + d.NewPlace.Description
		}
		if d.Link != "" && from != "" {
			where += ", " + d.Link + " " + from
		}
	} else if d.PlaceID != "" {
		where = byID[d.PlaceID].Name
	}
	switch d.Type {
	case entry.Cut:
		return "Cut to " + where + ". Present: " + names(d.Cast) + ".\n"
	case entry.Split:
		return names(d.Cast) + " leave for " + where + "; the others stay behind.\n"
	case entry.Converge:
		if into == nil {
			return ""
		}
		return "Arrival at " + byID[into.PlaceID].Name + ", where " + names(into.Cast) + " are.\n"
	}
	return ""
}
