package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestRecordingClient_AlwaysCallsInner(t *testing.T) {
	// Recording is a write-through wrapper — even if a fixture already
	// exists, the next Complete must call the inner (which is the
	// whole point: capturing fresh responses).
	dir := t.TempDir()
	inner := &counterClient{resp: Response{Text: "fresh"}}
	r := NewRecordingClient(inner, dir)

	req := Request{Model: "m", Prompt: "p"}
	_, _ = r.Complete(context.Background(), req)
	_, _ = r.Complete(context.Background(), req)
	if inner.calls.Load() != 2 {
		t.Errorf("inner calls = %d, want 2 (no cache, always passes through)", inner.calls.Load())
	}
}

func TestRecordingClient_PersistsResponseOnSuccess(t *testing.T) {
	dir := t.TempDir()
	want := Response{Text: "recorded text", TokensIn: 7, TokensOut: 3}
	inner := &counterClient{resp: want}
	r := NewRecordingClient(inner, dir)

	req := Request{Model: "m", Prompt: "p"}
	got, err := r.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Text != want.Text {
		t.Errorf("Text = %q, want %q", got.Text, want.Text)
	}

	// File written with the same key scheme as ReplayClient.
	path := filepath.Join(dir, requestHash(req)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded fixture: %v", err)
	}
	var onDisk Response
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if onDisk.Text != want.Text {
		t.Errorf("fixture Text = %q, want %q", onDisk.Text, want.Text)
	}
}

func TestRecordingClient_OnInnerError_NoFixtureWritten(t *testing.T) {
	dir := t.TempDir()
	wantErr := errors.New("simulated")
	inner := &counterClient{err: wantErr}
	r := NewRecordingClient(inner, dir)

	_, err := r.Complete(context.Background(), Request{Prompt: "p"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("error path wrote %d fixtures, want 0", len(entries))
	}
}

func TestRecordingClient_OverwritesExistingFixture(t *testing.T) {
	// Same request, different response over time → fixture should
	// reflect the latest run (call Recording explicitly to
	// regenerate). Distinguishes Recording (overwrite) from Cache
	// (preserve hit).
	dir := t.TempDir()
	req := Request{Model: "m", Prompt: "p"}

	// First recording.
	inner1 := &counterClient{resp: Response{Text: "v1"}}
	_, _ = NewRecordingClient(inner1, dir).Complete(context.Background(), req)

	// Second recording with a different inner response.
	inner2 := &counterClient{resp: Response{Text: "v2"}}
	_, _ = NewRecordingClient(inner2, dir).Complete(context.Background(), req)

	path := filepath.Join(dir, requestHash(req)+".json")
	data, _ := os.ReadFile(path)
	var onDisk Response
	_ = json.Unmarshal(data, &onDisk)
	if onDisk.Text != "v2" {
		t.Errorf("fixture not overwritten: got %q, want %q", onDisk.Text, "v2")
	}
}

func TestRecordingClient_FixturesDirAutoCreated(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "deep", "nested")
	inner := &counterClient{resp: Response{Text: "x"}}
	_, err := NewRecordingClient(inner, dir).Complete(context.Background(), Request{Prompt: "p"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("fixtures dir not created: %v", err)
	}
}

// Compile-time check that *RecordingClient satisfies the Client interface.
var _ Client = (*RecordingClient)(nil)

// counterClient is already defined in cache_test.go in the same
// package. The atomic.Int64 import keeps go-vet happy when this
// file is processed in isolation.
var _ = atomic.Int64{}
