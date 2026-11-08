package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"aidventure/internal/attribute"
	"aidventure/internal/game"
	"aidventure/internal/subject"
)

type subjectsAPI struct {
	subjects *subject.Store
	log      *slog.Logger
}

func (a subjectsAPI) list(w http.ResponseWriter, r *http.Request) {
	subjects, err := a.subjects.List(r.Context(), r.PathValue("gameId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subjects)
}

func (a subjectsAPI) create(w http.ResponseWriter, r *http.Request) {
	in, ok := readSubjectInput(w, r)
	if !ok {
		return
	}
	sub, err := a.subjects.Create(r.Context(), r.PathValue("gameId"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

func (a subjectsAPI) update(w http.ResponseWriter, r *http.Request) {
	in, ok := readSubjectInput(w, r)
	if !ok {
		return
	}
	err := a.subjects.Update(r.Context(), r.PathValue("gameId"), r.PathValue("subjectId"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readSubjectInput(w http.ResponseWriter, r *http.Request) (subject.Input, bool) {
	var in subject.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return in, false
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "subject name is required")
		return in, false
	}
	return in, true
}

func (a subjectsAPI) remove(w http.ResponseWriter, r *http.Request) {
	if err := a.subjects.Delete(r.Context(), r.PathValue("gameId"), r.PathValue("subjectId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a subjectsAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attribute.ErrBad):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, subject.ErrInUse):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, game.ErrNotFound), errors.Is(err, subject.ErrNotFound), errors.Is(err, attribute.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
