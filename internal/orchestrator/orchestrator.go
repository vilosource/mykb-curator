// Package orchestrator is the top-level run loop. It composes
// adapters and pipelines but never knows about concrete
// implementations directly — wiring happens at the composition root
// (cmd/mykb-curator).
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/docspecs"
	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/cache/ircache"
	"github.com/vilosource/mykb-curator/internal/cache/runstate"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/cluster"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/reconciler"
	"github.com/vilosource/mykb-curator/internal/reporter"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
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
	// Mutually exclusive with BuildPasses; if both are set,
	// BuildPasses wins.
	Passes *passes.Pipeline

	// BuildPasses constructs a per-run pipeline using the kb snapshot
	// the orchestrator just pulled. Used by passes that close over
	// the snapshot (e.g., ResolveKBRefs). When nil, Passes is used
	// directly; when set, called once per Run, after kb.Pull.
	BuildPasses func(snap kb.Snapshot) *passes.Pipeline

	// Backend renders the final IR to the target format. Required
	// when Frontends is set.
	Backend backends.Backend

	// OnRendered is an optional sink invoked after a spec renders
	// cleanly. The composition root attaches this to either a file
	// writer (CLI dump mode) or a wiki upserter (production). Nil =
	// no sink; the render still happens (so the run report is
	// meaningful).
	OnRendered func(specID string, rendered []byte, doc ir.Document) error

	// RunState is the persistent per-spec state cache. When set, the
	// reconciler's lastBotRevID is looked up here and the new
	// revision ID is written back after each upsert. Nil falls back
	// to first-render mode every run (acceptable but means human-
	// edit detection only catches edits between renders that
	// happened during the SAME run, which is not useful).
	RunState *runstate.Cache

	// IRCache memoises rendered IR by (spec_hash, kb_subset_hash,
	// pipeline_version). When set, the orchestrator skips the
	// Frontend.Build call on hit and replays the cached IR.
	// Especially valuable for LLM-driven editorial frontends.
	// Nil = always run the frontend.
	IRCache *ircache.Cache

	// PipelineVersion is included in the IR cache key. Bump it
	// whenever frontend/pass output behaviour changes so cached IR
	// from older code is invalidated. Defaults to "v1" if unset.
	PipelineVersion string

	// Maintenance is the kb-maintenance pipeline (staleness, link-rot,
	// etc.). When set, it runs after the spec loop against the same
	// kb snapshot. Nil = no maintenance.
	Maintenance *maintenance.Pipeline

	// OnMaintenance handles the proposals produced by the maintenance
	// pipeline (typically: open a PR via the PR backend). Called only
	// when Maintenance is set and produced ≥ 1 proposal. Errors are
	// recorded on the run report but do not fail the overall run —
	// the curated pages have already shipped.
	OnMaintenance func(proposals []maintenance.MutationProposal) error

	// DocSpecs + Cluster wire the SDD-for-docs path: one *.doc.yaml
	// topic → a cross-linked parent+children cluster. Both optional;
	// absent = no docspec path (legacy *.spec.md unaffected). Cluster
	// requires the architecture frontend (LLM-backed), so the
	// composition root only wires these when an LLM is present.
	DocSpecs docspecs.Store
	Cluster  *cluster.Cluster

	// Judge is the report-only output reviewer. When set, every
	// rendered cluster page is reviewed against its declared intent
	// and the verdict is surfaced on the run report. It NEVER blocks
	// a push — a failing verdict is a warning, not a failure.
	Judge *judge.Judge
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

	// Resolve the pass pipeline for this run: BuildPasses overrides
	// the static Passes if set, so per-run-bound passes (e.g.,
	// ResolveKBRefs that closes over the snapshot) work transparently.
	passPipeline := o.deps.Passes
	if o.deps.BuildPasses != nil {
		passPipeline = o.deps.BuildPasses(snap)
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

		if skip := o.diffDrivenSkip(ctx, s, snap); skip != nil {
			rb.AddSpecResult(*skip)
			continue
		}

		rb.AddSpecResult(o.processSpec(ctx, s, snap, passPipeline))
	}

	o.runDocSpecs(ctx, snap, passPipeline, rb)

	o.runMaintenance(ctx, snap, rb)

	return rb.Build(), nil
}

