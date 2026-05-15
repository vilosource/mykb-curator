package runstate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpen_CreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(filepath.Join(dir, "rs.bolt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close() }()

	if c == nil {
		t.Errorf("Open returned nil cache")
	}
}

func TestGet_MissingSpec_ReturnsZeroAndOK(t *testing.T) {
	c := openTmp(t)
	defer func() { _ = c.Close() }()

	got, ok, err := c.Get("does-not-exist.spec.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Errorf("ok = true, want false for missing key")
	}
	if got.LastBotRevID != "" {
		t.Errorf("LastBotRevID = %q, want empty", got.LastBotRevID)
	}
}

func TestSetGet_RoundTrip(t *testing.T) {
	c := openTmp(t)
	defer func() { _ = c.Close() }()

	want := SpecState{
		LastBotRevID:   "rev-49231",
		LastKBCommit:   "kb-commit-abc",
		LastRenderHash: "render-hash-def",
		LastRunID:      "run-xyz",
		LastRunAt:      time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC),
	}
	if err := c.Set("area-vault.spec.md", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get("area-vault.spec.md")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("ok = false after Set")
	}
	if got.LastBotRevID != want.LastBotRevID {
		t.Errorf("LastBotRevID = %q, want %q", got.LastBotRevID, want.LastBotRevID)
	}
	if got.LastKBCommit != want.LastKBCommit {
		t.Errorf("LastKBCommit = %q, want %q", got.LastKBCommit, want.LastKBCommit)
	}
	if !got.LastRunAt.Equal(want.LastRunAt) {
		t.Errorf("LastRunAt = %v, want %v", got.LastRunAt, want.LastRunAt)
	}
}

func TestSet_Overwrites(t *testing.T) {
	c := openTmp(t)
	defer func() { _ = c.Close() }()

	_ = c.Set("k", SpecState{LastBotRevID: "first"})
	_ = c.Set("k", SpecState{LastBotRevID: "second"})

	got, _, _ := c.Get("k")
	if got.LastBotRevID != "second" {
		t.Errorf("LastBotRevID = %q, want %q (Set overwrites)", got.LastBotRevID, "second")
	}
}

func TestPersistence_ReopenSeesPriorState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rs.bolt")

	c1, _ := Open(path)
	_ = c1.Set("k", SpecState{LastBotRevID: "persisted"})
	_ = c1.Close()

	c2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = c2.Close() }()

	got, ok, _ := c2.Get("k")
	if !ok || got.LastBotRevID != "persisted" {
		t.Errorf("Get on reopened cache = (%+v, %v), want persisted state", got, ok)
	}
}

func openTmp(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "rs.bolt"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}
