package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/model"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

type rollAPI struct {
	roller *play.Roller
	log    *slog.Logger
}

func (a rollAPI) roll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ActorID string `json:"actorId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	e, err := a.roller.Roll(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), in.ActorID, nil)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// advance streams the turn as newline-delimited JSON: events as the model
// produces them, then one line with the outcome or an error.
func (a rollAPI) advance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	emit := func(v any) {
		_ = enc.Encode(v)
		if flusher != nil {
			flusher.Flush()
		}
	}
	out, err := a.roller.Advance(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), func(ev play.Event) { emit(ev) })
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.log.Error("advance failed", "err", err)
			emit(map[string]string{"error": err.Error()})
		}
		return
	}
	emit(map[string]any{"outcome": out})
}

func (a rollAPI) turn(w http.ResponseWriter, r *http.Request) {
	cur, err := a.roller.Turn(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"turn": cur})
}

func (a rollAPI) pass(w http.ResponseWriter, r *http.Request) {
	if err := a.roller.Pass(r.Context(), r.PathValue("gameId"), r.PathValue("threadId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a rollAPI) preview(w http.ResponseWriter, r *http.Request) {
	p, err := a.roller.Preview(r.Context(), r.PathValue("gameId"), r.PathValue("threadId"), r.PathValue("sceneId"), r.URL.Query().Get("actor"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a rollAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, game.ErrNotFound), errors.Is(err, thread.ErrNotFound),
		errors.Is(err, scene.ErrNotFound), errors.Is(err, subject.ErrNotFound), errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, play.ErrBadTransition):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, entry.ErrNotActor), errors.Is(err, entry.ErrNotPresent), errors.Is(err, play.ErrNoModel),
		errors.Is(err, play.ErrNoScene), errors.Is(err, play.ErrNoCast), errors.Is(err, play.ErrNoTurn), errors.Is(err, thread.ErrEnded):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, play.ErrEmpty), errors.Is(err, play.ErrTooLong):
		writeError(w, http.StatusBadGateway, err.Error())
	case errors.Is(err, context.Canceled):
		// The client let go; nobody is listening for the answer.
		a.log.Info("roll cancelled")
	default:
		a.log.Error("roll failed", "err", err)
		writeError(w, http.StatusBadGateway, "model request failed: "+err.Error())
	}
}
