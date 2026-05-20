// Package refine is the closed Judge loop (DESIGN §5.7): it promotes the
// report-only Judge from "is it good?" to "make it good" by, on a
// failing verdict, re-synthesizing the failing sections with the verdict
// injected as feedback, re-running the passes, and re-judging — bounded,
// then publishing best-effort with the final verdict recorded.
//
// The loop owns pass application and works on the PRE-pass cluster doc:
// revisions are spliced into the pre-pass base and the passes run once
// per iteration on the whole doc, so non-idempotent passes (e.g. zone
// markers) are never double-applied to an already-passed section.
//
// Deterministic given its injected collaborators; all three (Reviser,
// Reviewer, Passes) are interfaces a test fakes.
package refine

import (
	"context"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Reviser re-synthesizes one section with a prior Judge verdict injected
// as feedback. *architecture.Frontend satisfies it.
type Reviser interface {
	ReviseSection(ctx context.Context, page docspec.DocPage, sec docspec.DocSection, snap kb.Snapshot, fb architecture.SectionFeedback) (ir.Section, error)
}

// Reviewer judges a rendered page. *judge.Judge satisfies it.
type Reviewer interface {
	Review(ctx context.Context, page docspec.DocPage, doc ir.Document, grounding map[string]string) (judge.Report, error)
}

// Passes applies the deterministic pass pipeline to a doc.
// *passes.Pipeline satisfies it; nil means "no passes".
type Passes interface {
	Apply(ctx context.Context, doc ir.Document) (ir.Document, error)
}

// Loop runs the bounded refine cycle.
type Loop struct {
	reviser  Reviser
	reviewer Reviewer
	passes   Passes // may be nil
	maxIters int
}

// NewLoop binds a Loop. maxIters is the refinement budget (0 = off,
// i.e. report-only — the page is judged once and published as drafted).
func NewLoop(r Reviser, j Reviewer, p Passes, maxIters int) *Loop {
	return &Loop{reviser: r, reviewer: j, passes: p, maxIters: maxIters}
}

// Result is the outcome of a refine run.
type Result struct {
	Doc        ir.Document  // final post-pass doc to push
	Report     judge.Report // final verdict
	Iterations int          // refine rounds performed (0 = published as first drafted)
}

// Run judges preDoc (the pre-pass cluster doc for one page), and while
// the verdict fails, re-synthesizes the failing sections with feedback,
// re-applies the passes, and re-judges — until AllPass, the budget is
// spent, or no progress is made. It returns the final post-pass doc,
// the final verdict, and the number of refine rounds performed.
func (l *Loop) Run(ctx context.Context, page docspec.DocPage, preDoc ir.Document, snap kb.Snapshot, grounding map[string]string) (Result, error) {
	work := preDoc // pre-pass base we splice revisions into

	applied, err := l.apply(ctx, work)
	if err != nil {
		return Result{}, err
	}
	rep, err := l.reviewer.Review(ctx, page, applied, grounding)
	if err != nil {
		return Result{}, err
	}

	iters := 0
	if l.maxIters <= 0 {
		return Result{Doc: applied, Report: rep, Iterations: 0}, nil // report-only
	}

	prevFail := failingCount(rep)
	for iters < l.maxIters && !rep.AllPass() {
		revisedAny := false
		for _, v := range rep.Verdicts {
			if v.Pass && !v.Inconclusive {
				continue // section already satisfies its contract
			}
			sec, ok := proseSection(page, v.Section)
			if !ok {
				continue // not a re-synthesizable prose section
			}
			newSec, err := l.reviser.ReviseSection(ctx, page, sec, snap, feedback(v))
			if err != nil {
				return Result{}, err
			}
			if spliceSection(&work, newSec) {
				revisedAny = true
			}
		}
		if !revisedAny {
			break // nothing actionable to revise
		}

		applied, err = l.apply(ctx, work)
		if err != nil {
			return Result{}, err
		}
		rep, err = l.reviewer.Review(ctx, page, applied, grounding)
		if err != nil {
			return Result{}, err
		}
		iters++

		nowFail := failingCount(rep)
		if nowFail >= prevFail {
			break // no progress — stop rather than burn the budget
		}
		prevFail = nowFail
	}

	return Result{Doc: applied, Report: rep, Iterations: iters}, nil
}

func (l *Loop) apply(ctx context.Context, doc ir.Document) (ir.Document, error) {
	if l.passes == nil {
		return doc, nil
	}
	return l.passes.Apply(ctx, doc)
}

// feedback maps a failing Judge verdict to the frontend's feedback type.
func feedback(v judge.Verdict) architecture.SectionFeedback {
	return architecture.SectionFeedback{
		Reason:           v.Reason,
		UngroundedClaims: v.UngroundedClaims,
	}
}

// failingCount counts verdicts that did not cleanly pass (a failure or
// an inconclusive review).
func failingCount(rep judge.Report) int {
	n := 0
	for _, v := range rep.Verdicts {
		if !v.Pass || v.Inconclusive {
			n++
		}
	}
	return n
}

// proseSection finds the judged prose DocSection with the given title.
// Structural sections (render:*) and sections with no intent carry no
// narrative contract, so they are not re-synthesizable here.
func proseSection(page docspec.DocPage, title string) (docspec.DocSection, bool) {
	for _, s := range page.Sections {
		if s.Title == title && s.Render == "" && s.Intent != "" {
			return s, true
		}
	}
	return docspec.DocSection{}, false
}

// spliceSection replaces the section whose heading matches sec.Heading
// with sec, in place. Returns whether a match was found.
func spliceSection(doc *ir.Document, sec ir.Section) bool {
	for i := range doc.Sections {
		if doc.Sections[i].Heading == sec.Heading {
			doc.Sections[i] = sec
			return true
		}
	}
	return false
}
