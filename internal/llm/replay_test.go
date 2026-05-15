package llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReplayClient_HitReturnsFixture(t *testing.T) {
	dir := t.TempDir()
	req := Request{Model: "test-model", Prompt: "hello", MaxTokens: 64}
	key := requestHash(req)

	want := Response{Text: "world", TokensIn: 1, TokensOut: 1}
	data, _ := json.Marshal(want)
	if err := os.WriteFile(filepath.Join(dir, key+".json"), data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	c := NewReplayClient(dir)
	got, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Text != want.Text {
		t.Errorf("Text = %q, want %q", got.Text, want.Text)
	}
	if !got.CacheHit {
		t.Errorf("CacheHit = false, want true")
	}
}

func TestReplayClient_MissIsError(t *testing.T) {
	dir := t.TempDir()
	c := NewReplayClient(dir)
	_, err := c.Complete(context.Background(), Request{Model: "x", Prompt: "y"})
	if err == nil {
		t.Errorf("expected error for missing fixture, got nil")
	}
}

func TestRequestHash_StableAndDifferentiating(t *testing.T) {
	a := Request{Model: "m", Prompt: "p", MaxTokens: 10}
	b := Request{Model: "m", Prompt: "p", MaxTokens: 10}
	c := Request{Model: "m", Prompt: "p", MaxTokens: 11}

	if requestHash(a) != requestHash(b) {
		t.Errorf("identical requests hashed differently")
	}
	if requestHash(a) == requestHash(c) {
		t.Errorf("different requests hashed the same")
	}
}
