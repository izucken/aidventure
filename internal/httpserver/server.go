// Package httpserver wires the HTTP routes: the JSON API under /api and the
// browser UI everywhere else.
package httpserver

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/model"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

// Deps is everything the API reads, writes and drives.
type Deps struct {
	Games    *game.Store
	Threads  *thread.Store
	Subjects *subject.Store
	Scenes   *scene.Store
	Entries  *entry.Store
	Models   *model.Store
	Roller   *play.Roller
	// DB is for transitions, which span stores in one transaction.
	DB *sql.DB
}

func New(log *slog.Logger, frontend http.Handler, st Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", handleHealthz)

	m := modelsAPI{models: st.Models, log: log}
	mux.HandleFunc("GET /api/models", m.list)
	mux.HandleFunc("POST /api/models", m.create)
	mux.HandleFunc("PUT /api/models/{modelId}", m.update)
	mux.HandleFunc("GET /api/models/selection", m.selection)
	mux.HandleFunc("PUT /api/models/selection", m.setSelection)

	g := gamesAPI{games: st.Games, log: log}
	mux.HandleFunc("GET /api/games", g.list)
	mux.HandleFunc("POST /api/games", g.create)
	mux.HandleFunc("PUT /api/games/{gameId}", g.update)
	mux.HandleFunc("PUT /api/games/{gameId}/settings", g.settings)
	mux.HandleFunc("GET /api/selection", g.selection)
	mux.HandleFunc("PUT /api/selection", g.setSelection)

	t := threadsAPI{threads: st.Threads, log: log}
	mux.HandleFunc("GET /api/games/{gameId}/threads", t.list)
	mux.HandleFunc("POST /api/games/{gameId}/threads", t.create)
	mux.HandleFunc("PUT /api/games/{gameId}/threads/{threadId}", t.update)
	mux.HandleFunc("DELETE /api/games/{gameId}/threads/{threadId}", t.remove)
	mux.HandleFunc("GET /api/games/{gameId}/selection", t.selection)
	mux.HandleFunc("PUT /api/games/{gameId}/selection", t.setSelection)

	s := subjectsAPI{subjects: st.Subjects, log: log}
	mux.HandleFunc("GET /api/games/{gameId}/subjects", s.list)
	mux.HandleFunc("POST /api/games/{gameId}/subjects", s.create)
	mux.HandleFunc("PUT /api/games/{gameId}/subjects/{subjectId}", s.update)
	mux.HandleFunc("DELETE /api/games/{gameId}/subjects/{subjectId}", s.remove)

	l := linksAPI{subjects: st.Subjects, log: log}
	mux.HandleFunc("GET /api/games/{gameId}/links", l.list)
	mux.HandleFunc("POST /api/games/{gameId}/links", l.create)
	mux.HandleFunc("PUT /api/games/{gameId}/links/{linkId}", l.update)
	mux.HandleFunc("DELETE /api/games/{gameId}/links/{linkId}", l.remove)

	sc := scenesAPI{scenes: st.Scenes, entries: st.Entries, log: log}
	mux.HandleFunc("GET /api/games/{gameId}/threads/{threadId}/scenes", sc.list)
	mux.HandleFunc("PUT /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}", sc.update)
	mux.HandleFunc("DELETE /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}", sc.remove)
	mux.HandleFunc("POST /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/presence", sc.presence)

	tr := transitionsAPI{db: st.DB, log: log}
	mux.HandleFunc("POST /api/games/{gameId}/threads/{threadId}/transitions", tr.create)

	e := entriesAPI{entries: st.Entries, log: log}
	mux.HandleFunc("GET /api/games/{gameId}/threads/{threadId}/entries", e.listThread)
	mux.HandleFunc("GET /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/entries", e.list)
	mux.HandleFunc("POST /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/entries", e.append)
	mux.HandleFunc("PUT /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/entries/{entryId}", e.update)
	mux.HandleFunc("DELETE /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/entries/{entryId}", e.remove)
	roll := rollAPI{roller: st.Roller, log: log}
	mux.HandleFunc("POST /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/roll", roll.roll)
	mux.HandleFunc("GET /api/games/{gameId}/threads/{threadId}/scenes/{sceneId}/prompt", roll.preview)
	mux.HandleFunc("POST /api/games/{gameId}/threads/{threadId}/advance", roll.advance)
	mux.HandleFunc("GET /api/games/{gameId}/threads/{threadId}/turn", roll.turn)
	mux.HandleFunc("DELETE /api/games/{gameId}/threads/{threadId}/turn", roll.pass)

	// Unknown API paths must not fall through to the SPA's index.html.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such API route: "+r.Method+" "+r.URL.Path)
	})
	mux.Handle("/", frontend)
	return logRequests(log, mux)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are gone; the body is the only place left to say something.
		slog.Error("encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads a request body into v, rejecting oversized bodies and unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the underlying writer, which the
// dev proxy needs to hijack the connection for Vite's HMR websocket.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
