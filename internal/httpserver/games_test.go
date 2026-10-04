package httpserver_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/httpserver"
	"aidventure/internal/llm"
	"aidventure/internal/model"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/store"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

// scripted is an engine that keeps every scene going and answers every entry
// with one fixed script.
type scripted string

func (s scripted) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	if strings.HasPrefix(req.System, "You decide where") {
		return llm.Response{Text: `{"move":"narrative","place":null,"newPlace":null,"with":[],"into":null,"next":"Mara"}`}, nil
	}
	if req.Stream != nil {
		req.Stream("text", string(s))
	}
	return llm.Response{Text: string(s)}, nil
}

func (scripted) Render(_ context.Context, req llm.Request) (llm.Rendered, error) {
	return llm.Rendered{Prompt: req.User}, nil
}

func newServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	deps := httpserver.Deps{
		Games:    game.NewStore(db),
		Threads:  thread.NewStore(db),
		Subjects: subject.NewStore(db),
		Scenes:   scene.NewStore(db),
		Entries:  entry.NewStore(db),
		Models:   model.NewStore(db),
		DB:       db,
	}
	deps.Roller = &play.Roller{
		DB: db, Models: deps.Models, Threads: deps.Threads, Subjects: deps.Subjects, Scenes: deps.Scenes, Entries: deps.Entries,
		Engine: func(model.Model) (llm.Engine, error) { return scripted(`[{"kind":"speech","text":"Rolled."}]`), nil },
		Log:    slog.Default(),
	}
	return httptest.NewServer(httpserver.New(slog.Default(), http.NotFoundHandler(), deps))
}

func TestGamesFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()

	call := caller(t, srv)

	if status, body := call("POST", "/api/games", `{"name":"  "}`); status != http.StatusBadRequest {
		t.Fatalf("blank name: %d %s", status, body)
	}
	status, body := call("POST", "/api/games", `{"name":"Harbour"}`)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	var created game.Game
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Name != "Harbour" {
		t.Fatalf("create body: %s (%v)", body, err)
	}

	if status, body := call("GET", "/api/selection", ""); status != http.StatusOK || body != `{"gameId":null}` {
		t.Fatalf("empty selection: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/selection", `{"gameId":"nope"}`); status != http.StatusNotFound {
		t.Fatalf("select unknown: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/selection", `{"gameId":"`+created.ID+`"}`); status != http.StatusNoContent {
		t.Fatalf("select: %d %s", status, body)
	}
	if status, body := call("GET", "/api/selection", ""); status != http.StatusOK || body != `{"gameId":"`+created.ID+`"}` {
		t.Fatalf("selection: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/games/nope", `{"name":"Port"}`); status != http.StatusNotFound {
		t.Fatalf("rename unknown game: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/games/"+created.ID, `{"name":" "}`); status != http.StatusBadRequest {
		t.Fatalf("rename to blank: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/games/"+created.ID, `{"name":"Port"}`); status != http.StatusNoContent {
		t.Fatalf("rename game: %d %s", status, body)
	}
	if status, body := call("GET", "/api/games", ""); status != http.StatusOK || !strings.Contains(body, `"name":"Port"`) {
		t.Fatalf("games after rename: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/games/nope/settings", `{"director":"x","voice":"y"}`); status != http.StatusNotFound {
		t.Fatalf("settings of unknown game: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/games/"+created.ID+"/settings", `{"director":"dungeon master","voice":"third person, past"}`); status != http.StatusNoContent {
		t.Fatalf("settings: %d %s", status, body)
	}
	if status, body := call("GET", "/api/games", ""); status != http.StatusOK || !strings.Contains(body, `"director":"dungeon master","voice":"third person, past"`) {
		t.Fatalf("games after settings: %d %s", status, body)
	}
}

func TestThreadsFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()
	call := caller(t, srv)

	if status, body := call("GET", "/api/games/nope/threads", ""); status != http.StatusNotFound {
		t.Fatalf("threads of unknown game: %d %s", status, body)
	}
	_, body := call("POST", "/api/games", `{"name":"Harbour"}`)
	var g game.Game
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatal(err)
	}
	base := "/api/games/" + g.ID

	if status, body := call("GET", base+"/threads", ""); status != http.StatusOK || body != "[]" {
		t.Fatalf("empty threads: %d %s", status, body)
	}
	status, body := call("POST", base+"/threads", `{"name":"The docks"}`)
	if status != http.StatusCreated {
		t.Fatalf("create thread: %d %s", status, body)
	}
	var created thread.Thread
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.GameID != g.ID {
		t.Fatalf("create thread body: %s (%v)", body, err)
	}
	if status, body := call("GET", base+"/selection", ""); status != http.StatusOK || body != `{"threadId":null}` {
		t.Fatalf("empty selection: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/selection", `{"threadId":"nope"}`); status != http.StatusNotFound {
		t.Fatalf("select unknown thread: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/selection", `{"threadId":"`+created.ID+`"}`); status != http.StatusNoContent {
		t.Fatalf("select thread: %d %s", status, body)
	}
	if status, body := call("GET", base+"/selection", ""); status != http.StatusOK || body != `{"threadId":"`+created.ID+`"}` {
		t.Fatalf("selection: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/threads/nope", `{"name":"Quay"}`); status != http.StatusNotFound {
		t.Fatalf("rename unknown thread: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/threads/"+created.ID, `{"name":"Quay"}`); status != http.StatusNoContent {
		t.Fatalf("rename thread: %d %s", status, body)
	}
	if status, body := call("GET", base+"/threads", ""); status != http.StatusOK || !strings.Contains(body, `"name":"Quay"`) {
		t.Fatalf("threads after rename: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/threads/nope", ""); status != http.StatusNotFound {
		t.Fatalf("delete unknown thread: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/threads/"+created.ID, ""); status != http.StatusNoContent {
		t.Fatalf("delete thread: %d %s", status, body)
	}
	if status, body := call("GET", base+"/threads", ""); status != http.StatusOK || body != "[]" {
		t.Fatalf("threads after delete: %d %s", status, body)
	}
	if status, body := call("GET", base+"/selection", ""); status != http.StatusOK || body != `{"threadId":null}` {
		t.Fatalf("selection after delete: %d %s", status, body)
	}
}

// caller returns a helper that sends a request and returns the status and trimmed body.
func caller(t *testing.T, srv *httptest.Server) func(method, path, body string) (int, string) {
	return func(method, path, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, strings.TrimSpace(string(out))
	}
}

func TestSubjectsFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()
	call := caller(t, srv)

	_, body := call("POST", "/api/games", `{"name":"Harbour"}`)
	var g game.Game
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatal(err)
	}
	base := "/api/games/" + g.ID + "/subjects"

	if status, body := call("POST", base, `{"name":"","description":"x"}`); status != http.StatusBadRequest {
		t.Fatalf("blank name: %d %s", status, body)
	}
	status, body := call("POST", base, `{"name":"Barrel","description":"Oak."}`)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	var created subject.Subject
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if status, body := call("PUT", base+"/nope", `{"name":"x","description":""}`); status != http.StatusNotFound {
		t.Fatalf("update unknown: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/"+created.ID, `{"name":"Barrel","description":"Oak, empty.","actor":true,"scene":true,"user":true}`); status != http.StatusNoContent {
		t.Fatalf("update: %d %s", status, body)
	}
	status, body = call("GET", base, "")
	if status != http.StatusOK || !strings.Contains(body, `"description":"Oak, empty.","actor":true,"scene":true,"user":true`) {
		t.Fatalf("list: %d %s", status, body)
	}

	// Links join subjects of the game and read from one to the other.
	_, body = call("POST", base, `{"name":"Quay","description":"","scene":true}`)
	var quay subject.Subject
	if err := json.Unmarshal([]byte(body), &quay); err != nil {
		t.Fatal(err)
	}
	links := "/api/games/" + g.ID + "/links"
	if status, body := call("POST", links, `{"fromId":"`+created.ID+`","toId":"`+created.ID+`","relation":"is"}`); status != http.StatusBadRequest {
		t.Fatalf("self link: %d %s", status, body)
	}
	if status, body := call("POST", links, `{"fromId":"`+created.ID+`","toId":"nope","relation":"on"}`); status != http.StatusNotFound {
		t.Fatalf("link to unknown: %d %s", status, body)
	}
	status, body = call("POST", links, `{"fromId":"`+created.ID+`","toId":"`+quay.ID+`","relation":"stands on"}`)
	if status != http.StatusCreated {
		t.Fatalf("link: %d %s", status, body)
	}
	var l subject.Link
	if err := json.Unmarshal([]byte(body), &l); err != nil {
		t.Fatal(err)
	}
	if status, body := call("PUT", links+"/"+l.ID, `{"fromId":"`+quay.ID+`","toId":"`+created.ID+`","relation":"holds"}`); status != http.StatusNoContent {
		t.Fatalf("update link: %d %s", status, body)
	}
	if status, body := call("GET", links, ""); status != http.StatusOK || !strings.Contains(body, `"relation":"holds"`) {
		t.Fatalf("links: %d %s", status, body)
	}
	if status, body := call("DELETE", links+"/nope", ""); status != http.StatusNotFound {
		t.Fatalf("delete unknown link: %d %s", status, body)
	}
	if status, body := call("DELETE", links+"/"+l.ID, ""); status != http.StatusNoContent {
		t.Fatalf("delete link: %d %s", status, body)
	}
	if status, body := call("GET", links, ""); status != http.StatusOK || body != "[]" {
		t.Fatalf("links after delete: %d %s", status, body)
	}
}

func TestScenesFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()
	call := caller(t, srv)

	_, body := call("POST", "/api/games", `{"name":"Harbour"}`)
	var g game.Game
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatal(err)
	}
	_, body = call("POST", "/api/games/"+g.ID+"/threads", `{"name":"Docks"}`)
	var th thread.Thread
	if err := json.Unmarshal([]byte(body), &th); err != nil {
		t.Fatal(err)
	}
	_, body = call("POST", "/api/games/"+g.ID+"/subjects", `{"name":"Tavern","description":"","actor":false,"scene":true}`)
	var tavern subject.Subject
	if err := json.Unmarshal([]byte(body), &tavern); err != nil {
		t.Fatal(err)
	}
	_, body = call("POST", "/api/games/"+g.ID+"/subjects", `{"name":"Barrel","description":"","actor":false,"scene":false}`)
	var barrel subject.Subject
	if err := json.Unmarshal([]byte(body), &barrel); err != nil {
		t.Fatal(err)
	}
	_, body = call("POST", "/api/games/"+g.ID+"/subjects", `{"name":"Narrator","description":"","actor":true,"scene":false,"user":false}`)
	var narrator subject.Subject
	if err := json.Unmarshal([]byte(body), &narrator); err != nil {
		t.Fatal(err)
	}
	base := "/api/games/" + g.ID + "/threads/" + th.ID + "/scenes"
	move := "/api/games/" + g.ID + "/threads/" + th.ID + "/transitions"
	cut := func(place string) string {
		return `{"direction":{"type":"cut","placeId":"` + place + `","cast":["` + narrator.ID + `"]},"actorId":"` + narrator.ID + `","segments":[{"kind":"narration","text":"Rain."}]}`
	}

	if status, body := call("GET", base, ""); status != http.StatusOK || body != "[]" {
		t.Fatalf("empty scenes: %d %s", status, body)
	}
	if status, body := call("POST", move, cut(barrel.ID)); status != http.StatusUnprocessableEntity {
		t.Fatalf("cut to barrel: %d %s", status, body)
	}
	if status, body := call("POST", move, cut("nope")); status != http.StatusNotFound {
		t.Fatalf("cut to unknown: %d %s", status, body)
	}
	if status, body := call("POST", move, `{"direction":{"type":"cut"},"actorId":"`+narrator.ID+`","segments":[]}`); status != http.StatusBadRequest {
		t.Fatalf("cut without place: %d %s", status, body)
	}
	status, body := call("POST", move, cut(tavern.ID))
	if status != http.StatusCreated || !strings.Contains(body, `"placeId":"`+tavern.ID+`"`) || !strings.Contains(body, `"type":"cut"`) || !strings.Contains(body, `"threadId":"`+th.ID+`"`) {
		t.Fatalf("cut: %d %s", status, body)
	}
	var made struct {
		Scene scene.Scene `json:"scene"`
	}
	if err := json.Unmarshal([]byte(body), &made); err != nil {
		t.Fatal(err)
	}
	sc := made.Scene
	if status, body := call("GET", base, ""); status != http.StatusOK || !strings.Contains(body, `"cast":["`+narrator.ID+`"]`) {
		t.Fatalf("scenes: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/"+sc.ID+"/entries/"+sc.Opening, ""); status != http.StatusUnprocessableEntity {
		t.Fatalf("delete opening entry: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/nope", `{"title":"x"}`); status != http.StatusNotFound {
		t.Fatalf("retitle unknown scene: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/"+sc.ID, `{"title":" The reckoning "}`); status != http.StatusNoContent {
		t.Fatalf("retitle: %d %s", status, body)
	}
	if status, body := call("GET", base, ""); status != http.StatusOK || !strings.Contains(body, `"title":"The reckoning"`) {
		t.Fatalf("scenes after retitle: %d %s", status, body)
	}
	if status, body := call("POST", base+"/"+sc.ID+"/presence", `{"type":"enter","actors":["`+barrel.ID+`"]}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("barrel enters: %d %s", status, body)
	}
	if status, body := call("POST", base+"/"+sc.ID+"/presence", `{"type":"exit","actors":[]}`); status != http.StatusBadRequest {
		t.Fatalf("nobody exits: %d %s", status, body)
	}
	if status, body := call("POST", base+"/"+sc.ID+"/presence", `{"type":"exit","actors":["`+narrator.ID+`"]}`); status != http.StatusCreated || !strings.Contains(body, `"type":"exit"`) {
		t.Fatalf("narrator exits: %d %s", status, body)
	}
	if status, body := call("GET", base, ""); status != http.StatusOK || !strings.Contains(body, `"cast":[]`) {
		t.Fatalf("scenes after exit: %d %s", status, body)
	}
	if status, body := call("POST", base+"/"+sc.ID+"/entries", `{"actorId":"`+narrator.ID+`","segments":[{"kind":"speech","text":"x"}]}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("absent narrator speaks: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/nope", ""); status != http.StatusNotFound {
		t.Fatalf("delete unknown scene: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/"+sc.ID, ""); status != http.StatusNoContent {
		t.Fatalf("delete scene: %d %s", status, body)
	}
	if status, body := call("GET", base, ""); status != http.StatusOK || body != "[]" {
		t.Fatalf("scenes after delete: %d %s", status, body)
	}
}

func TestEntriesFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()
	call := caller(t, srv)
	create := func(path, body string, out any) {
		t.Helper()
		status, res := call("POST", path, body)
		if status != http.StatusCreated {
			t.Fatalf("POST %s: %d %s", path, status, res)
		}
		if err := json.Unmarshal([]byte(res), out); err != nil {
			t.Fatal(err)
		}
	}

	var g game.Game
	create("/api/games", `{"name":"Harbour"}`, &g)
	var th thread.Thread
	create("/api/games/"+g.ID+"/threads", `{"name":"Docks"}`, &th)
	var tavern, innkeeper subject.Subject
	create("/api/games/"+g.ID+"/subjects", `{"name":"Tavern","description":"","actor":false,"scene":true}`, &tavern)
	create("/api/games/"+g.ID+"/subjects", `{"name":"Innkeeper","description":"","actor":true,"scene":false}`, &innkeeper)
	var made struct {
		Scene scene.Scene `json:"scene"`
	}
	create("/api/games/"+g.ID+"/threads/"+th.ID+"/transitions", `{"direction":{"type":"cut","placeId":"`+tavern.ID+`","cast":["`+innkeeper.ID+`"]},"actorId":"`+innkeeper.ID+`","segments":[{"kind":"narration","text":"Rain."}]}`, &made)
	sc := made.Scene
	base := "/api/games/" + g.ID + "/threads/" + th.ID + "/scenes/" + sc.ID + "/entries"

	if status, body := call("GET", base, ""); status != http.StatusOK || !strings.Contains(body, `"text":"Rain."`) {
		t.Fatalf("opening entry: %d %s", status, body)
	}
	if status, body := call("POST", base, `{"actorId":"`+tavern.ID+`","segments":[{"kind":"speech","text":"hi"}]}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("tavern speaks: %d %s", status, body)
	}
	if status, body := call("POST", base, `{"actorId":"`+innkeeper.ID+`","segments":[{"kind":"song","text":"la"}]}`); status != http.StatusBadRequest {
		t.Fatalf("bad kind: %d %s", status, body)
	}
	var e entry.Entry
	create(base, `{"actorId":"`+innkeeper.ID+`","segments":[{"kind":"action","text":"sets a cup down"},{"kind":"speech","text":"You said three days."}]}`, &e)
	if e.Seq != 2 || len(e.Segments) != 2 {
		t.Fatalf("entry = %+v", e)
	}
	var between entry.Entry
	create(base, `{"actorId":"`+innkeeper.ID+`","afterId":"`+sc.Opening+`","segments":[{"kind":"action","text":"leans in"}]}`, &between)
	if between.Seq != 2 {
		t.Fatalf("inserted entry = %+v", between)
	}
	if status, body := call("POST", base, `{"actorId":"`+innkeeper.ID+`","afterId":"nope","segments":[{"kind":"action","text":"x"}]}`); status != http.StatusNotFound {
		t.Fatalf("insert after unknown: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/"+between.ID, ""); status != http.StatusNoContent {
		t.Fatalf("delete inserted: %d %s", status, body)
	}
	if status, body := call("GET", base, ""); status != http.StatusOK || !strings.Contains(body, `"kind":"action"`) {
		t.Fatalf("entries: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/nope", `{"actorId":"`+innkeeper.ID+`","segments":[{"kind":"speech","text":"x"}]}`); status != http.StatusNotFound {
		t.Fatalf("update unknown: %d %s", status, body)
	}
	if status, body := call("PUT", base+"/"+e.ID, `{"actorId":"`+innkeeper.ID+`","segments":[{"kind":"thought","text":"Nine days."}]}`); status != http.StatusNoContent {
		t.Fatalf("update: %d %s", status, body)
	}
	if status, body := call("GET", base, ""); status != http.StatusOK || !strings.Contains(body, `"kind":"thought"`) || strings.Contains(body, `"kind":"action"`) {
		t.Fatalf("entries after update: %d %s", status, body)
	}
	subjects := "/api/games/" + g.ID + "/subjects/"
	if status, body := call("DELETE", subjects+tavern.ID, ""); status != http.StatusConflict || !strings.Contains(body, "1 scene") {
		t.Fatalf("delete place in use: %d %s", status, body)
	}
	if status, body := call("DELETE", subjects+innkeeper.ID, ""); status != http.StatusConflict || !strings.Contains(body, "2 entries") {
		t.Fatalf("delete actor in use: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/nope", ""); status != http.StatusNotFound {
		t.Fatalf("delete unknown: %d %s", status, body)
	}
	if status, body := call("DELETE", base+"/"+e.ID, ""); status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", status, body)
	}
	if status, body := call("GET", base, ""); status != http.StatusOK || strings.Contains(body, `"kind":"thought"`) {
		t.Fatalf("entries after delete: %d %s", status, body)
	}
	if status, body := call("DELETE", subjects+innkeeper.ID, ""); status != http.StatusConflict {
		t.Fatalf("delete actor still in cast: %d %s", status, body)
	}
}

func TestModelsFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()
	call := caller(t, srv)

	if status, body := call("POST", "/api/models", `{"name":"x","engine":"ollama","endpoint":"http://h:1","model":"m"}`); status != http.StatusBadRequest {
		t.Fatalf("bad engine: %d %s", status, body)
	}
	status, body := call("POST", "/api/models", `{"name":"Gemma","engine":"llamacpp","endpoint":"http://h:11434","model":"gemma"}`)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	var m model.Model
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	if status, body := call("PUT", "/api/models/nope", `{"name":"Gemma","engine":"llamacpp","endpoint":"http://h:11434","model":"gemma"}`); status != http.StatusNotFound {
		t.Fatalf("update unknown: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/models/"+m.ID, `{"name":"Gemma","engine":"llamacpp","endpoint":"http://h:11434","model":"gemma-27b"}`); status != http.StatusNoContent {
		t.Fatalf("update: %d %s", status, body)
	}
	if status, body := call("GET", "/api/models", ""); status != http.StatusOK || !strings.Contains(body, `"model":"gemma-27b"`) {
		t.Fatalf("list: %d %s", status, body)
	}
	if status, body := call("POST", "/api/models", `{"name":"GLM","engine":"openrouter","model":"z-ai/glm"}`); status != http.StatusBadRequest {
		t.Fatalf("hosted without key: %d %s", status, body)
	}
	status, body = call("POST", "/api/models", `{"name":"GLM","engine":"openrouter","model":"z-ai/glm","key":"sk-or-1"}`)
	if status != http.StatusCreated || strings.Contains(body, "sk-or-1") || !strings.Contains(body, `"keySet":true`) || !strings.Contains(body, `"endpoint":"https://openrouter.ai/api/v1"`) {
		t.Fatalf("hosted: %d %s", status, body)
	}
	if status, body := call("GET", "/api/models", ""); status != http.StatusOK || strings.Contains(body, "sk-or-1") {
		t.Fatalf("list leaks key: %d %s", status, body)
	}
	if status, body := call("POST", "/api/models/nope/copy", ""); status != http.StatusNotFound {
		t.Fatalf("copy unknown model: %d %s", status, body)
	}
	if status, body := call("POST", "/api/models/"+m.ID+"/copy", ""); status != http.StatusCreated || !strings.Contains(body, `"name":"Gemma copy"`) {
		t.Fatalf("copy model: %d %s", status, body)
	}
}

func TestRollFlow(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := newServer(t, db)
	defer srv.Close()
	call := caller(t, srv)
	create := func(path, body string, out any) {
		t.Helper()
		status, res := call("POST", path, body)
		if status != http.StatusCreated {
			t.Fatalf("POST %s: %d %s", path, status, res)
		}
		if err := json.Unmarshal([]byte(res), out); err != nil {
			t.Fatal(err)
		}
	}
	var g game.Game
	create("/api/games", `{"name":"Harbour"}`, &g)
	var th thread.Thread
	create("/api/games/"+g.ID+"/threads", `{"name":"Docks"}`, &th)
	var tavern, mara subject.Subject
	create("/api/games/"+g.ID+"/subjects", `{"name":"Tavern","description":"","actor":false,"scene":true}`, &tavern)
	create("/api/games/"+g.ID+"/subjects", `{"name":"Mara","description":"","actor":true,"scene":false}`, &mara)
	var made struct {
		Scene scene.Scene `json:"scene"`
	}
	create("/api/games/"+g.ID+"/threads/"+th.ID+"/transitions", `{"direction":{"type":"cut","placeId":"`+tavern.ID+`","cast":["`+mara.ID+`"]},"actorId":"`+mara.ID+`","segments":[{"kind":"narration","text":"Rain."}]}`, &made)
	sc := made.Scene
	roll := "/api/games/" + g.ID + "/threads/" + th.ID + "/scenes/" + sc.ID + "/roll"

	if status, body := call("GET", "/api/models/selection", ""); status != http.StatusOK || body != `{"modelId":null}` {
		t.Fatalf("no selection: %d %s", status, body)
	}
	if status, body := call("POST", roll, `{"actorId":"`+mara.ID+`"}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("roll without model: %d %s", status, body)
	}
	var m model.Model
	create("/api/models", `{"name":"m","engine":"llamacpp","endpoint":"http://h:1","model":"x"}`, &m)
	if status, body := call("PUT", "/api/models/selection", `{"modelId":"nope"}`); status != http.StatusNotFound {
		t.Fatalf("select unknown model: %d %s", status, body)
	}
	if status, body := call("PUT", "/api/models/selection", `{"modelId":"`+m.ID+`"}`); status != http.StatusNoContent {
		t.Fatalf("select model: %d %s", status, body)
	}
	var e entry.Entry
	create(roll, `{"actorId":"`+mara.ID+`"}`, &e)
	if e.ActorID != mara.ID || len(e.Segments) != 1 || e.Segments[0].Text != "Rolled." {
		t.Fatalf("rolled entry = %+v", e)
	}
	if status, body := call("GET", strings.Replace(roll, "/roll", "/prompt?actor="+mara.ID, 1), ""); status != http.StatusOK || !strings.Contains(body, `"prompt":"PLACES`) {
		t.Fatalf("prompt preview: %d %s", status, body)
	}
	threadBase := "/api/games/" + g.ID + "/threads/" + th.ID
	if status, body := call("GET", threadBase+"/turn", ""); status != http.StatusOK || body != `{"turn":null}` {
		t.Fatalf("turn: %d %s", status, body)
	}
	if status, body := call("POST", threadBase+"/advance", ""); status != http.StatusOK || !strings.Contains(body, `"phase":"entry"`) || !strings.Contains(body, `"outcome":{"directed":{`) || !strings.Contains(body, `"type":"direction"`) {
		t.Fatalf("advance with one model actor: %d %s", status, body)
	}
	if status, body := call("DELETE", threadBase+"/turn", ""); status != http.StatusUnprocessableEntity {
		t.Fatalf("pass with no turn: %d %s", status, body)
	}
}
