package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterComplete(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, chunk := range []string{
				`{"choices":[{"delta":{"reasoning":"She is "}}]}`,
				`{"choices":[{"delta":{"reasoning":"stalling."}}]}`,
				`{"choices":[{"delta":{"content":"MARA:\n"}}]}`,
				`{"choices":[{"delta":{"content":"\"Hello.\"\n"}}],"usage":{"prompt_tokens":40,"completion_tokens":9}}`,
				`[DONE]`,
			} {
				_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "```\n\"Hello.\"\n```", "reasoning": "She is stalling.\n"}}},
			"usage":   map[string]any{"prompt_tokens": 40, "completion_tokens": 7, "prompt_tokens_details": map[string]any{"cached_tokens": 30}},
		})
	}))
	defer srv.Close()
	client := NewOpenRouter(srv.URL+"/", "sk-or-1", "z-ai/glm", true, 500)

	res, err := client.Complete(t.Context(), Request{System: "sys", User: "script", Prefill: "MARA:\n", Grammar: "root ::= \"x\""})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "\"Hello.\"\n" || res.Think != "She is stalling." || res.PromptTokens != 40 || res.CachedTokens != 30 || res.Tokens != 7 {
		t.Fatalf("res = %+v", res)
	}
	if auth != "Bearer sk-or-1" || got["model"] != "z-ai/glm" || got["grammar"] != nil {
		t.Fatalf("request = %+v auth %q", got, auth)
	}
	reasoning, _ := got["reasoning"].(map[string]any)
	if reasoning["enabled"] != true || reasoning["max_tokens"] != float64(500) {
		t.Fatalf("reasoning = %+v", reasoning)
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 2 || msgs[1].(map[string]any)["content"] != "script" {
		t.Fatalf("messages = %+v", msgs)
	}

	var parts []string
	res, err = client.Complete(t.Context(), Request{User: "u", Stream: func(part, delta string) { parts = append(parts, part+":"+delta) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Think != "She is stalling." || res.Text != "MARA:\n\"Hello.\"\n" || res.Tokens != 9 || res.PromptTokens != 40 {
		t.Fatalf("streamed res = %+v", res)
	}
	if strings.Join(parts, "|") != "think:She is |think:stalling.|text:MARA:\n|text:\"Hello.\"\n" {
		t.Fatalf("parts = %q", parts)
	}
	if r, _ := client.Render(t.Context(), Request{System: "s", User: "u"}); !strings.HasPrefix(r.Prompt, "SYSTEM\ns\n\nUSER\nu") || r.Grammar != "" {
		t.Fatalf("render = %+v", r)
	}
	if _, err := NewOpenRouter(srv.URL, "", "z-ai/glm", false, 0).Complete(t.Context(), Request{}); err != nil {
		t.Fatal(err)
	}
	if got["reasoning"] != nil {
		t.Fatalf("thinking off still asks for reasoning: %+v", got)
	}
}

func TestOpenRouterErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "gone" {
			http.Error(w, `{"error":{"message":"no such model"}}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "rate limited", "code": 429}})
	}))
	defer srv.Close()
	if _, err := NewOpenRouter(srv.URL, "k", "gone", false, 0).Complete(t.Context(), Request{}); err == nil || !strings.Contains(err.Error(), "no such model") {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewOpenRouter(srv.URL, "k", "busy", false, 0).Complete(t.Context(), Request{}); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v", err)
	}
}