// runDocSpecs renders every *.doc.yaml cluster: one topic → a
// parent page + N cross-linked children. Each rendered page reuses
// the proven passes → backend → reconcile/push tail via a synthetic
// per-page spec. The Judge (when wired) reviews each page before
// push but is report-only — its verdict never blocks the upsert.
//
// Optional: absent DocSpecs/Cluster wiring is a silent no-op so
// legacy-only deployments are unaffected.
func (o *Orchestrator) runDocSpecs(ctx context.Context, snap kb.Snapshot, passPipeline *passes.Pipeline, rb *reporter.Builder) {
	if o.deps.DocSpecs == nil || o.deps.Cluster == nil {
		return
	}
	files, err := o.deps.DocSpecs.Pull(ctx)
	if err != nil {
		rb.AddError(fmt.Errorf("docspecs pull: %w", err))
		return
	}
	for _, f := range files {
		pages, err := o.deps.Cluster.Render(ctx, f.Spec, snap)
		if err != nil {
			rb.AddSpecResult(reporter.SpecResult{
				ID:     f.ID,
				Status: reporter.StatusFailed,
				Reason: fmt.Errorf("cluster render: %w", err).Error(),
			})
			continue
		}
		byTitle := docPagesByTitle(f.Spec)
		for _, p := range pages {
			rb.AddSpecResult(o.processClusterPage(ctx, f.ID, p, byTitle[p.Page], snap, passPipeline, rb))
		}
	}
}

// processClusterPage runs one cluster page through passes + backend,
// reviews it with the Judge (report-only), then reconciles + pushes
// it under a synthetic per-page spec ID so run-state + human-edit
// detection work exactly as for legacy specs.
func (o *Orchestrator) processClusterPage(ctx context.Context, dsID string, p cluster.RenderedPage, page docspec.DocPage, snap kb.Snapshot, passPipeline *passes.Pipeline, rb *reporter.Builder) reporter.SpecResult {
	id := dsID + "::" + p.Page
	doc := p.Doc

	var err error
	if passPipeline != nil {
		doc, err = passPipeline.Apply(ctx, doc)
		if err != nil {
			return reporter.SpecResult{ID: id, Status: reporter.StatusFailed, Reason: err.Error()}
		}
	}

	// Judge BEFORE push — report-only. A failing or inconclusive
	// verdict is a warning on the report, never a push gate.
	if o.deps.Judge != nil && page.Page != "" {
		if rep, jerr := o.deps.Judge.Review(ctx, page, doc); jerr != nil {
			rb.AddWarning(fmt.Sprintf("judge %q: %v", p.Page, jerr))
		} else if !rep.AllPass() {
			rb.AddWarning(fmt.Sprintf("judge %q: %s", p.Page, summariseVerdicts(rep)))
		}
	}

	if o.deps.Backend == nil || o.deps.WikiTarget == nil {
		return reporter.SpecResult{ID: id, Status: reporter.StatusRendered, BlocksRegenerated: countBlocks(doc)}
	}
	rendered, err := o.deps.Backend.Render(doc)
	if err != nil {
		return reporter.SpecResult{ID: id, Status: reporter.StatusFailed, Reason: fmt.Errorf("backend %s: %w", o.deps.Backend.Name(), err).Error()}
	}

	synthetic := specs.Spec{ID: id, Page: p.Page, Wiki: o.deps.Wiki}
	pr, err := o.reconcileAndPush(ctx, synthetic, rendered, snap.Commit)
	if err != nil {
		return reporter.SpecResult{ID: id, Status: reporter.StatusFailed, Reason: err.Error()}
	}
	res := reporter.SpecResult{
		ID:                id,
		Status:            reporter.StatusRendered,
		BlocksRegenerated: countBlocks(doc),
		HumanEdits:        pr.HumanEdits,
		NewRevisionID:     pr.NewRevisionID,
	}
	if pr.NoOp {
		res.Status = reporter.StatusSkipped
		res.Reason = "no content change since last bot revision"
	}
	if o.deps.OnRendered != nil {
		if err := o.deps.OnRendered(id, rendered, doc); err != nil {
			return reporter.SpecResult{ID: id, Status: reporter.StatusFailed, Reason: err.Error()}
		}
	}
	return res
}

