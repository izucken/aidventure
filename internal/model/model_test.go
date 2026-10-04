package model_test

import (
	"errors"
	"path/filepath"
	"testing"

	"aidventure/internal/model"
	"aidventure/internal/store"
)

func TestStore(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	models := model.NewStore(db)
	good := model.Input{Name: "Gemma", Engine: model.LlamaCpp, Endpoint: "http://host:11434", Model: "gemma-4-12b", Think: true, ThinkBudget: 800}

	for name, bad := range map[string]model.Input{
		"blank name":   {Engine: model.LlamaCpp, Endpoint: good.Endpoint, Model: good.Model},
		"bad engine":   {Name: "x", Engine: "ollama", Endpoint: good.Endpoint, Model: good.Model},
		"bad endpoint": {Name: "x", Engine: model.LlamaCpp, Endpoint: "host:11434", Model: good.Model},
		"blank model":  {Name: "x", Engine: model.LlamaCpp, Endpoint: good.Endpoint},
		"keyless":      {Name: "x", Engine: model.OpenRouter, Model: "z-ai/glm"},
	} {
		if _, err := models.Create(ctx, bad); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("%s: err = %v; want ErrInvalid", name, err)
		}
	}
	if err := models.Update(ctx, "nope", good); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("update unknown: err = %v; want ErrNotFound", err)
	}

	m, err := models.Create(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	good.Model = "gemma-4-27b"
	if err := models.Update(ctx, m.ID, good); err != nil {
		t.Fatal(err)
	}
	list, err := models.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != m.ID || list[0].Model != "gemma-4-27b" || !list[0].Think || list[0].ThinkBudget != 800 {
		t.Fatalf("list = %+v", list)
	}

	// OpenRouter has a known address and needs a key, which an update without one keeps.
	hosted, err := models.Create(ctx, model.Input{Name: "GLM", Engine: model.OpenRouter, Model: "z-ai/glm", Key: " sk-or-1 "})
	if err != nil {
		t.Fatal(err)
	}
	if hosted.Endpoint != "https://openrouter.ai/api/v1" || hosted.Key != "sk-or-1" {
		t.Fatalf("hosted = %+v", hosted)
	}
	if err := models.Update(ctx, hosted.ID, model.Input{Name: "GLM", Engine: model.OpenRouter, Model: "z-ai/glm-5"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := models.Get(ctx, hosted.ID); got.Key != "sk-or-1" || got.Model != "z-ai/glm-5" {
		t.Fatalf("after keyless update = %+v", got)
	}
}
