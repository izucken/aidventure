package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"aidventure/internal/attribute"
	"aidventure/internal/game"
	"aidventure/internal/subject"
)

type linksAPI struct {
	subjects *subject.Store
	log      *slog.Logger
}

func (a linksAPI) list(w http.ResponseWriter, r *http.Request) {
	links, err := a.subjects.Links(r.Context(), r.PathValue("gameId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, links)
}

func (a linksAPI) create(w http.ResponseWriter, r *http.Request) {
	var in subject.LinkInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	l, err := a.subjects.AddLink(r.Context(), r.PathValue("gameId"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (a linksAPI) update(w http.ResponseWriter, r *http.Request) {
	var in subject.LinkInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.subjects.UpdateLink(r.Context(), r.PathValue("gameId"), r.PathValue("linkId"), in); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a linksAPI) remove(w http.ResponseWriter, r *http.Request) {
	if err := a.subjects.DeleteLink(r.Context(), r.PathValue("gameId"), r.PathValue("linkId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a linksAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, subject.ErrBadLink), errors.Is(err, attribute.ErrBad):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, game.ErrNotFound), errors.Is(err, subject.ErrNotFound), errors.Is(err, subject.ErrNoLink), errors.Is(err, attribute.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
