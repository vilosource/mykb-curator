// Package maintenance implements the second curator pipeline (per
// DESIGN.md §6): kb-maintenance — fact-checking, link-rot, staleness
// — producing MutationProposals that the PR backend turns into
// branches and PRs against the kb repo.
//
// Architecture mirrors the rendering pipeline: a sequence of Checks
// (analogous to passes), each producing 0..N MutationProposals
// (analogous to IR transforms). The composed Pipeline runs all
// checks and aggregates their output.
//
// Concrete checks live in subpackages (checks/staleness,
// checks/linkrot, ...). The PR backend (prbackend/) consumes the
// aggregated proposals and opens a PR.
package maintenance

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
)

// ProposalKind enumerates the mutations a check can propose.
type ProposalKind int

const (
	// ProposalVerify: the entry has been confirmed against its source;
	// mark verified + record evidence.
	ProposalVerify ProposalKind = iota + 1

	// ProposalDeprecate: the entry is stale, contradicted, or its
	// source is gone; archive or annotate accordingly.
	ProposalDeprecate

	// ProposalAdd: new knowledge surfaced (e.g., external truth check
	// found a fact the kb is missing).
	ProposalAdd
)

// MutationProposal is the maintenance pipeline's IR. One proposal per
// (target entry, kind) — checks emit; PR backend merges.
type MutationProposal struct {
	// Kind is the mutation type.
	Kind ProposalKind

	// Area + ID identify the existing entry (Verify, Deprecate) or
	// the proposed location (Add).
	Area string
	ID   string

	// Text is the entry text. Required for Add; informational for
	// Verify/Deprecate.
	Text string

	// Reason is a short human-readable explanation surfaced in the
	// PR description.
	Reason string

	// Source is the check that produced this proposal — informational
	// (run reports + PR body).
	Source string

	// Evidence is an optional structured payload (e.g., HTTP status
	// code, source URL, contradiction pair). Free-form by design.
	Evidence map[string]string
}

// Check is one maintenance check. Implementations are deterministic
// by default; LLM-backed checks (e.g., contradiction detection) close
// over an llm.Client in their constructor.
type Check interface {
	// Name returns a stable identifier for run reports + PR bodies.
	Name() string

	// Run inspects the kb snapshot and emits proposals for whatever
	// the check is responsible for. Returning an error stops the
	// pipeline; checks should prefer "skip entry, emit no proposal,
	// continue" for routine failures.
	Run(ctx context.Context, snap kb.Snapshot) ([]MutationProposal, error)
}

// Pipeline runs a sequence of Checks against a snapshot.
type Pipeline struct {
	checks []Check
}

// NewPipeline constructs a Pipeline that runs the given checks in
// declared order. Empty pipeline is valid (returns no proposals).
func NewPipeline(checks ...Check) *Pipeline {
	return &Pipeline{checks: checks}
}

// Run executes every check in order, aggregating their proposals.
// Stops on the first error, wrapping it with the failing check's
// name so run reports can show which check broke.
func (p *Pipeline) Run(ctx context.Context, snap kb.Snapshot) ([]MutationProposal, error) {
	var out []MutationProposal
	for _, c := range p.checks {
		got, err := c.Run(ctx, snap)
		if err != nil {
			return out, fmt.Errorf("check %q: %w", c.Name(), err)
		}
		out = append(out, got...)
	}
	return out, nil
}

// Names returns the registered check names in execution order.
func (p *Pipeline) Names() []string {
	out := make([]string, len(p.checks))
	for i, c := range p.checks {
		out[i] = c.Name()
	}
	return out
}
