package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer imitates llama-server with a Gemma-like template: thinking off
// closes an empty thought block, thinking on leaves the turn open. A
// completion with a stop string ends the content there and says so; one
// under a predict limit shorter than the content is cut and says so.
func fakeServer(t *testing.T, content string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var calls []map[string]any
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
				b.WriteString("<|turn>" + msg["role"].(string) + "\n" + msg["content"].(string) + "<turn|>\n")
			}
			b.WriteString("<|turn>model\n")
			if !body["chat_template_kwargs"].(map[string]any)["enable_thinking"].(bool) {
				b.WriteString("<|channel>thought\n<channel|>")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"prompt": b.String()})
		case "/completion":
			calls = append(calls, body)
			out := map[string]any{
				"content": content, "stopped_eos": true, "tokens_evaluated": 40, "tokens_cached": 30, "tokens_predicted": 5,
				"timings": map[string]float64{"prompt_ms": 12.5, "predicted_ms": 100},
			}
			if stops, ok := body["stop"].([]any); ok {
				if before, _, found := strings.Cut(content, stops[0].(string)); found {
					out["content"], out["stopped_eos"], out["stopped_word"] = before, false, true
				}
				if limit, _ := body["n_predict"].(float64); limit > 0 && int(limit) < len(out["content"].(string)) {
					out["content"], out["stopped_word"], out["stopped_limit"] = out["content"].(string)[:int(limit)], false, true
				}
			} else if p, _ := body["prompt"].(string); strings.Contains(p, "<channel|>") && !strings.HasSuffix(p, "<|channel>thought\n<channel|>") {
				// The answer call after a thought: what follows the close marker.
				_, after, _ := strings.Cut(content, "<channel|>")
				out["content"] = after
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	return srv, &calls
}

var shape = map[string]any{"type": "string"}

func TestCompleteWithoutThinking(t *testing.T) {
	srv, calls := fakeServer(t, "\"Hello.\"\n")
	defer srv.Close()
	res, err := NewLlamaCpp(srv.URL+"/", false, 0).Complete(t.Context(), Request{System: "sys", User: "script", Schema: shape})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\"Hello.\"\n" || res.Think != "" || res.PromptTokens != 40 || res.CachedTokens != 30 || res.Tokens != 5 {
		t.Fatalf("res = %+v", res)
	}
	got := (*calls)[0]
	prompt, _ := got["prompt"].(string)
	if !strings.Contains(prompt, "<|turn>system\nsys<turn|>\n<|turn>user\nscript<turn|>\n") || !strings.HasSuffix(prompt, "<|turn>model\n<|channel>thought\n<channel|>") {
		t.Fatalf("prompt = %q", prompt)
	}
	if schema, _ := got["json_schema"].(map[string]any); schema["type"] != "string" || got["cache_prompt"] != true || got["stop"] != nil || len(*calls) != 1 {
		t.Fatalf("body = %+v", got)
	}
}

// A thinking model runs two completions: the thought to its close, then the
// answer under the schema from the thought on, on the cached prefix.
func TestCompleteWithThinking(t *testing.T) {
	srv, calls := fakeServer(t, "<|channel>thought\nShe is stalling. Answer short.\n<channel|>\"Hello.\"\n")
	defer srv.Close()
	res, err := NewLlamaCpp(srv.URL, true, 0).Complete(t.Context(), Request{System: "sys", User: "script", Schema: shape})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\"Hello.\"\n" || res.Think != "She is stalling. Answer short." || res.Tokens != 10 {
		t.Fatalf("res = %+v", res)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d", len(*calls))
	}
	first, second := (*calls)[0], (*calls)[1]
	if stop, _ := first["stop"].([]any); len(stop) != 1 || stop[0] != "<channel|>" || first["json_schema"] != nil || first["n_predict"] != float64(-1) {
		t.Fatalf("thought call = %+v", first)
	}
	if p, _ := second["prompt"].(string); !strings.HasSuffix(p, "<|turn>model\n<|channel>thought\nShe is stalling. Answer short.\n<channel|>") || second["json_schema"] == nil || second["stop"] != nil {
		t.Fatalf("answer call = %+v", second)
	}
}

// A budget cuts a long thought; the wrap-up closes it before the answer.
func TestCompleteWithBudget(t *testing.T) {
	srv, calls := fakeServer(t, "<|channel>thought\nShe is stalling. She will not say.\n<channel|>\"Hello.\"\n")
	defer srv.Close()
	res, err := NewLlamaCpp(srv.URL, true, 34).Complete(t.Context(), Request{User: "u", Schema: shape})
	if err != nil {
		t.Fatal(err)
	}
	if res.Think != "She is stalling."+strings.TrimRight(wrapUp, "\n") || res.Text != "\"Hello.\"\n" {
		t.Fatalf("res = %+v", res)
	}
	if (*calls)[0]["n_predict"] != float64(34) {
		t.Fatalf("thought call = %+v", (*calls)[0])
	}
	if p, _ := (*calls)[1]["prompt"].(string); !strings.HasSuffix(p, "<|channel>thought\nShe is stalling."+wrapUp+"<channel|>") {
		t.Fatalf("answer prompt = %q", p)
	}
}

func TestRender(t *testing.T) {
	srv, calls := fakeServer(t, "")
	defer srv.Close()
	r, err := NewLlamaCpp(srv.URL, true, 0).Render(t.Context(), Request{System: "sys", User: "u", Schema: shape})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.Prompt, "<|turn>model\n") || !strings.Contains(r.Schema, `"type": "string"`) {
		t.Fatalf("rendered = %+v", r)
	}
	if len(*calls) != 0 {
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

// Streamed, the thought arrives as thought and the answer as text, each from its own call.
func TestStreamSplitsThought(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			chunks := []map[string]any{{"content": "\"Hel"}, {"content": "lo.\"\n", "stop": true, "stopped_eos": true, "tokens_predicted": 3}}
			if _, thinking := body["stop"]; thinking {
				// The opener is split across chunks on purpose.
				chunks = []map[string]any{{"content": "<|chan"}, {"content": "nel>thought\nShe is "}, {"content": "stalling.", "stop": true, "stopped_word": true, "tokens_predicted": 4, "tokens_evaluated": 40}}
			}
			for _, chunk := range chunks {
				b, _ := json.Marshal(chunk)
				_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			}
		}
	}))
	defer srv.Close()

	var parts []string
	res, err := NewLlamaCpp(srv.URL, true, 0).Complete(t.Context(), Request{User: "u", Schema: shape, Stream: func(part, delta string) {
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
