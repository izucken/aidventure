package httpserver

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"aidventure/internal/entry"
	"aidventure/internal/game"
	"aidventure/internal/play"
	"aidventure/internal/scene"
	"aidventure/internal/subject"
	"aidventure/internal/thread"
)

// transitionsAPI takes the play out of a thread's current scene by hand: a
// cut, a split or a converge, with the entry that arrives.
type transitionsAPI struct {
	db  *sql.DB
	log *slog.Logger
}

func (a transitionsAPI) create(w http.ResponseWriter, r *http.Request) {
	var in play.Transition
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	res, err := play.DoTransition(r.Context(), a.db, r.PathValue("gameId"), r.PathValue("threadId"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (a transitionsAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, entry.ErrInvalid), errors.Is(err, play.ErrBadTransition):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, game.ErrNotFound), errors.Is(err, thread.ErrNotFound), errors.Is(err, subject.ErrNotFound), errors.Is(err, scene.ErrNoPlace):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, scene.ErrNotScene), errors.Is(err, entry.ErrNotActor), errors.Is(err, entry.ErrNotPresent),
		errors.Is(err, thread.ErrEnded), errors.Is(err, play.ErrNoScene), errors.Is(err, play.ErrNoCast):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
