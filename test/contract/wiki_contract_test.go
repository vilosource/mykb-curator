//go:build contract

package contract_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/memory"
)

// wikiBuilder constructs a fresh WikiTarget pre-seeded with no pages.
// Each impl needs its own builder; the suite asserts behaviour
// against the constructed Target.
type wikiBuilder func(t *testing.T) wiki.Target

var allWikiTargets = map[string]wikiBuilder{
	"memory": func(_ *testing.T) wiki.Target { return memory.New("User:Bot") },
	// "mediawiki": lands next slice once the testcontainers-go bootstrap
	// is wired.
}

func TestWikiTarget_Contract(t *testing.T) {
	for name, build := range allWikiTargets {
		t.Run(name, func(t *testing.T) {
			WikiTargetContractSuite(t, build(t))
		})
	}
}

// WikiTargetContractSuite asserts behavioural properties every
// wiki.Target impl must satisfy. New impls just register in
// allWikiTargets — no code duplication.
func WikiTargetContractSuite(t *testing.T, tgt wiki.Target) {
	t.Helper()
	ctx := context.Background()

	t.Run("Whoami non-empty", func(t *testing.T) {
		got, err := tgt.Whoami(ctx)
		if err != nil {
			t.Fatalf("Whoami: %v", err)
		}
		if got == "" {
			t.Errorf("Whoami is empty")
		}
	})

	t.Run("GetPage missing returns nil", func(t *testing.T) {
		p, err := tgt.GetPage(ctx, "WikiContract_DoesNotExist_99999")
		if err != nil {
			t.Errorf("GetPage error: %v", err)
		}
		if p != nil {
			t.Errorf("expected nil for missing page, got %+v", p)
		}
	})

	t.Run("Upsert then GetPage round-trip", func(t *testing.T) {
		title := "WikiContract_Roundtrip"
		_, err := tgt.UpsertPage(ctx, title, "round-trip content", "test")
		if err != nil {
			t.Fatalf("UpsertPage: %v", err)
		}
		p, err := tgt.GetPage(ctx, title)
		if err != nil || p == nil {
			t.Fatalf("GetPage: %v / %+v", err, p)
		}
		if p.Content != "round-trip content" {
			t.Errorf("Content = %q, want %q", p.Content, "round-trip content")
		}
		if !p.LatestRevision.IsBot {
			t.Errorf("LatestRevision.IsBot = false, want true (bot wrote it)")
		}
	})

	t.Run("Upsert records distinct revisions", func(t *testing.T) {
		title := "WikiContract_TwoRevs"
		r1, _ := tgt.UpsertPage(ctx, title, "v1", "first")
		r2, _ := tgt.UpsertPage(ctx, title, "v2", "second")
		if r1.ID == r2.ID {
			t.Errorf("revisions must have distinct IDs; got %q twice", r1.ID)
		}
		hist, err := tgt.History(ctx, title, "")
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		if len(hist) < 2 {
			t.Errorf("len(History) = %d, want ≥ 2", len(hist))
		}
		// Newest-first ordering is part of the contract.
		if hist[0].ID != r2.ID {
			t.Errorf("History[0] = %q, want newest %q", hist[0].ID, r2.ID)
		}
	})

	t.Run("HumanEditsSinceBot returns nil when no human edits", func(t *testing.T) {
		title := "WikiContract_BotOnly"
		r1, _ := tgt.UpsertPage(ctx, title, "v1", "first")
		_, _ = tgt.UpsertPage(ctx, title, "v2", "second")
		got, err := tgt.HumanEditsSinceBot(ctx, title, r1.ID)
		if err != nil {
			t.Fatalf("HumanEditsSinceBot: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("UploadFile returns a non-empty asset ref", func(t *testing.T) {
		ref, err := tgt.UploadFile(ctx, "WikiContract_Asset.png", []byte("\x89PNG\r\n"), "image/png", "test upload")
		if err != nil {
			t.Fatalf("UploadFile: %v", err)
		}
		if ref == "" {
			t.Errorf("UploadFile returned an empty asset ref")
		}
	})

	t.Run("UploadFile is idempotent for identical filename+content", func(t *testing.T) {
		content := []byte("\x89PNG\r\nidem")
		r1, err := tgt.UploadFile(ctx, "WikiContract_Idem.png", content, "image/png", "first")
		if err != nil {
			t.Fatalf("UploadFile #1: %v", err)
		}
		r2, err := tgt.UploadFile(ctx, "WikiContract_Idem.png", content, "image/png", "second")
		if err != nil {
			t.Fatalf("UploadFile #2 (same name+content must not error): %v", err)
		}
		if r1 != r2 {
			t.Errorf("idempotent re-upload changed ref: %q -> %q", r1, r2)
		}
	})
}
