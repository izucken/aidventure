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
	"sync"
	"time"
)

// LlamaCpp talks to llama-server. The prompt is rendered by the server from
// the model's own chat template, so no turn markup lives here; completion
// goes through the native /completion route for schemas and cache control.
// A thinking model answers in two completions: the thought, stopped at the
// close marker, then the answer from the thought on, under the schema.
type LlamaCpp struct {
	endpoint string
	think    bool
	// budget is how many tokens a thought may run; 0 lets it run until the model closes it.
	budget int
	http   *http.Client

	once    sync.Once
	markers thinkMarkers
	markErr error
}

func NewLlamaCpp(endpoint string, think bool, budget int) *LlamaCpp {
	return &LlamaCpp{endpoint: strings.TrimRight(endpoint, "/"), think: think, budget: budget, http: &http.Client{Timeout: 10 * time.Minute}}
}

// wrapUp closes a thought the budget cut short, so the answer follows a
// finished sentence rather than a broken one.
const wrapUp = "\n\nThat is enough thought; answering now.\n"

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *LlamaCpp) Render(ctx context.Context, req Request) (Rendered, error) {
	prompt, err := c.prompt(ctx, req)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{Prompt: prompt, Schema: schemaText(req.Schema)}, nil
}

func (c *LlamaCpp) Complete(ctx context.Context, req Request) (Response, error) {
	prompt, err := c.prompt(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if !c.think {
		out, err := c.run(ctx, c.answer(prompt, req.Schema), thinkMarkers{}, false, req.Stream)
		if err != nil {
			return Response{}, err
		}
		return out.response(), nil
	}
	marks, err := c.thinkMarkers(ctx)
	if err != nil {
		return Response{}, err
	}
	thinking := map[string]any{"prompt": prompt, "n_predict": -1, "cache_prompt": true, "stop": []string{marks.close}}
	if c.budget > 0 {
		thinking["n_predict"] = c.budget
	}
	thought, err := c.run(ctx, thinking, marks, true, req.Stream)
	if err != nil {
		return Response{}, err
	}
	res := thought.response()
	if !thought.atWord() && !thought.atLimit() {
		// The model answered without a thought block; what it wrote is the answer.
		return res, nil
	}
	tail := thought.Content
	if thought.atLimit() {
		tail += wrapUp
		if req.Stream != nil {
			req.Stream("think", wrapUp)
		}
	}
	out, err := c.run(ctx, c.answer(prompt+tail+marks.close, req.Schema), marks, false, req.Stream)
	if err != nil {
		return Response{}, err
	}
	res.Think = marks.thought(tail)
	res.Text = out.Content
	res.Tokens += out.TokensPredicted
	res.PromptTime += ms(out.Timings.PromptMS)
	res.GenerateTime += ms(out.Timings.PredictedMS)
	return res, nil
}

// prompt renders the request through the model's chat template.
func (c *LlamaCpp) prompt(ctx context.Context, req Request) (string, error) {
	return c.applyTemplate(ctx, []message{{"system", req.System}, {"user", req.User}}, c.think)
}

// answer is the body of the completion that writes the answer, under the schema when there is one.
func (c *LlamaCpp) answer(prompt string, schema map[string]any) map[string]any {
	body := map[string]any{"prompt": prompt, "n_predict": -1, "cache_prompt": true}
	if schema != nil {
		body["json_schema"] = schema
	}
	return body
}

// run is one /completion call, streamed to sink when there is one.
func (c *LlamaCpp) run(ctx context.Context, body map[string]any, marks thinkMarkers, think bool, sink func(part, delta string)) (completion, error) {
	if sink != nil {
		body["stream"] = true
		return c.stream(ctx, body, marks, think, sink)
	}
	var out completion
	err := c.post(ctx, "/completion", body, &out)
	return out, err
}

// completion is llama-server's /completion answer, final chunk included when streaming.
type completion struct {
	Content string `json:"content"`
	Stop    bool   `json:"stop"`
	// Why it stopped: a stop string, the predict limit, or the model's own end.
	// Current servers say so in stop_type; older ones in the booleans.
	StopType        string `json:"stop_type"`
	StoppedWord     bool   `json:"stopped_word"`
	StoppedLimit    bool   `json:"stopped_limit"`
	TokensEvaluated int    `json:"tokens_evaluated"`
	TokensCached    int    `json:"tokens_cached"`
	TokensPredicted int    `json:"tokens_predicted"`
	Timings         struct {
		PromptMS    float64 `json:"prompt_ms"`
		PredictedMS float64 `json:"predicted_ms"`
	} `json:"timings"`
}

// atWord and atLimit are why the completion stopped, whichever way the server said it.
func (out completion) atWord() bool  { return out.StopType == "word" || out.StoppedWord }
func (out completion) atLimit() bool { return out.StopType == "limit" || out.StoppedLimit }

// response is the completion as the rest of the server reads it, all of it as text.
func (out completion) response() Response {
	return Response{
		Text:         out.Content,
		PromptTokens: out.TokensEvaluated,
		CachedTokens: out.TokensCached,
		Tokens:       out.TokensPredicted,
		PromptTime:   ms(out.Timings.PromptMS),
		GenerateTime: ms(out.Timings.PredictedMS),
	}
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

// schemaText is the schema as it is sent, for the preview; empty when there is none.
func schemaText(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	b, _ := json.MarshalIndent(schema, "", "  ")
	return string(b)
}

// stream reads llama-server's event stream, hands each delta to sink as
// thought or text, and returns the whole completion as if it had not streamed.
func (c *LlamaCpp) stream(ctx context.Context, body map[string]any, marks thinkMarkers, think bool, sink func(part, delta string)) (completion, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return completion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/completion", bytes.NewReader(payload))
	if err != nil {
		return completion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return completion{}, fmt.Errorf("llama-server: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return completion{}, fmt.Errorf("llama-server /completion: %s: %s", res.Status, strings.TrimSpace(string(msg)))
	}

	var all strings.Builder
	var last completion
	split := splitter{marks: marks, think: think, sink: sink}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk completion
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			return completion{}, fmt.Errorf("llama-server: stream: %w", err)
		}
		all.WriteString(chunk.Content)
		split.feed(chunk.Content)
		if chunk.Stop {
			last = chunk
		}
	}
	if err := sc.Err(); err != nil {
		return completion{}, fmt.Errorf("llama-server: stream: %w", err)
	}
	split.flush()
	last.Content = all.String()
	return last, nil
}

// splitter routes streamed deltas to thought or text. A thought's opening
// marker and preamble are dropped; a close marker, should the server let one
// through, ends the thought.
type splitter struct {
	marks   thinkMarkers
	think   bool
	sink    func(part, delta string)
	pending string
	started bool
	closed  bool
}

func (s *splitter) feed(delta string) {
	if !s.think || s.closed {
		s.sink("text", delta)
		return
	}
	s.pending += delta
	if before, after, ok := strings.Cut(s.pending, s.marks.close); ok {
		s.emitThink(before)
		s.pending = ""
		s.closed = true
		if after != "" {
			s.sink("text", after)
		}
		return
	}
	if !s.started {
		// Hold the head until the opener and preamble can be recognised whole.
		if len(s.pending) < len(s.marks.open)+len(s.marks.preamble)+2 {
			return
		}
		s.pending = s.strip(s.pending)
		s.started = true
	}
	if keep := len(s.marks.close) - 1; len(s.pending) > keep {
		s.sink("think", s.pending[:len(s.pending)-keep])
		s.pending = s.pending[len(s.pending)-keep:]
	}
}

func (s *splitter) emitThink(text string) {
	if !s.started {
		text = s.strip(text)
		s.started = true
	}
	if text != "" {
		s.sink("think", text)
	}
}

func (s *splitter) strip(text string) string {
	text = strings.TrimPrefix(strings.TrimLeft(text, "\n"), s.marks.open)
	text = strings.TrimPrefix(strings.TrimLeft(text, "\n "), s.marks.preamble)
	return strings.TrimLeft(text, "\n")
}

func (s *splitter) flush() {
	if s.think && !s.closed && s.pending != "" {
		s.emitThink(s.pending)
		s.pending = ""
	}
}

func (c *LlamaCpp) applyTemplate(ctx context.Context, msgs []message, think bool) (string, error) {
	var out struct {
		Prompt string `json:"prompt"`
	}
	err := c.post(ctx, "/apply-template", map[string]any{
		"messages":             msgs,
		"chat_template_kwargs": map[string]any{"enable_thinking": think},
	}, &out)
	return out.Prompt, err
}

// thinkMarkers is how a model family delimits its thought block. Detected
// once from the template, never configured. The only model-family knowledge
// in the codebase; add a row when a family arrives whose template differs.
type thinkMarkers struct {
	open, close string // tokens around the thought block
	preamble    string // text the template puts inside an empty block, to strip from the thought
}

// thought is the text of a thought block without the opener and preamble around it.
func (m thinkMarkers) thought(text string) string {
	text = strings.TrimPrefix(strings.TrimSpace(text), m.open)
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), m.preamble))
}

var knownMarkers = []thinkMarkers{
	{open: "<|channel>", close: "<channel|>", preamble: "thought"}, // Gemma
	{open: "<think>", close: "</think>"},                           // Qwen, DeepSeek
}

// thinkMarkers finds the family by rendering a thinking-off prompt: the
// template closes an empty thought block at the end, so the closer is its suffix.
func (c *LlamaCpp) thinkMarkers(ctx context.Context) (thinkMarkers, error) {
	c.once.Do(func() {
		off, err := c.applyTemplate(ctx, []message{{"user", "x"}}, false)
		if err != nil {
			c.markErr = err
			return
		}
		off = strings.TrimSpace(off)
		for _, m := range knownMarkers {
			if strings.HasSuffix(off, m.close) {
				c.markers = m
				return
			}
		}
		c.markErr = fmt.Errorf("model template has no known thought block; thinking-off prompt ends with %q", tail(off, 24))
	})
	return c.markers, c.markErr
}

func (c *LlamaCpp) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("llama-server: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("llama-server %s: %s: %s", path, res.Status, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("llama-server %s: decode: %w", path, err)
	}
	return nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