func docPagesByTitle(s docspec.DocSpec) map[string]docspec.DocPage {
	m := map[string]docspec.DocPage{s.Parent.Page: s.Parent}
	for _, c := range s.Children {
		m[c.Page] = c
	}
	return m
}

func summariseVerdicts(r judge.Report) string {
	var b strings.Builder
	for _, v := range r.Verdicts {
		if v.Pass && !v.Inconclusive {
			continue
		}
		state := "FAIL"
		if v.Inconclusive {
			state = "INCONCLUSIVE"
		}
		fmt.Fprintf(&b, "[%s %s: %s] ", state, v.Section, v.Reason)
	}
	return strings.TrimSpace(b.String())
}

// runMaintenance executes the maintenance pipeline if configured.
// Errors are recorded on the report but don't fail the run — the
// page renders have already happened; maintenance is supplementary.
func (o *Orchestrator) runMaintenance(ctx context.Context, snap kb.Snapshot, rb *reporter.Builder) {
	if o.deps.Maintenance == nil {
		return
	}
	proposals, err := o.deps.Maintenance.Run(ctx, snap)
	if err != nil {
		rb.AddError(fmt.Errorf("maintenance: %w", err))
		return
	}
	rb.AddWarning(fmt.Sprintf("maintenance produced %d proposal(s)", len(proposals)))
	if len(proposals) == 0 || o.deps.OnMaintenance == nil {
		return
	}
	if err := o.deps.OnMaintenance(proposals); err != nil {
		rb.AddError(fmt.Errorf("maintenance handler: %w", err))
	}
}

// diffDrivenSkip decides whether to skip a spec because nothing it
// cares about has changed since the last successful render. Returns
// a SpecResult{Status=Skipped} if so, nil if the spec should render.
//
// Skip conditions (all must hold):
//   - cache has prior state for this spec (LastKBCommit non-empty)
//   - kb source can compute a diff (not ErrDiffNotSupported)
//   - the diff does NOT intersect spec.Include.Areas
//
// First-render specs (no prior state) always render. Local kb
// sources (no diff support) always render — conservative fallback.
func (o *Orchestrator) diffDrivenSkip(ctx context.Context, s specs.Spec, snap kb.Snapshot) *reporter.SpecResult {
	if o.deps.RunState == nil {
		return nil
	}
	st, ok, err := o.deps.RunState.Get(s.ID)
	if err != nil || !ok || st.LastKBCommit == "" {
		return nil // first render or cache read failed — render to be safe
	}
	if st.LastKBCommit == snap.Commit {
		// kb didn't move at all; any spec touching this kb is
		// guaranteed to produce identical IR (modulo non-kb inputs
		// like spec edits — but spec edits change spec.Hash, which
		// already invalidates the IR cache when we add it).
		return &reporter.SpecResult{
			ID:     s.ID,
			Status: reporter.StatusSkipped,
			Reason: fmt.Sprintf("kb unchanged since last run (commit %s)", snap.Commit),
		}
	}
	changed, err := o.deps.KB.DiffSince(ctx, st.LastKBCommit)
	if errors.Is(err, kb.ErrDiffNotSupported) {
		return nil // render
	}
	if err != nil {
		// Diff failed for an unexpected reason — fall back to render.
		// Safer to do work than skip silently.
		return nil
	}
	if intersects(s.Include.Areas, changed) {
		return nil // includes touched; render
	}
	return &reporter.SpecResult{
		ID:     s.ID,
		Status: reporter.StatusSkipped,
		Reason: fmt.Sprintf("no kb changes in declared includes since commit %s (changed: %v)", st.LastKBCommit, changed),
	}
}

func intersects(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if set[y] {
			return true
		}
	}
	return false
}

