// Package llm is the boundary to language model engines. The rest of the
// server speaks Request and Response; nothing above this package knows which
// engine or model family is behind them.
package llm

import (
	"context"
	"fmt"
	"time"

	"aidventure/internal/model"
)

type Request struct {
	// System is standing instruction; User is the material.
	System, User string
	// Schema is the JSON schema the answer must conform to, as a plain object
	// tree; nil is free text. Each engine binds it its own way, and with a
	// thinking model it applies after the thought.
	Schema map[string]any
	// Stream, when set, receives the answer as it is produced: part is "think"
	// or "text". The Response at the end is complete regardless.
	Stream func(part, delta string) `json:"-"`
}

type Response struct {
	// Think is the model's reasoning, when the model thinks; Text is the answer.
	Think, Text string
	// Prompt and cached token counts show how well the prefix cache worked.
	PromptTokens, CachedTokens, Tokens int
	PromptTime, GenerateTime           time.Duration
}

// Rendered is what the model literally receives: the prompt after the chat
// template, where there is one, and the schema as sent.
type Rendered struct {
	Prompt string `json:"prompt"`
	Schema string `json:"schema"`
}

type Engine interface {
	Complete(ctx context.Context, req Request) (Response, error)
	// Render shows what Complete would send, without sending it.
	Render(ctx context.Context, req Request) (Rendered, error)
}

// New returns the engine client for a model entry.
func New(m model.Model) (Engine, error) {
	switch m.Engine {
	case model.LlamaCpp:
		return NewLlamaCpp(m.Endpoint, m.Think, m.ThinkBudget), nil
	case model.OpenRouter:
		return NewOpenRouter(m.Endpoint, m.Key, m.Model, m.Think, m.ThinkBudget), nil
	}
	return nil, fmt.Errorf("unsupported engine %q", m.Engine)
}
