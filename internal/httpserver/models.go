package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"aidventure/internal/model"
)

type modelsAPI struct {
	models *model.Store
	log    *slog.Logger
}

// modelView is a model as the client may see it: the key stays on the server.
type modelView struct {
	model.Model
	KeySet bool `json:"keySet"`
}

func view(m model.Model) modelView {
	v := modelView{Model: m, KeySet: m.Key != ""}
	v.Key = ""
	return v
}

func (a modelsAPI) list(w http.ResponseWriter, r *http.Request) {
	models, err := a.models.List(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	views := make([]modelView, 0, len(models))
	for _, m := range models {
		views = append(views, view(m))
	}
	writeJSON(w, http.StatusOK, views)
}

func (a modelsAPI) create(w http.ResponseWriter, r *http.Request) {
	var in model.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	m, err := a.models.Create(r.Context(), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view(m))
}

func (a modelsAPI) update(w http.ResponseWriter, r *http.Request) {
	var in model.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.models.Update(r.Context(), r.PathValue("modelId"), in); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// copy makes a new entry from an existing one.
func (a modelsAPI) copy(w http.ResponseWriter, r *http.Request) {
	m, err := a.models.Copy(r.Context(), r.PathValue("modelId"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view(m))
}

type modelSelection struct {
	ModelID *string `json:"modelId"`
}

func (a modelsAPI) selection(w http.ResponseWriter, r *http.Request) {
	id, err := a.models.Current(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	var sel modelSelection
	if id != "" {
		sel.ModelID = &id
	}
	writeJSON(w, http.StatusOK, sel)
}

func (a modelsAPI) setSelection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ModelID string `json:"modelId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.models.SetCurrent(r.Context(), in.ModelID); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a modelsAPI) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		a.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
