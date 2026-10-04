package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenRouter talks to OpenRouter's chat completions, an OpenAI-shaped API in
// front of hosted models. No grammar reaches the model: the briefs carry the
// form and the answer is read as written, less any code fence around it.
// Reasoning comes back as its own field, and the thinking budget maps onto
// the provider's reasoning budget.
type OpenRouter struct {
	endpoint, key, model string
	think                bool
	budget               int
	http                 *http.Client
}

func NewOpenRouter(endpoint, key, model string, think bool, budget int) *OpenRouter {
	return &OpenRouter{endpoint: strings.TrimRight(endpoint, "/"), key: key, model: model, think: think, budget: budget, http: &http.Client{Timeout: 10 * time.Minute}}
}

func (c *OpenRouter) body(req Request, stream bool) map[string]any {
	body := map[string]any{
		"model":    c.model,
		"messages": []message{{"system", req.System}, {"user", req.User}},
		"stream":   stream,
		"usage":    map[string]any{"include": true},
	}
	if c.think {
		reasoning := map[string]any{"enabled": true}
		if c.budget > 0 {
			reasoning["max_tokens"] = c.budget
		}
		body["reasoning"] = reasoning
	}
	return body
}

// Render shows the messages as sent. There is no grammar to show: the form
// lives in the brief.
func (c *OpenRouter) Render(_ context.Context, req Request) (Rendered, error) {
	return Rendered{Prompt: "SYSTEM\n" + req.System + "\n\nUSER\n" + req.User + "\n"}, nil
}

func (c *OpenRouter) Complete(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	out, err := c.ask(ctx, req)
	if err != nil {
		return Response{}, err
	}
	res := Response{
		Think:        strings.TrimSpace(out.reasoning),
		Text:         unfence(out.content),
		PromptTokens: out.usage.PromptTokens,
		CachedTokens: out.usage.PromptTokensDetails.CachedTokens,
		Tokens:       out.usage.CompletionTokens,
		GenerateTime: time.Since(start),
	}
	return res, nil
}

// chat is what one completion yields, streamed or not.
type chat struct {
	content, reasoning string
	usage              usage
}

type usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// envelope is the response shape, whole or as one streamed chunk; a failure
// may arrive in it with a 200 as well as with an error status.
type envelope struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"`
		} `json:"message"`
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func (c *OpenRouter) ask(ctx context.Context, req Request) (chat, error) {
	payload, err := json.Marshal(c.body(req, req.Stream != nil))
	if err != nil {
		return chat{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return chat{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+c.key)
	r.Header.Set("X-Title", "aidventure")
	res, err := c.http.Do(r)
	if err != nil {
		return chat{}, fmt.Errorf("openrouter: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return chat{}, fmt.Errorf("openrouter %s: %s: %s", c.model, res.Status, strings.TrimSpace(string(msg)))
	}
	if req.Stream == nil {
		var env envelope
		if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
			return chat{}, fmt.Errorf("openrouter: decode: %w", err)
		}
		return c.whole(env)
	}
	return c.stream(res.Body, req.Stream)
}

func (c *OpenRouter) whole(env envelope) (chat, error) {
	if env.Error != nil {
		return chat{}, fmt.Errorf("openrouter %s: %s", c.model, env.Error.Message)
	}
	if len(env.Choices) == 0 {
		return chat{}, fmt.Errorf("openrouter %s: answer has no choices", c.model)
	}
	out := chat{content: env.Choices[0].Message.Content, reasoning: env.Choices[0].Message.Reasoning}
	if env.Usage != nil {
		out.usage = *env.Usage
	}
	return out, nil
}

// stream reads the event stream, hands each delta to sink as thought or
// text, and returns the whole answer as if it had not streamed.
func (c *OpenRouter) stream(body io.Reader, sink func(part, delta string)) (chat, error) {
	var out chat
	var content, reasoning strings.Builder
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			break
		}
		var env envelope
		if err := json.Unmarshal([]byte(data), &env); err != nil {
			return chat{}, fmt.Errorf("openrouter: stream: %w", err)
		}
		if env.Error != nil {
			return chat{}, fmt.Errorf("openrouter %s: %s", c.model, env.Error.Message)
		}
		if env.Usage != nil {
			out.usage = *env.Usage
		}
		for _, ch := range env.Choices {
			if ch.Delta.Reasoning != "" {
				reasoning.WriteString(ch.Delta.Reasoning)
				sink("think", ch.Delta.Reasoning)
			}
			if ch.Delta.Content != "" {
				content.WriteString(ch.Delta.Content)
				sink("text", ch.Delta.Content)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return chat{}, fmt.Errorf("openrouter: stream: %w", err)
	}
	out.content, out.reasoning = content.String(), reasoning.String()
	return out, nil
}

// unfence takes a code fence off an answer that came wrapped in one, which a
// chat model does with anything that looks like a format.
func unfence(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "```") {
		return text
	}
	if _, rest, ok := strings.Cut(t, "\n"); ok {
		t = rest
	} else {
		return text
	}
	t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	return strings.TrimSpace(t) + "\n"
}
