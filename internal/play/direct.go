package play

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/llm"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

// The director's brief: the moves a scene has, and how to answer with one.
const directing = `You decide where things go next in the active thread. Read the state and answer; the first line is one word:
narrative   the scene goes on where it is
cut         actors can move on to another place in this narrative thread, and a new scene begins there with the cut
split       some tracked actors leave for another place and go on in a new thread; the rest stay here. Only tracked actors, the ones the story follows, are followed by a split; others may go along or stay
converge    those present go to where another thread is into it's scene, and the threads become one

Decision should be guided by narrative coherence - what makes sense to happen next.

After cut or split, say where and who:
-> Place name              a place from PLACES
=> New place: what it is | how it stands to the current place   a place that does not exist yet; you make it, and say where it is
with Name, Name            after a cut, who is present there; after a split, who leaves

Who is present is a matter of staging.
The cast is who can act in the scene.
An actor left out has not gone anywhere, and may be present in any later scene.
Story follows tracked actors, and untracked actors are not important.

After converge, say which thread:
into Thread name

Last, say who acts first:
next Name                  one of those present after the move; do not let one actor act twice in a row without reason

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
	roster(&b, st.subjects, st.links)
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

// grammar offers the director only the moves the scene has: a split needs two
// tracked actors to part, and the first to leave must be one of them; a
// converge needs another thread to join.
func (st stage) grammar() string {
	var places, actors, present, tracked, others []string
	for _, s := range st.subjects {
		if s.Scene {
			places = append(places, s.Name)
		}
		if s.Actor {
			actors = append(actors, s.Name)
		}
	}
	for _, s := range st.present() {
		present = append(present, s.Name)
	}
	for _, s := range st.tracked() {
		tracked = append(tracked, s.Name)
	}
	for _, t := range st.threads {
		if t.ID != st.threadID {
			others = append(others, t.Name)
		}
	}
	var b strings.Builder
	// Each move ends with who acts first; a cut may hand the first word to anyone it brings in.
	b.WriteString(`root ::= "narrative\n" "next " present "\n" | "cut\n" place "\n" "with " actor (", " actor)* "\n" "next " actor "\n"`)
	if len(tracked) > 1 {
		b.WriteString(` | "split\n" place "\n" "with " tracked (", " present)* "\n" "next " present "\n"`)
	}
	if len(others) > 0 {
		b.WriteString(` | "converge\n" "into " thread "\n" "next " present "\n"`)
	}
	b.WriteString("\nplace ::= \"=> \" [^\\n:|]+ \": \" [^\\n|]+ \" | \" [^\\n]+")
	if len(places) > 0 {
		b.WriteString(" | \"-> \" known\nknown ::= " + alternatives(places))
	}
	b.WriteString("\nactor ::= " + alternatives(actors) + "\n")
	b.WriteString("present ::= " + alternatives(present) + "\n")
	if len(tracked) > 1 {
		b.WriteString("tracked ::= " + alternatives(tracked) + "\n")
	}
	if len(others) > 0 {
		b.WriteString("thread ::= " + alternatives(others) + "\n")
	}
	return b.String()
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
		System:  roleOf(directing, st.told.Director),
		User:    st.sheet() + "\nWhat happens next in " + active + "?",
		Grammar: st.grammar(),
		Stream:  sink.stream("direct"),
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

// direction reads the director's answer back into ids: the move, then who acts
// first under it, who must be one of those the move allows. The grammar only
// offers real names, so a miss is a bug, not a model error.
func (st stage) direction(text string) (Direction, error) {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	// Without a grammar a model may keep talking after its answer; the answer
	// ends at the next line, and what follows is commentary.
	at := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "next ") })
	if at < 1 {
		return Direction{}, fmt.Errorf("director did not say who acts: %q", text)
	}
	who := strings.TrimSpace(lines[at][5:])
	d, err := st.move(lines[:at])
	if err != nil {
		return Direction{}, err
	}
	for _, s := range st.candidates(d) {
		if strings.EqualFold(s.Name, who) {
			d.ActorID = s.ID
			return d, nil
		}
	}
	return Direction{}, fmt.Errorf("director had %q act first, who is not among those the %s allows", who, d.Type)
}

// move reads the lines that say where the play goes. A split that takes every
// tracked actor is a cut: nobody left behind is being followed.
func (st stage) move(lines []string) (Direction, error) {
	d := Direction{Type: entry.Type(lines[0])}
	switch d.Type {
	case entry.Narrative:
		return d, nil
	case entry.Cut, entry.Split:
		if len(lines) < 3 {
			return Direction{}, fmt.Errorf("director gave %q without a place and a cast", d.Type)
		}
		switch {
		case strings.HasPrefix(lines[1], "-> "):
			name := strings.TrimSpace(lines[1][3:])
			id, ok := st.byName(name, func(s subject.Subject) bool { return s.Scene })
			if !ok {
				return Direction{}, fmt.Errorf("director cut to unknown place %q", name)
			}
			d.PlaceID = id
		case strings.HasPrefix(lines[1], "=> "):
			name, rest, _ := strings.Cut(lines[1][3:], ":")
			desc, link, _ := strings.Cut(rest, "|")
			d.NewPlace = &subject.Input{Name: strings.TrimSpace(name), Description: strings.TrimSpace(desc), Scene: true}
			d.Link = strings.TrimSpace(link)
		default:
			return Direction{}, fmt.Errorf("director gave no place: %q", lines[1])
		}
		for _, name := range strings.Split(strings.TrimPrefix(lines[2], "with "), ",") {
			id, ok := st.byName(name, func(s subject.Subject) bool { return s.Actor })
			if !ok {
				return Direction{}, fmt.Errorf("director cast unknown actor %q", name)
			}
			if !slices.Contains(d.Cast, id) {
				d.Cast = append(d.Cast, id)
			}
		}
		if d.Type == entry.Split && !slices.ContainsFunc(st.tracked(), func(s subject.Subject) bool { return !slices.Contains(d.Cast, s.ID) }) {
			d.Type = entry.Cut
		}
		return d, nil
	case entry.Converge:
		if len(lines) < 2 {
			return Direction{}, fmt.Errorf("director converged without a thread")
		}
		name := strings.TrimSpace(strings.TrimPrefix(lines[1], "into "))
		for _, t := range st.threads {
			if t.ID != st.threadID && strings.EqualFold(t.Name, name) {
				d.IntoThreadID = t.ID
				return d, nil
			}
		}
		return Direction{}, fmt.Errorf("director converged into unknown thread %q", name)
	}
	return Direction{}, fmt.Errorf("director answered %q", lines[0])
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
