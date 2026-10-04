package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer imitates llama-server with a Gemma-like template: thinking off
// closes an empty thought block, thinking on leaves the turn open.
func fakeServer(t *testing.T, content string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var completion map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/apply-template":
			var b strings.Builder
			for _, m := range body["messages"].([]any) {
				msg := m.(map[string]any)
				b.WriteString("<|turn>" + msg["role"].(string) + "\n" + msg["content"].(string))
				if msg["role"] != "assistant" {
					b.WriteString("<turn|>\n")
				}
			}
			think := body["chat_template_kwargs"].(map[string]any)["enable_thinking"].(bool)
			if !strings.HasSuffix(b.String(), "<turn|>\n") {
				// assistant prefill: already open
			} else {
				b.WriteString("<|turn>model\n")
				if !think {
					b.WriteString("<|channel>thought\n<channel|>")
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"prompt": b.String()})
		case "/completion":
			completion = body
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": content, "tokens_evaluated": 40, "tokens_cached": 30, "tokens_predicted": 5,
				"timings": map[string]float64{"prompt_ms": 12.5, "predicted_ms": 100},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	return srv, &completion
}

func TestCompleteWithoutThinking(t *testing.T) {
	srv, got := fakeServer(t, "\"Hello.\"\n")
	defer srv.Close()
	res, err := NewLlamaCpp(srv.URL+"/", false, 0).Complete(t.Context(), Request{System: "sys", User: "script", Prefill: "MARA:\n", Grammar: "root ::= \"x\""})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\"Hello.\"\n" || res.Think != "" || res.PromptTokens != 40 || res.CachedTokens != 30 || res.Tokens != 5 {
		t.Fatalf("res = %+v", res)
	}
	prompt, _ := (*got)["prompt"].(string)
	if !strings.Contains(prompt, "<|turn>system\nsys<turn|>\n<|turn>user\nscript<turn|>\n") || !strings.HasSuffix(prompt, "<|turn>assistant\nMARA:\n") {
		t.Fatalf("prompt = %q", prompt)
	}
	if (*got)["grammar"] != "root ::= \"x\"" || (*got)["grammar_lazy"] != nil || (*got)["cache_prompt"] != true {
		t.Fatalf("body = %+v", *got)
	}
}

func TestCompleteWithThinking(t *testing.T) {
	srv, got := fakeServer(t, "<|channel>thought\nShe is stalling. Answer short.\n<channel|>\"Hello.\"\n")
	defer srv.Close()
	res, err := NewLlamaCpp(srv.URL, true, 0).Complete(t.Context(), Request{System: "sys", User: "script", Prefill: "MARA:\n", Grammar: "root ::= \"x\""})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\"Hello.\"\n" || res.Think != "She is stalling. Answer short." {
		t.Fatalf("res = %+v", res)
	}
	prompt, _ := (*got)["prompt"].(string)
	if strings.Contains(prompt, "MARA") || !strings.HasSuffix(prompt, "<|turn>model\n") {
		t.Fatalf("prompt = %q", prompt)
	}
	triggers, _ := (*got)["grammar_triggers"].([]any)
	if (*got)["grammar_lazy"] != true || len(triggers) != 1 || triggers[0].(map[string]any)["value"] != "<channel|>" {
		t.Fatalf("body = %+v", *got)
	}
	if (*got)["grammar"] != "root ::= \"<channel|>\" answer\nanswer ::= \"x\"" {
		t.Fatalf("grammar = %q", (*got)["grammar"])
	}
}

func TestRender(t *testing.T) {
	srv, got := fakeServer(t, "")
	defer srv.Close()
	r, err := NewLlamaCpp(srv.URL, true, 0).Render(t.Context(), Request{System: "sys", User: "u", Grammar: "root ::= \"x\""})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.Prompt, "<|turn>model\n") || !strings.HasPrefix(r.Grammar, "root ::= \"<channel|>\" answer") {
		t.Fatalf("rendered = %+v", r)
	}
	if *got != nil {
		t.Fatal("render must not call /completion")
	}
}

func TestCompleteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"slot busy"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := NewLlamaCpp(srv.URL, false, 0).Complete(t.Context(), Request{}); err == nil || !strings.Contains(err.Error(), "slot busy") {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamSplitsThought(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apply-template":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			think := body["chat_template_kwargs"].(map[string]any)["enable_thinking"].(bool)
			p := "<|turn>model\n"
			if !think {
				p += "<|channel>thought\n<channel|>"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"prompt": p})
		case "/completion":
			w.Header().Set("Content-Type", "text/event-stream")
			// The close marker is split across chunks on purpose.
			for i, c := range []string{"<|channel>thought\nShe is ", "stalling.\n<chan", "nel|>\"Hel", "lo.\"\n"} {
				chunk := map[string]any{"content": c, "stop": i == 3}
				if i == 3 {
					chunk["tokens_predicted"] = 7
					chunk["tokens_evaluated"] = 40
				}
				b, _ := json.Marshal(chunk)
				_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			}
		}
	}))
	defer srv.Close()

	var parts []string
	res, err := NewLlamaCpp(srv.URL, true, 0).Complete(t.Context(), Request{User: "u", Grammar: "root ::= \"x\"", Stream: func(part, delta string) {
		parts = append(parts, part+":"+delta)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Think != "She is stalling." || res.Text != "\"Hello.\"\n" || res.Tokens != 7 || res.PromptTokens != 40 {
		t.Fatalf("res = %+v", res)
	}
	var think, text strings.Builder
	for _, p := range parts {
		kind, delta, _ := strings.Cut(p, ":")
		if kind == "think" {
			think.WriteString(delta)
		} else {
			text.WriteString(delta)
		}
	}
	if strings.TrimSpace(think.String()) != "She is stalling." || text.String() != "\"Hello.\"\n" {
		t.Fatalf("streamed think=%q text=%q parts=%q", think.String(), text.String(), parts)
	}
}

// TestCompleteWithBudget runs a budgeted thought: the thought in its own call
// under the budget, stopped at the close marker, then the answer from the
// thought on, under the grammar. A thought cut by the budget is wrapped up.
func TestCompleteWithBudget(t *testing.T) {
	cut := true
	var calls []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/apply-template":
			p := "<|turn>model\n"
			if !body["chat_template_kwargs"].(map[string]any)["enable_thinking"].(bool) {
				p += "<|channel>thought\n<channel|>"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"prompt": p})
		case "/completion":
			calls = append(calls, body)
			if _, thinking := body["stop"]; thinking {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"content": "<|channel>thought\nShe is stalling. She will", "stopped_limit": cut, "stopped_word": !cut,
					"tokens_evaluated": 40, "tokens_cached": 30, "tokens_predicted": 8, "timings": map[string]float64{"prompt_ms": 10, "predicted_ms": 80},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": "\"Hello.\"\n", "stopped_eos": true, "tokens_evaluated": 48, "tokens_cached": 48, "tokens_predicted": 5,
				"timings": map[string]float64{"prompt_ms": 1, "predicted_ms": 50},
			})
		}
	}))
	defer srv.Close()
	client := NewLlamaCpp(srv.URL, true, 800)

	res, err := client.Complete(t.Context(), Request{User: "u", Grammar: "root ::= \"x\""})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\"Hello.\"\n" || res.Think != "She is stalling. She will"+strings.TrimRight(wrapUp, "\n") || res.Tokens != 13 || res.PromptTokens != 40 {
		t.Fatalf("res = %+v", res)
	}
	if len(calls) != 2 || calls[0]["n_predict"] != float64(800) || calls[0]["grammar"] != nil || calls[1]["grammar"] != "root ::= \"x\"" || calls[1]["n_predict"] != float64(-1) {
		t.Fatalf("calls = %+v", calls)
	}
	if stop, _ := calls[0]["stop"].([]any); len(stop) != 1 || stop[0] != "<channel|>" {
		t.Fatalf("stop = %v", calls[0]["stop"])
	}
	if p, _ := calls[1]["prompt"].(string); !strings.HasSuffix(p, "<|turn>model\n<|channel>thought\nShe is stalling. She will"+wrapUp+"<channel|>") {
		t.Fatalf("answer prompt = %q", p)
	}

	// A thought that closes within the budget needs no wrap-up.
	cut, calls = false, nil
	res, err = client.Complete(t.Context(), Request{User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Think != "She is stalling. She will" || res.Text != "\"Hello.\"\n" || calls[1]["grammar"] != nil {
		t.Fatalf("res = %+v calls = %+v", res, calls)
	}
	if p, _ := calls[1]["prompt"].(string); !strings.HasSuffix(p, "She will<channel|>") {
		t.Fatalf("answer prompt = %q", p)
	}
	if r, err := client.Render(t.Context(), Request{User: "u", Grammar: "root ::= \"x\""}); err != nil || r.Grammar != "root ::= \"x\"" {
		t.Fatalf("render = %+v, %v", r, err)
	}
}
