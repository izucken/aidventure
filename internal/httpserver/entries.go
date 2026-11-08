package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

type entriesAPI struct {
	entries *entry.Store
	log     *slog.Logger
}

func (a entriesAPI) list(w http.ResponseWriter, r *http.Request) {
	entries, err := a.entries.List(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

type entryInput struct {
	ActorID  string          `json:"actorId"`
	Segments []entry.Segment `json:"segments"`
	// AfterID places the entry right after that one; empty appends.
	AfterID string `json:"afterId,omitempty"`
}

func (a entriesAPI) listThread(w http.ResponseWriter, r *http.Request) {
	entries, err := a.entries.ListThread(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (a entriesAPI) append(w http.ResponseWriter, r *http.Request) {
	var in entryInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var e entry.Entry
	var err error
	if in.AfterID != "" {
		e, err = a.entries.AppendAfter(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), in.AfterID, in.ActorID, "", in.Segments)
	} else {
		e, err = a.entries.Append(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), in.ActorID, "", in.Segments)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (a entriesAPI) update(w http.ResponseWriter, r *http.Request) {
	var in entryInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	err := a.entries.Update(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), r.PathValue("entryId"), in.ActorID, in.Segments)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a entriesAPI) remove(w http.ResponseWriter, r *http.Request) {
	err := a.entries.Delete(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), r.PathValue("entryId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a entriesAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, entry.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, game.ErrNotFound), errors.Is(err, thread.ErrNotFound),
		errors.Is(err, scene.ErrNotFound), errors.Is(err, subject.ErrNotFound), errors.Is(err, entry.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, entry.ErrNotActor), errors.Is(err, entry.ErrOpening), errors.Is(err, entry.ErrEnding), errors.Is(err, entry.ErrNotPresent), errors.Is(err, entry.ErrInUse):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
