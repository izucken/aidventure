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
// goes through the native /completion route for grammars and cache control.
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
	body, _, err := c.prepare(ctx, req)
	if err != nil {
		return Rendered{}, err
	}
	grammar, _ := body["grammar"].(string)
	if c.budgeted() {
		grammar = req.Grammar // the answer binds plainly, in a call of its own after the thought
	}
	return Rendered{Prompt: body["prompt"].(string), Grammar: grammar}, nil
}

func (c *LlamaCpp) budgeted() bool { return c.think && c.budget > 0 }

func (c *LlamaCpp) Complete(ctx context.Context, req Request) (Response, error) {
	if c.budgeted() {
		return c.completeBudgeted(ctx, req)
	}
	body, marks, err := c.prepare(ctx, req)
	if err != nil {
		return Response{}, err
	}
	out, err := c.run(ctx, body, marks, c.think, req.Stream)
	if err != nil {
		return Response{}, err
	}
	res := out.response()
	if c.think {
		if think, text, ok := strings.Cut(out.Content, marks.close); ok {
			res.Think = marks.thought(think)
			res.Text = text
		}
	}
	return res, nil
}

// completeBudgeted runs the thought and the answer as two completions. The
// thought runs under the token budget and stops at the close marker; the
// answer runs from the thought and the marker, under the grammar, and rides
// the prefix cache. A thought the budget cut short gets the wrap-up line
// first, so the answer follows a finished sentence.
func (c *LlamaCpp) completeBudgeted(ctx context.Context, req Request) (Response, error) {
	body, marks, err := c.prepare(ctx, req)
	if err != nil {
		return Response{}, err
	}
	prompt := body["prompt"].(string)
	thought, err := c.run(ctx, map[string]any{"prompt": prompt, "n_predict": c.budget, "cache_prompt": true, "stop": []string{marks.close}}, marks, true, req.Stream)
	if err != nil {
		return Response{}, err
	}
	res := thought.response()
	if !thought.StoppedWord && !thought.StoppedLimit {
		// The model answered without a thought block; what it wrote is the answer.
		return res, nil
	}
	tail := thought.Content
	if thought.StoppedLimit {
		tail += wrapUp
		if req.Stream != nil {
			req.Stream("think", wrapUp)
		}
	}
	answer := map[string]any{"prompt": prompt + tail + marks.close, "n_predict": -1, "cache_prompt": true}
	if req.Grammar != "" {
		answer["grammar"] = req.Grammar
	}
	out, err := c.run(ctx, answer, marks, false, req.Stream)
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

// prepare builds the /completion body: the templated prompt, and the grammar,
// wrapped for lazy use when the model thinks.
func (c *LlamaCpp) prepare(ctx context.Context, req Request) (map[string]any, thinkMarkers, error) {
	msgs := []message{{"system", req.System}, {"user", req.User}}
	if !c.think && req.Prefill != "" {
		msgs = append(msgs, message{"assistant", req.Prefill})
	}
	prompt, err := c.applyTemplate(ctx, msgs, c.think)
	if err != nil {
		return nil, thinkMarkers{}, err
	}
	body := map[string]any{
		"prompt":       prompt,
		"n_predict":    -1,
		"cache_prompt": true,
	}
	var marks thinkMarkers
	if c.think {
		if marks, err = c.thinkMarkers(ctx); err != nil {
			return nil, thinkMarkers{}, err
		}
	}
	if req.Grammar != "" {
		body["grammar"] = req.Grammar
		if c.think {
			// Grammar waits until the thought block closes, then binds the answer.
			// The trigger text is fed to the grammar, so the root must consume it.
			body["grammar"] = "root ::= \"" + marks.close + "\" answer\n" + strings.Replace(req.Grammar, "root ::=", "answer ::=", 1)
			body["grammar_lazy"] = true
			body["grammar_triggers"] = []map[string]any{{"type": 1, "value": marks.close}}
			body["preserved_tokens"] = []string{marks.close}
		}
	}
	return body, marks, nil
}

// completion is llama-server's /completion answer, final chunk included when streaming.
type completion struct {
	Content string `json:"content"`
	Stop    bool   `json:"stop"`
	// Why it stopped: a stop string, the predict limit, or the model's own end.
	StoppedWord     bool `json:"stopped_word"`
	StoppedLimit    bool `json:"stopped_limit"`
	TokensEvaluated int  `json:"tokens_evaluated"`
	TokensCached    int  `json:"tokens_cached"`
	TokensPredicted int  `json:"tokens_predicted"`
	Timings         struct {
		PromptMS    float64 `json:"prompt_ms"`
		PredictedMS float64 `json:"predicted_ms"`
	} `json:"timings"`
}

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

// splitter routes streamed deltas to thought or text. Before the close marker
// it holds back a marker's worth of tail so a marker split across chunks is
// still caught; the thought's opening marker and preamble are dropped.
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
