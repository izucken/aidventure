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
	// System is standing instruction; User is the material; Prefill is text
	// the answer must begin with. Prefill is ignored when the model thinks,
	// since a forced opening would close the thought block before it starts.
	System, User, Prefill string
	// Grammar constrains the answer, in the engine's grammar language, and must
	// define root. Empty is free text. With a thinking model it applies after the thought.
	Grammar string
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
// template, and the grammar as sent, which may wrap the request's grammar.
type Rendered struct {
	Prompt  string `json:"prompt"`
	Grammar string `json:"grammar"`
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
