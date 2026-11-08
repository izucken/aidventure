package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"aidventure/internal/attribute"
	"aidventure/internal/game"
)

type attributesAPI struct {
	attributes *attribute.Store
	log        *slog.Logger
}

func (a attributesAPI) list(w http.ResponseWriter, r *http.Request) {
	attrs, err := a.attributes.List(r.Context(), r.PathValue("gameId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, attrs)
}

func (a attributesAPI) create(w http.ResponseWriter, r *http.Request) {
	var in attribute.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	made, err := a.attributes.Create(r.Context(), r.PathValue("gameId"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, made)
}

func (a attributesAPI) update(w http.ResponseWriter, r *http.Request) {
	var in attribute.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.attributes.Update(r.Context(), r.PathValue("gameId"), r.PathValue("attributeId"), in); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a attributesAPI) remove(w http.ResponseWriter, r *http.Request) {
	if err := a.attributes.Delete(r.Context(), r.PathValue("gameId"), r.PathValue("attributeId")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a attributesAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attribute.ErrBad):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, attribute.ErrInUse):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, game.ErrNotFound), errors.Is(err, attribute.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
