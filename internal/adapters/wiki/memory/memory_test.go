package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
)

var _ wiki.Target = (*Target)(nil)

func TestWhoami_ReturnsConfiguredIdentity(t *testing.T) {
	tgt := New("User:Test")
	got, err := tgt.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got != "User:Test" {
		t.Errorf("Whoami = %q, want %q", got, "User:Test")
	}
}

func TestGetPage_NotPresent_ReturnsNil(t *testing.T) {
	page, err := New("U").GetPage(context.Background(), "Nope")
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if page != nil {
		t.Errorf("expected nil for missing page, got %+v", page)
	}
}

func TestUpsertPage_CreateAndRead(t *testing.T) {
	tgt := New("User:Bot")
	rev, err := tgt.UpsertPage(context.Background(), "P", "hello", "first")
	if err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	if rev.ID == "" {
		t.Errorf("revision id empty")
	}
	if !rev.IsBot {
		t.Errorf("IsBot = false, want true (bot identity should mark its own revisions)")
	}
	if rev.User != "User:Bot" {
		t.Errorf("User = %q, want %q", rev.User, "User:Bot")
	}

	got, _ := tgt.GetPage(context.Background(), "P")
	if got == nil || got.Content != "hello" {
		t.Errorf("GetPage returned %+v, want content=hello", got)
	}
}

func TestUpsertPage_UpdateAppendsRevision(t *testing.T) {
	tgt := New("User:Bot")
	r1, _ := tgt.UpsertPage(context.Background(), "P", "v1", "first")
	r2, _ := tgt.UpsertPage(context.Background(), "P", "v2", "second")

	if r1.ID == r2.ID {
		t.Errorf("revisions must have distinct IDs; got both %q", r1.ID)
	}

	page, _ := tgt.GetPage(context.Background(), "P")
	if page.Content != "v2" {
		t.Errorf("Content = %q, want %q (latest)", page.Content, "v2")
	}

	hist, _ := tgt.History(context.Background(), "P", "")
	if len(hist) != 2 {
		t.Fatalf("len(History) = %d, want 2", len(hist))
	}
	// History returns newest first per the contract.
	if hist[0].ID != r2.ID {
		t.Errorf("history[0] = %q, want newest %q", hist[0].ID, r2.ID)
	}
}

func TestHumanEditsSinceBot_OnlyBotRevisions_ReturnsNil(t *testing.T) {
	tgt := New("User:Bot")
	r1, _ := tgt.UpsertPage(context.Background(), "P", "v1", "first")
	_, _ = tgt.UpsertPage(context.Background(), "P", "v2", "second")

	got, err := tgt.HumanEditsSinceBot(context.Background(), "P", r1.ID)
	if err != nil {
		t.Fatalf("HumanEditsSinceBot: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil (no human edits between bot revisions); got %+v", got)
	}
}

func TestHumanEditsSinceBot_AfterHumanEdit_ReturnsTheEdit(t *testing.T) {
	tgt := New("User:Bot")
	r1, _ := tgt.UpsertPage(context.Background(), "P", "v1-bot", "first")
	// Simulate a human editing the page directly.
	tgt.simulateHumanEdit("P", "User:Alice", "v1-human", "drive-by fix")

	got, err := tgt.HumanEditsSinceBot(context.Background(), "P", r1.ID)
	if err != nil {
		t.Fatalf("HumanEditsSinceBot: %v", err)
	}
	if got == nil {
		t.Fatalf("expected human edit detected, got nil")
	}
	if got.Revision.User != "User:Alice" {
		t.Errorf("User = %q, want %q", got.Revision.User, "User:Alice")
	}
	if got.Revision.IsBot {
		t.Errorf("flagged revision is marked IsBot")
	}
}

func TestUpsertPage_NormalizesTitle(t *testing.T) {
	// MW treats spaces and underscores interchangeably in titles. The
	// in-memory impl should match that — Foo_Bar and "Foo Bar" map to
	// the same page so tests written against either retrieve the
	// same content.
	tgt := New("U")
	_, _ = tgt.UpsertPage(context.Background(), "Foo Bar", "content", "summary")
	page, _ := tgt.GetPage(context.Background(), "Foo_Bar")
	if page == nil {
		t.Errorf("title normalization missing: 'Foo Bar' and 'Foo_Bar' should resolve to same page")
		return
	}
	if !strings.Contains(page.Content, "content") {
		t.Errorf("content mismatch")
	}
}
