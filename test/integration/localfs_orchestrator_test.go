//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// TestLocalFS_Orchestrator_HappyPath_AndGuardrail wires the real
// LocalFS spec store into the orchestrator, points it at the
// committed acme fixture directory, and asserts that:
//
//  1. The two valid specs (area-vault, azure-infrastructure) are
//     accepted and end up in the report as Skipped (v0.0.1 — the
//     rendering pipeline is still stubbed).
//  2. The deliberately-bad spec (_invalid-wrong-wiki, claims
//     wiki=widgetco) is rejected by the frontmatter guardrail and
//     recorded as Failed with an informative reason.
//
// This is pyramid level 2 — real component (LocalFS) talking to real
// component (Orchestrator), no mocks, but contained to a fixture
// directory committed in the repo.
func TestLocalFS_Orchestrator_HappyPath_AndGuardrail(t *testing.T) {
	store := localfs.New("../fixtures/specs/acme")
	o := orchestrator.New(orchestrator.Deps{
		Wiki:       "acme",
		KB:         inMemKB{},
		Specs:      store,
		WikiTarget: inMemWiki{},
		LLM:        inMemLLM{},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rep, err := o.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Specs) != 4 {
		t.Fatalf("len(Specs) = %d, want 4 (3 valid + 1 deliberately invalid); got: %+v", len(rep.Specs), rep.Specs)
	}

	var accepted, rejected int
	for _, s := range rep.Specs {
		switch s.Status {
		case reporter.StatusSkipped:
			accepted++
		case reporter.StatusFailed:
			rejected++
			if s.Reason == "" {
				t.Errorf("failed spec %s has empty Reason", s.ID)
			}
		default:
			t.Errorf("spec %s: unexpected status %q (v0.0.1 stub allows only Skipped or Failed)", s.ID, s.Status)
		}
	}
	if accepted != 3 {
		t.Errorf("accepted = %d, want 3 (area-vault + azure-infrastructure + projection-vault-smoke)", accepted)
	}
	if rejected != 1 {
		t.Errorf("rejected = %d, want 1 (_invalid-wrong-wiki frontmatter guardrail)", rejected)
	}
}
