//go:build integration

// Package integration_test contains pyramid level 2 tests: real
// components talking to real components (not mocks) but in-process
// or via local containers.
//
// This file is the walking-skeleton scenario: it wires the
// Orchestrator end-to-end with in-process fakes (no containers yet)
// and asserts the run loop reaches a final report. It exists to
// prove the seams compile and the test infrastructure works; real
// integration tests (against MediaWiki, git, Pi) land in subsequent
// PRs alongside the concrete adapter implementations.
package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

type inMemKB struct{}

func (inMemKB) Pull(context.Context) (kb.Snapshot, error) {
	return kb.Snapshot{Commit: "skeleton-commit", ChangedAreas: []string{"vault"}}, nil
}
func (inMemKB) Whoami() string { return "in-memory" }

type inMemSpecs struct{ items []specs.Spec }

func (s inMemSpecs) Pull(context.Context) ([]specs.Spec, error) { return s.items, nil }
func (inMemSpecs) Whoami() string                               { return "in-memory" }

type inMemWiki struct{}

func (inMemWiki) Whoami(context.Context) (string, error) { return "User:Skeleton", nil }
func (inMemWiki) GetPage(context.Context, string) (*wiki.Page, error) {
	return nil, nil
}
func (inMemWiki) UpsertPage(context.Context, string, string, string) (wiki.Revision, error) {
	return wiki.Revision{ID: "rev-1", User: "User:Skeleton", IsBot: true}, nil
}
func (inMemWiki) History(context.Context, string, string) ([]wiki.Revision, error) {
	return nil, nil
}
func (inMemWiki) HumanEditsSinceBot(context.Context, string, string) (*wiki.HumanEdit, error) {
	return nil, nil
}

type inMemLLM struct{}

func (inMemLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{Text: "fixture", CacheHit: true}, nil
}

// TestOrchestrator_WalkingSkeleton wires the Orchestrator end-to-end
// with in-memory fakes and asserts that:
//   - The run completes without error.
//   - The report records the kb commit.
//   - Every spec gets a SpecResult (status is "skipped" in v0.0
//     because the rendering pipeline is not yet implemented).
func TestOrchestrator_WalkingSkeleton(t *testing.T) {
	o := orchestrator.New(orchestrator.Deps{
		Wiki: "skeleton",
		KB:   inMemKB{},
		Specs: inMemSpecs{items: []specs.Spec{
			{ID: "page-a", Wiki: "skeleton", Page: "PageA", Kind: "projection"},
			{ID: "page-b", Wiki: "skeleton", Page: "PageB", Kind: "editorial"},
			{ID: "page-c", Wiki: "skeleton", Page: "PageC", Kind: "hub"},
		}},
		WikiTarget: inMemWiki{},
		LLM:        inMemLLM{},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rep, err := o.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.KBCommit != "skeleton-commit" {
		t.Errorf("KBCommit = %q, want %q", rep.KBCommit, "skeleton-commit")
	}
	if got := len(rep.Specs); got != 3 {
		t.Fatalf("len(Specs) = %d, want 3", got)
	}
	for _, s := range rep.Specs {
		if s.Status != reporter.StatusSkipped {
			t.Errorf("spec %s: status = %q, want %q (v0.0 stub behaviour)", s.ID, s.Status, reporter.StatusSkipped)
		}
	}
	if len(rep.Errors) != 0 {
		t.Errorf("Errors = %v, want none", rep.Errors)
	}
}
