package llm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// counterClient counts Complete calls + returns canned responses.
// Used to verify that the cache decorator prevents re-invocation
// of the wrapped client on hits.
type counterClient struct {
	calls atomic.Int64
	resp  Response
	err   error
}

func (c *counterClient) Complete(_ context.Context, _ Request) (Response, error) {
	c.calls.Add(1)
	if c.err != nil {
		return Response{}, c.err
	}
	return c.resp, nil
}

func TestCacheDecorator_MissPopulatesAndReturns(t *testing.T) {
	dir := t.TempDir()
	inner := &counterClient{resp: Response{Text: "cached body", TokensIn: 5, TokensOut: 2}}
	c := NewCacheDecorator(inner, dir)

	req := Request{Model: "m", Prompt: "hello", MaxTokens: 100}
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "cached body" {
		t.Errorf("Text = %q, want %q", resp.Text, "cached body")
	}
	// Fixture file should exist now.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("cache dir has %d files, want 1", len(entries))
	}
}

func TestCacheDecorator_HitReplaysWithoutCallingInner(t *testing.T) {
	dir := t.TempDir()
	inner := &counterClient{resp: Response{Text: "from-inner"}}
	c := NewCacheDecorator(inner, dir)
	req := Request{Model: "m", Prompt: "hi"}

	_, _ = c.Complete(context.Background(), req) // populate
	if inner.calls.Load() != 1 {
		t.Fatalf("first call: inner calls = %d, want 1", inner.calls.Load())
	}
	resp, _ := c.Complete(context.Background(), req) // hit
	if inner.calls.Load() != 1 {
		t.Errorf("after cache hit: inner calls = %d, want still 1", inner.calls.Load())
	}
	if !resp.CacheHit {
		t.Errorf("CacheHit = false, want true on hit")
	}
	if resp.Text != "from-inner" {
		t.Errorf("Text = %q, want %q (cache replays the original response)", resp.Text, "from-inner")
	}
}

func TestCacheDecorator_InnerErrorIsNotCached(t *testing.T) {
	dir := t.TempDir()
	wantErr := errors.New("simulated transient failure")
	inner := &counterClient{err: wantErr}
	c := NewCacheDecorator(inner, dir)

	req := Request{Model: "m", Prompt: "hi"}
	_, err := c.Complete(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error from inner, got nil")
	}
	// Should NOT have written a cache entry.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("cache should be empty after error, got %d files", len(entries))
	}
	// Second call should hit inner again — error not memoised.
	_, _ = c.Complete(context.Background(), req)
	if inner.calls.Load() != 2 {
		t.Errorf("inner calls = %d, want 2 (errors don't poison cache)", inner.calls.Load())
	}
}

func TestCacheDecorator_DifferentRequests_GetDifferentEntries(t *testing.T) {
	dir := t.TempDir()
	inner := &counterClient{resp: Response{Text: "x"}}
	c := NewCacheDecorator(inner, dir)

	_, _ = c.Complete(context.Background(), Request{Prompt: "one"})
	_, _ = c.Complete(context.Background(), Request{Prompt: "two"})

	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("cache has %d files, want 2 (distinct requests)", len(entries))
	}
}

func TestCacheDecorator_PersistsAcrossNewDecorator(t *testing.T) {
	// A second decorator pointing at the same dir should hit the
	// cache written by the first. This is what makes the cache useful
	// across CLI invocations.
	dir := t.TempDir()
	inner1 := &counterClient{resp: Response{Text: "persisted"}}
	_, _ = NewCacheDecorator(inner1, dir).Complete(context.Background(), Request{Prompt: "p"})

	inner2 := &counterClient{resp: Response{Text: "NOT seen — should never be called"}}
	resp, _ := NewCacheDecorator(inner2, dir).Complete(context.Background(), Request{Prompt: "p"})
	if inner2.calls.Load() != 0 {
		t.Errorf("new decorator hit inner; want cache hit instead")
	}
	if resp.Text != "persisted" {
		t.Errorf("Text = %q, want %q", resp.Text, "persisted")
	}
}

func TestCacheDecorator_CacheDirCreatedIfMissing(t *testing.T) {
	// Pass a non-existent sub-directory — decorator should create it
	// on first write so the user doesn't have to mkdir.
	base := t.TempDir()
	dir := filepath.Join(base, "deep", "nested", "cache")
	inner := &counterClient{resp: Response{Text: "x"}}
	_, err := NewCacheDecorator(inner, dir).Complete(context.Background(), Request{Prompt: "p"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("cache dir not created: %v", err)
	}
}

func TestCacheDecorator_KeyIncludesModelAndTokens(t *testing.T) {
	// Same prompt with different model OR different max_tokens must
	// produce different cache keys — same prompt to claude-3 vs
	// claude-4 should not collide.
	dir := t.TempDir()
	inner := &counterClient{resp: Response{Text: "x"}}
	c := NewCacheDecorator(inner, dir)
	_, _ = c.Complete(context.Background(), Request{Model: "a", Prompt: "p", MaxTokens: 100})
	_, _ = c.Complete(context.Background(), Request{Model: "b", Prompt: "p", MaxTokens: 100})
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("different models produced %d cache files, want 2", len(entries))
	}
}

// Sanity: ensure file names are sensible (hex.json) for diff-friendliness.
func TestCacheDecorator_FilenamesAreHexJSON(t *testing.T) {
	dir := t.TempDir()
	inner := &counterClient{resp: Response{Text: "x"}}
	_, _ = NewCacheDecorator(inner, dir).Complete(context.Background(), Request{Prompt: "p"})
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasSuffix(name, ".json") {
		t.Errorf("filename %q should end in .json", name)
	}
}