// processSpec runs the rendering pipeline for one spec and returns
// the SpecResult to record. If the rendering pipeline is not wired
// (no Frontends registry), the spec is recorded as Skipped — the
// v0.0 walking-skeleton behaviour, preserved so partial deployments
// still produce useful run reports.
func (o *Orchestrator) processSpec(ctx context.Context, s specs.Spec, snap kb.Snapshot, passPipeline *passes.Pipeline) reporter.SpecResult {
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

	doc, err := o.buildOrReuseIR(ctx, s, snap, frontend)
	if err != nil {
		return reporter.SpecResult{ID: s.ID, Status: reporter.StatusFailed, Reason: fmt.Errorf("frontend %s: %w", frontend.Name(), err).Error()}
	}

	if passPipeline != nil {
		doc, err = passPipeline.Apply(ctx, doc)
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
		pushResult, err := o.reconcileAndPush(ctx, s, rendered, snap.Commit)
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
// state and acts on the Decision. When o.deps.RunState is set, the
// previous run's bot revision is looked up so the reconciler can
// detect human edits since that point; otherwise first-render mode.
func (o *Orchestrator) reconcileAndPush(ctx context.Context, s specs.Spec, rendered []byte, kbCommit string) (pushResult, error) {
	lastBotRevID := o.lookupLastBotRev(s.ID)

	rec := reconciler.New(o.deps.WikiTarget)
	dec, err := rec.Reconcile(ctx, s.Page, rendered, lastBotRevID)
	if err != nil {
		return pushResult{}, err
	}

	res := pushResult{
		HumanEdits: convertHumanEdits(dec.HumanEdits),
	}

	switch dec.Action {
	case reconciler.ActionNoOp:
		res.NoOp = true
		// Persist updated last-run kb-commit even on no-op so the
		// next run's diff-driven skip works against this commit.
		o.persistRunState(s.ID, lastBotRevID, kbCommit)
		return res, nil
	case reconciler.ActionCreate, reconciler.ActionUpsert:
		summary := fmt.Sprintf("mykb-curator: spec=%s", s.ID)
		// Use the reconciler's merged content (which preserves human
		// polish on editorial blocks) rather than the raw new render.
		// For ActionCreate the merge is just the new render verbatim.
		content := dec.MergedContent
		if content == "" {
			content = string(rendered)
		}
		rev, err := o.deps.WikiTarget.UpsertPage(ctx, s.Page, content, summary)
		if err != nil {
			return pushResult{}, fmt.Errorf("upsert %q: %w", s.Page, err)
		}
		res.NewRevisionID = rev.ID
		o.persistRunState(s.ID, rev.ID, kbCommit)
		return res, nil
	default:
		return pushResult{}, fmt.Errorf("unknown reconciler action %q", dec.Action)
	}
}

func (o *Orchestrator) lookupLastBotRev(specID string) string {
	if o.deps.RunState == nil {
		return ""
	}
	st, ok, err := o.deps.RunState.Get(specID)
	if err != nil || !ok {
		return ""
	}
	return st.LastBotRevID
}

func (o *Orchestrator) persistRunState(specID, revID, kbCommit string) {
	if o.deps.RunState == nil {
		return
	}
	// Best-effort write — a cache failure must not fail the run.
	// The run report will record the upsert; next run with a working
	// cache will pick up.
	_ = o.deps.RunState.Set(specID, runstate.SpecState{
		LastBotRevID: revID,
		LastKBCommit: kbCommit,
		LastRunAt:    time.Now().UTC(),
	})
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

// buildOrReuseIR returns the IR for a spec, either from the IR cache
// (when configured and hit) or by calling the frontend (then
// persisting). The cache key combines spec content, the spec's kb
// subset, and the pipeline version so unrelated changes elsewhere
// don't invalidate the cache.
func (o *Orchestrator) buildOrReuseIR(ctx context.Context, s specs.Spec, snap kb.Snapshot, frontend frontends.Frontend) (ir.Document, error) {
	if o.deps.IRCache == nil || s.Hash == "" {
		// Cache disabled or spec has no content hash → no memoisation.
		return frontend.Build(ctx, s, snap)
	}
	key := ircache.Key(s.Hash, ircache.HashKBSubset(snap, s.Include), o.pipelineVersion())
	if cached, ok, err := o.deps.IRCache.Get(key); err == nil && ok {
		return *cached, nil
	}
	doc, err := frontend.Build(ctx, s, snap)
	if err != nil {
		return ir.Document{}, err
	}
	// Persist best-effort: a write failure shouldn't fail the run.
	_ = o.deps.IRCache.Set(key, doc)
	return doc, nil
}

func (o *Orchestrator) pipelineVersion() string {
	if o.deps.PipelineVersion == "" {
		return "v1"
	}
	return o.deps.PipelineVersion
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
