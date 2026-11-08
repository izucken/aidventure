package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

type scenesAPI struct {
	scenes  *scene.Store
	entries *entry.Store
	log     *slog.Logger
}

func (a scenesAPI) list(w http.ResponseWriter, r *http.Request) {
	scenes, err := a.scenes.List(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scenes)
}

// presence records who entered or exited the scene, at its end or after a given entry.
func (a scenesAPI) presence(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type   entry.Type `json:"type"`
		Actors []string   `json:"actors"`
		// AfterID places the line right after that entry; empty puts it at the end.
		AfterID string `json:"afterId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	e, err := a.entries.Presence(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), in.Type, in.Actors, in.AfterID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// update sets the scene's title; an empty title takes it away.
func (a scenesAPI) update(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.scenes.Retitle(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), strings.TrimSpace(in.Title)); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a scenesAPI) remove(w http.ResponseWriter, r *http.Request) {
	if err := a.scenes.Delete(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a scenesAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, entry.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, game.ErrNotFound), errors.Is(err, thread.ErrNotFound), errors.Is(err, scene.ErrNotFound), errors.Is(err, subject.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, entry.ErrNotActor), errors.Is(err, entry.ErrNotFound):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
