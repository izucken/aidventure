package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"aidventure/internal/game"
)

type gamesAPI struct {
	games *game.Store
	log   *slog.Logger
}

type selection struct {
	GameID *string `json:"gameId"`
}

func (a gamesAPI) list(w http.ResponseWriter, r *http.Request) {
	games, err := a.games.List(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, games)
}

func (a gamesAPI) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "game name is required")
		return
	}
	g, err := a.games.Create(r.Context(), name)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

// update renames the game.
func (a gamesAPI) update(w http.ResponseWriter, r *http.Request) {
	name, ok := readName(w, r, "game")
	if !ok {
		return
	}
	if err := a.games.Rename(r.Context(), r.PathValue("gameId"), name); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// settings replaces how the game is told.
func (a gamesAPI) settings(w http.ResponseWriter, r *http.Request) {
	var in game.Settings
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := a.games.SetSettings(r.Context(), r.PathValue("gameId"), in); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readName reads a body of the form {"name": ...}; what is the thing named, for the error.
func readName(w http.ResponseWriter, r *http.Request, what string) (string, bool) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return "", false
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, what+" name is required")
		return "", false
	}
	return name, true
}

func (a gamesAPI) selection(w http.ResponseWriter, r *http.Request) {
	id, err := a.games.Current(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	var sel selection
	if id != "" {
		sel.GameID = &id
	}
	writeJSON(w, http.StatusOK, sel)
}

func (a gamesAPI) setSelection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		GameID string `json:"gameId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	err := a.games.SetCurrent(r.Context(), in.GameID)
	if errors.Is(err, game.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a gamesAPI) fail(w http.ResponseWriter, err error) {
	a.log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, err.Error())
}
