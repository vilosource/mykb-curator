package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newFakeAnthropic(handler func(req map[string]any) (status int, body string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		status, resp := handler(body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
}

func TestAnthropicClient_HappyPath(t *testing.T) {
	srv := newFakeAnthropic(func(req map[string]any) (int, string) {
		// Verify our adapter sends the expected request shape.
		if req["model"] != "claude-opus-4-7" {
			t.Errorf("model = %v, want claude-opus-4-7", req["model"])
		}
		if req["max_tokens"] == nil {
			t.Errorf("max_tokens missing in request")
		}
		// Anthropic messages response shape.
		return 200, `{
		  "id": "msg_x",
		  "type": "message",
		  "role": "assistant",
		  "content": [{"type":"text","text":"hello from claude"}],
		  "model": "claude-opus-4-7",
		  "stop_reason": "end_turn",
		  "usage": {"input_tokens": 12, "output_tokens": 3}
		}`
	})
	defer srv.Close()

	c := NewAnthropicClient(AnthropicConfig{
		Endpoint: srv.URL,
		APIKey:   "fake-key",
	})
	resp, err := c.Complete(context.Background(), Request{
		Model:     "claude-opus-4-7",
		Prompt:    "say hello",
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello from claude" {
		t.Errorf("Text = %q, want %q", resp.Text, "hello from claude")
	}
	if resp.TokensIn != 12 || resp.TokensOut != 3 {
		t.Errorf("tokens = (%d,%d), want (12,3)", resp.TokensIn, resp.TokensOut)
	}
}

func TestAnthropicClient_APIError_Surfaces(t *testing.T) {
	srv := newFakeAnthropic(func(_ map[string]any) (int, string) {
		return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`
	})
	defer srv.Close()

	c := NewAnthropicClient(AnthropicConfig{Endpoint: srv.URL, APIKey: "k"})
	_, err := c.Complete(context.Background(), Request{Model: "nope", Prompt: "x"})
	if err == nil {
		t.Errorf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Errorf("err = %v, want to include API error message", err)
	}
}

func TestAnthropicClient_SendsSystemPromptWhenSet(t *testing.T) {
	var got map[string]any
	srv := newFakeAnthropic(func(req map[string]any) (int, string) {
		got = req
		return 200, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`
	})
	defer srv.Close()

	c := NewAnthropicClient(AnthropicConfig{Endpoint: srv.URL, APIKey: "k"})
	_, _ = c.Complete(context.Background(), Request{
		Model:  "m",
		Prompt: "p",
		System: "you are a wiki curator",
	})
	if got["system"] != "you are a wiki curator" {
		t.Errorf("system = %v, want %q", got["system"], "you are a wiki curator")
	}
}
