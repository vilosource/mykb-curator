package staleness

import (
	"context"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

var _ maintenance.Check = (*Check)(nil)

func TestName(t *testing.T) {
	if got := New(30 * 24 * time.Hour).Name(); got != "staleness" {
		t.Errorf("Name = %q, want %q", got, "staleness")
	}
}

// rfc3339 helper for the test fixtures.
func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func TestRun_FreshEntries_NoProposals(t *testing.T) {
	now := time.Now().UTC()
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "f1", Area: "vault", Type: "fact", Updated: rfc(now.Add(-1 * time.Hour))},
		},
	}}}
	got, err := New(30*24*time.Hour).Run(context.Background(), snap)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 (entry is fresh)", len(got))
	}
}

func TestRun_StaleEntries_ProposeDeprecate(t *testing.T) {
	now := time.Now().UTC()
	stale := now.Add(-90 * 24 * time.Hour)
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "old", Area: "vault", Type: "fact", Updated: rfc(stale)},
		},
	}}}
	got, err := New(30*24*time.Hour).Run(context.Background(), snap)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Kind != maintenance.ProposalDeprecate {
		t.Errorf("Kind = %v, want ProposalDeprecate", got[0].Kind)
	}
	if got[0].Area != "vault" || got[0].ID != "old" {
		t.Errorf("Area/ID mismatch: %+v", got[0])
	}
	if got[0].Source != "staleness" {
		t.Errorf("Source = %q, want staleness", got[0].Source)
	}
}

func TestRun_VerifiedEntries_SkippedEvenIfStale(t *testing.T) {
	// Already-verified entries are by definition fresh-enough (a human
	// confirmed them); don't propose deprecation on them. They'll be
	// caught by a separate re-verification check later.
	stale := time.Now().UTC().Add(-365 * 24 * time.Hour)
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "verified", Area: "vault", Type: "fact", Updated: rfc(stale),
				Provenance: kb.EntryProvenance{Status: "verified"}},
		},
	}}}
	got, _ := New(30*24*time.Hour).Run(context.Background(), snap)
	if len(got) != 0 {
		t.Errorf("expected verified entry to be skipped; got %+v", got)
	}
}

func TestRun_MissingTimestamp_Skipped(t *testing.T) {
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "no-date", Area: "vault", Type: "fact", Updated: ""},
		},
	}}}
	got, _ := New(30*24*time.Hour).Run(context.Background(), snap)
	if len(got) != 0 {
		t.Errorf("missing-Updated should be skipped (no decision possible); got %+v", got)
	}
}

func TestRun_ArchivedEntries_Skipped(t *testing.T) {
	// Archived entries shouldn't generate proposals — they're already
	// out of active circulation.
	stale := time.Now().UTC().Add(-365 * 24 * time.Hour)
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "archived", Area: "vault", Type: "fact", Updated: rfc(stale), Zone: "archived"},
		},
	}}}
	got, _ := New(30*24*time.Hour).Run(context.Background(), snap)
	if len(got) != 0 {
		t.Errorf("archived entries should be skipped; got %+v", got)
	}
}
