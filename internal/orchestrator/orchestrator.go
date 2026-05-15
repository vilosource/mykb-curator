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
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/reconciler"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// Deps holds every collaborator the orchestrator needs. The
// composition root constructs concrete impls and passes them in;
// tests pass fakes.
//
// Frontends, Passes, and Backend are optional in v0.0.1: a nil
// Frontends registry causes the orchestrator to record specs as
// Skipped (the v0.0 walking-skeleton behaviour). Wiring them
// enables the full rendering pipeline.
type Deps struct {
	Wiki       string // tenant id, for the report
	KB         kb.Source
	Specs      specs.Store
	WikiTarget wiki.Target
	LLM        llm.Client

	// Frontends dispatches by spec.Kind to a Frontend that builds IR.
	// If nil, the rendering pipeline is not run; specs are reported
	// as Skipped.
	Frontends *frontends.Registry

	// Passes runs after each Frontend.Build, transforming the IR
	// (e.g., ApplyZoneMarkers). Empty pipeline = no transformation.
	Passes *passes.Pipeline

	// Backend renders the final IR to the target format. Required
	// when Frontends is set.
	Backend backends.Backend

	// OnRendered is an optional sink invoked after a spec renders
	// cleanly. The composition root attaches this to either a file
	// writer (CLI dump mode) or a wiki upserter (production). Nil =
	// no sink; the render still happens (so the run report is
	// meaningful).
	OnRendered func(specID string, rendered []byte, doc ir.Document) error
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

		rb.AddSpecResult(o.processSpec(ctx, s, snap))
	}

	return rb.Build(), nil
}

// processSpec runs the rendering pipeline for one spec and returns
// the SpecResult to record. If the rendering pipeline is not wired
// (no Frontends registry), the spec is recorded as Skipped — the
// v0.0 walking-skeleton behaviour, preserved so partial deployments
// still produce useful run reports.
func (o *Orchestrator) processSpec(ctx context.Context, s specs.Spec, snap kb.Snapshot) reporter.SpecResult {
	if o.deps.Frontends == nil {
		return reporter.SpecResult{
			ID:     s.ID,
			Status: reporter.StatusSkipped,
			Reason: "rendering pipeline not wired (orchestrator.Deps.Frontends == nil)",
		}
	}

	frontend, err := o.deps.Frontends.For(s.Kind)
	if err != nil {
		return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: err.Error()}
	}

	doc, err := frontend.Build(ctx, s, snap)
	if err != nil {
		return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: fmt.Errorf("frontend %s: %w", frontend.Name(), err).Error()}
	}

	if o.deps.Passes != nil {
		doc, err = o.deps.Passes.Apply(ctx, doc)
		if err != nil {
			return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: err.Error()}
		}
	}

	var rendered []byte
	if o.deps.Backend != nil {
		rendered, err = o.deps.Backend.Render(doc)
		if err != nil {
			return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: fmt.Errorf("backend %s: %w", o.deps.Backend.Name(), err).Error()}
		}
	}

	result := reporter.SpecResult{
		ID:                s.ID,
		Status:            reporter.StatusRendered,
		BlocksRegenerated: countBlocks(doc),
	}

	// Reconcile + push to the wiki when a real WikiTarget is wired.
	// The reconciler reads the current wiki state, detects human
	// edits since our last write, and tells us whether to upsert or
	// no-op. Acting on the Decision is the orchestrator's job.
	if rendered != nil && o.deps.WikiTarget != nil && s.Page != "" {
		pushResult, err := o.reconcileAndPush(ctx, s, rendered)
		if err != nil {
			return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: err.Error()}
		}
		result.HumanEdits = pushResult.HumanEdits
		result.NewRevisionID = pushResult.NewRevisionID
		if pushResult.NoOp {
			result.Status = reporter.StatusSkipped
			result.Reason = "no content change since last bot revision"
		}
	}

	if o.deps.OnRendered != nil && rendered != nil {
		if err := o.deps.OnRendered(s.ID, rendered, doc); err != nil {
			return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: err.Error()}
		}
	}

	return result
}

type pushResult struct {
	NoOp          bool
	NewRevisionID string
	HumanEdits    []reporter.HumanEditEvent
}

// reconcileAndPush runs the reconciler against the current wiki
// state and acts on the Decision. lastBotRevID is "" today (no
// cache yet) — first-render mode every time. When the run-state
// cache lands (v0.0.5), this becomes a lookup.
func (o *Orchestrator) reconcileAndPush(ctx context.Context, s specs.Spec, rendered []byte) (pushResult, error) {
	rec := reconciler.New(o.deps.WikiTarget)
	dec, err := rec.Reconcile(ctx, s.Page, rendered, "" /* lastBotRevID */)
	if err != nil {
		return pushResult{}, err
	}

	res := pushResult{
		HumanEdits: convertHumanEdits(dec.HumanEdits),
	}

	switch dec.Action {
	case reconciler.ActionNoOp:
		res.NoOp = true
		return res, nil
	case reconciler.ActionCreate, reconciler.ActionUpsert:
		summary := fmt.Sprintf("mykb-curator: spec=%s", s.ID)
		rev, err := o.deps.WikiTarget.UpsertPage(ctx, s.Page, string(rendered), summary)
		if err != nil {
			return pushResult{}, fmt.Errorf("upsert %q: %w", s.Page, err)
		}
		res.NewRevisionID = rev.ID
		return res, nil
	default:
		return pushResult{}, fmt.Errorf("unknown reconciler action %q", dec.Action)
	}
}

// convertHumanEdits maps reconciler types to reporter types. Two
// types because reporter must not depend on reconciler.
func convertHumanEdits(in []reconciler.HumanEditDetection) []reporter.HumanEditEvent {
	if len(in) == 0 {
		return nil
	}
	out := make([]reporter.HumanEditEvent, len(in))
	for i, e := range in {
		out[i] = reporter.HumanEditEvent{
			BlockID:     "page", // page-level for now; block-level in v0.5
			Action:      reporter.ActionOverwritten,
			Diff:        e.Diff,
			Explanation: fmt.Sprintf("human edit by %s after last bot write", e.Revision.User),
		}
	}
	return out
}

// countBlocks reports the total number of IR blocks across all
// sections. Used to populate the BlocksRegenerated field of the
// run report — gives operators a sense of how much content each
// spec produced this run.
func countBlocks(doc ir.Document) int {
	n := 0
	for _, sec := range doc.Sections {
		n += len(sec.Blocks)
	}
	return n
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
