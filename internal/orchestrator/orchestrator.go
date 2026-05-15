// Package orchestrator is the top-level run loop. It composes
// adapters and pipelines but never knows about concrete
// implementations directly — wiring happens at the composition root
// (cmd/mykb-curator).
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// Deps holds every collaborator the orchestrator needs. The
// composition root constructs concrete impls and passes them in;
// tests pass fakes.
type Deps struct {
	Wiki      string // tenant id, for the report
	KB        kb.Source
	Specs     specs.Store
	WikiTarget wiki.Target
	LLM       llm.Client
}

// Orchestrator drives one curator run end-to-end.
type Orchestrator struct {
	deps Deps
}

// New constructs an Orchestrator from its dependencies.
func New(deps Deps) *Orchestrator { return &Orchestrator{deps: deps} }

// Run executes one curator pass: pull kb, pull specs, render each
// (currently stubbed), reconcile, push, emit a report.
//
// v0.0 walking skeleton: this exercises the wiring and the contract
// between adapters and the orchestrator. Rendering and reconcile are
// stubbed — they record SpecResults as Skipped with a "not yet
// implemented" reason. The shape of the run loop is real; the
// pipelines plug in incrementally.
func (o *Orchestrator) Run(ctx context.Context) (reporter.Report, error) {
	rb := reporter.NewBuilder(o.deps.Wiki, newRunID())

	snap, err := o.deps.KB.Pull(ctx)
	if err != nil {
		rb.AddError(fmt.Errorf("kb pull: %w", err))
		return rb.Build(), err
	}
	rb.SetKBCommit(snap.Commit)

	specList, err := o.deps.Specs.Pull(ctx)
	if err != nil {
		rb.AddError(fmt.Errorf("specs pull: %w", err))
		return rb.Build(), err
	}

	for _, s := range specList {
		if err := validateSpecForWiki(s, o.deps.Wiki); err != nil {
			rb.AddSpecResult(reporter.SpecResult{
				ID:     s.ID,
				Status: reporter.StatusFailed,
				Reason: err.Error(),
			})
			continue
		}

		// v0.0 stub: every spec is recorded as skipped until the
		// rendering pipeline lands.
		rb.AddSpecResult(reporter.SpecResult{
			ID:     s.ID,
			Status: reporter.StatusSkipped,
			Reason: "rendering pipeline not yet implemented (v0.0 walking skeleton)",
		})
	}

	return rb.Build(), nil
}

// validateSpecForWiki enforces the frontmatter-as-guardrail rule:
// a spec must self-identify as targeting the wiki we're running for.
// Cross-tenant mis-routing is a hard error.
func validateSpecForWiki(s specs.Spec, runWiki string) error {
	if s.Wiki == "" {
		return fmt.Errorf("spec %s: missing wiki: frontmatter field", s.ID)
	}
	if s.Wiki != runWiki {
		return fmt.Errorf("spec %s: declares wiki=%q but run is for wiki=%q", s.ID, s.Wiki, runWiki)
	}
	return nil
}

func newRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
