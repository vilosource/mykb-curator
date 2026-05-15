package linkrot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

var _ maintenance.Check = (*Check)(nil)

func TestName(t *testing.T) {
	if got := New(time.Second).Name(); got != "link-rot" {
		t.Errorf("Name = %q, want %q", got, "link-rot")
	}
}

func TestRun_LiveURL_NoProposal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "l1", Area: "vault", Type: "link", URL: srv.URL},
		},
	}}}
	got, err := New(time.Second).Run(context.Background(), snap)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("live URL produced %d proposals, want 0", len(got))
	}
}

func TestRun_DeadURL_ProposesDeprecate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "broken", Area: "vault", Type: "link", URL: srv.URL},
		},
	}}}
	got, err := New(time.Second).Run(context.Background(), snap)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Kind != maintenance.ProposalDeprecate {
		t.Errorf("Kind = %v, want ProposalDeprecate", got[0].Kind)
	}
	if got[0].Evidence["http_status"] != "404" {
		t.Errorf("Evidence[http_status] = %q, want 404", got[0].Evidence["http_status"])
	}
}

func TestRun_NonLinkEntries_Skipped(t *testing.T) {
	// fact/decision/etc. entries shouldn't be probed even if they
	// happen to have a URL field set (defense in depth).
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "f", Type: "fact", URL: "http://example.invalid"},
		},
	}}}
	got, _ := New(100*time.Millisecond).Run(context.Background(), snap)
	if len(got) != 0 {
		t.Errorf("non-link entries should be skipped; got %+v", got)
	}
}

func TestRun_EmptyURL_Skipped(t *testing.T) {
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "no-url", Type: "link", URL: ""},
		},
	}}}
	got, _ := New(time.Second).Run(context.Background(), snap)
	if len(got) != 0 {
		t.Errorf("empty-URL link entries should be skipped; got %+v", got)
	}
}

func TestRun_NetworkError_ProposesDeprecate(t *testing.T) {
	// Unreachable URL → propose deprecate with the network error in
	// evidence. (The check has a short timeout so the test doesn't
	// stall on a real DNS lookup.)
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "dead", Type: "link", URL: "http://localhost:1"}, // port 1 is reserved
		},
	}}}
	got, _ := New(500*time.Millisecond).Run(context.Background(), snap)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (network error proposes deprecation)", len(got))
	}
	if got[0].Kind != maintenance.ProposalDeprecate {
		t.Errorf("Kind = %v, want ProposalDeprecate", got[0].Kind)
	}
}
