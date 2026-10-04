package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"aidventure/internal/game"
	"aidventure/internal/thread"
)

type threadsAPI struct {
	threads *thread.Store
	log     *slog.Logger
}

type threadSelection struct {
	ThreadID *string `json:"threadId"`
}

func (a threadsAPI) list(w http.ResponseWriter, r *http.Request) {
	threads, err := a.threads.List(r.Context(), r.PathValue("gameId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, threads)
}

func (a threadsAPI) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "thread name is required")
		return
	}
	t, err := a.threads.Create(r.Context(), r.PathValue("gameId"), name)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// update renames the thread.
func (a threadsAPI) update(w http.ResponseWriter, r *http.Request) {
	name, ok := readName(w, r, "thread")
	if !ok {
		return
	}
	if err := a.threads.Rename(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), name); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a threadsAPI) remove(w http.ResponseWriter, r *http.Request) {
	if err := a.threads.Delete(r.Context(), r.PathValue("gameId"), r.PathValue("threadId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a threadsAPI) selection(w http.ResponseWriter, r *http.Request) {
	id, err := a.threads.Current(r.Context(), r.PathValue("gameId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	var sel threadSelection
	if id != "" {
		sel.ThreadID = &id
	}
	writeJSON(w, http.StatusOK, sel)
}

func (a threadsAPI) setSelection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThreadID string `json:"threadId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.threads.SetCurrent(r.Context(), r.PathValue("gameId"), in.ThreadID); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a threadsAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, game.ErrNotFound), errors.Is(err, thread.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
