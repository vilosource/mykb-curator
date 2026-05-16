package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/llm"
)

func TestPiClient_Complete_RoundTrip(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/complete" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text": "pong", "tokens_in": 615, "tokens_out": 6,
		})
	}))
	defer srv.Close()

	c := llm.NewPiClient(srv.URL)
	resp, err := c.Complete(context.Background(), llm.Request{
		Model: "claude-opus-4-7", Prompt: "ping", System: "be terse", MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "pong" || resp.TokensIn != 615 || resp.TokensOut != 6 {
		t.Errorf("resp = %+v", resp)
	}
	if gotBody["prompt"] != "ping" || gotBody["model"] != "claude-opus-4-7" ||
		gotBody["system"] != "be terse" {
		t.Errorf("request body not mapped from llm.Request: %v", gotBody)
	}
}

func TestPiClient_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "pi batch-mode invocation failed", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := llm.NewPiClient(srv.URL).Complete(context.Background(), llm.Request{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("expected a 502-bearing error, got %v", err)
	}
}

func TestPiClient_EmptyTextIsError(t *testing.T) {
	// The wrapper guarantees non-empty or non-2xx; a 2xx with empty
	// text is a contract violation — fail loud, never silent "".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"text": ""})
	}))
	defer srv.Close()
	_, err := llm.NewPiClient(srv.URL).Complete(context.Background(), llm.Request{Prompt: "x"})
	if err == nil {
		t.Errorf("empty completion text must be an error")
	}
}

func TestPiClient_MalformedBodyIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	_, err := llm.NewPiClient(srv.URL).Complete(context.Background(), llm.Request{Prompt: "x"})
	if err == nil {
		t.Errorf("malformed response body must be an error")
	}
}

func TestPiClient_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "late"})
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := llm.NewPiClient(srv.URL).Complete(ctx, llm.Request{Prompt: "x"}); err == nil {
		t.Errorf("cancelled context must surface an error")
	}
}

// PiClient must satisfy llm.Client.
var _ llm.Client = (*llm.PiClient)(nil)
