package refine

import (
	"context"
	"errors"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// fakeReviser returns a fresh single-block section keyed by the section
// title and records every revision it was asked to make.
type fakeReviser struct {
	calls []string // section titles revised, in order
	err   error
}

func (r *fakeReviser) ReviseSection(_ context.Context, _ docspec.DocPage, sec docspec.DocSection, _ kb.Snapshot, _ architecture.SectionFeedback) (ir.Section, error) {
	if r.err != nil {
		return ir.Section{}, r.err
	}
	r.calls = append(r.calls, sec.Title)
	return ir.Section{
		Heading: sec.Title,
		Blocks:  []ir.Block{ir.ProseBlock{Text: "revised " + sec.Title}},
	}, nil
}

// seqReviewer returns a scripted sequence of reports and records how
// many times it was called.
type seqReviewer struct {
	reports []judge.Report
	calls   int
	err     error
}

func (s *seqReviewer) Review(_ context.Context, _ docspec.DocPage, _ ir.Document, _ map[string]string) (judge.Report, error) {
	if s.err != nil {
		return judge.Report{}, s.err
	}
	i := s.calls
	s.calls++
	if i < len(s.reports) {
		return s.reports[i], nil
	}
	return s.reports[len(s.reports)-1], nil // hold the last report
}

// identityPasses returns the doc unchanged and counts applications.
type identityPasses struct{ calls int }

func (p *identityPasses) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	p.calls++
	return doc, nil
}

func pass(section string) judge.Verdict { return judge.Verdict{Section: section, Pass: true} }
func fail(section string) judge.Verdict {
	return judge.Verdict{Section: section, Pass: false, Reason: "missed an item"}
}
func report(vs ...judge.Verdict) judge.Report {
	return judge.Report{Page: "P", Verdicts: vs}
}

// twoProse is a page with two judged prose sections, A and B.
func twoProse() docspec.DocPage {
	return docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{
			{Title: "A", Intent: "cover A"},
			{Title: "B", Intent: "cover B"},
		},
	}
}

// preDoc is the pre-pass cluster doc with both sections present so the
// loop can splice revisions in by heading.
func preDoc() ir.Document {
	return ir.Document{Sections: []ir.Section{
		{Heading: "A", Blocks: []ir.Block{ir.ProseBlock{Text: "draft A"}}},
		{Heading: "B", Blocks: []ir.Block{ir.ProseBlock{Text: "draft B"}}},
	}}
}

func TestRun_PassesFirstTry_NoRevision(t *testing.T) {
	rev := &fakeReviser{}
	jr := &seqReviewer{reports: []judge.Report{report(pass("A"), pass("B"))}}
	p := &identityPasses{}
	res, err := NewLoop(rev, jr, 3).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 0 {
		t.Errorf("Iterations = %d, want 0 (passed first try)", res.Iterations)
	}
	if len(rev.calls) != 0 {
		t.Errorf("reviser called %v, want no revisions on a first-try pass", rev.calls)
	}
	if !res.Report.AllPass() {
		t.Errorf("final report should AllPass")
	}
}

func TestRun_FailsThenPasses_RevisesOnlyFailingSection(t *testing.T) {
	rev := &fakeReviser{}
	// Round 0: A fails, B passes. Round 1 (after revising A): both pass.
	jr := &seqReviewer{reports: []judge.Report{
		report(fail("A"), pass("B")),
		report(pass("A"), pass("B")),
	}}
	res, err := NewLoop(rev, jr, 3).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, &identityPasses{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1", res.Iterations)
	}
	if len(rev.calls) != 1 || rev.calls[0] != "A" {
		t.Errorf("reviser calls = %v, want exactly [A] (only the failing section)", rev.calls)
	}
	if !res.Report.AllPass() {
		t.Errorf("final report should AllPass after one refine round")
	}
	// The revised content must be spliced into the published doc.
	if got := proseOf(res.Doc, "A"); got != "revised A" {
		t.Errorf("section A in published doc = %q, want %q", got, "revised A")
	}
	if got := proseOf(res.Doc, "B"); got != "draft B" {
		t.Errorf("section B should be untouched, got %q", got)
	}
}

func TestRun_BudgetExhausted_PublishesBestEffort(t *testing.T) {
	rev := &fakeReviser{}
	// Failing count strictly decreases (progress each round) but never
	// reaches zero within the budget of 2.
	jr := &seqReviewer{reports: []judge.Report{
		report(fail("A"), fail("B")), // 2 failing
		report(pass("A"), fail("B")), // 1 failing
		report(pass("A"), fail("B")), // still 1 failing (budget hit before this matters)
	}}
	res, err := NewLoop(rev, jr, 2).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, &identityPasses{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 2 {
		t.Errorf("Iterations = %d, want 2 (budget exhausted)", res.Iterations)
	}
	if res.Report.AllPass() {
		t.Errorf("final report should still be failing (best-effort publish)")
	}
}

func TestRun_NoProgress_EarlyStop(t *testing.T) {
	rev := &fakeReviser{}
	// Failing count does not shrink between rounds → stop after one round
	// even though the budget is large.
	jr := &seqReviewer{reports: []judge.Report{
		report(fail("A"), fail("B")), // 2 failing
		report(fail("A"), fail("B")), // still 2 failing → no progress
	}}
	res, err := NewLoop(rev, jr, 5).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, &identityPasses{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 1 {
		t.Errorf("Iterations = %d, want 1 (no-progress early stop)", res.Iterations)
	}
}

func TestRun_LoopOff_IsReportOnly(t *testing.T) {
	rev := &fakeReviser{}
	jr := &seqReviewer{reports: []judge.Report{report(fail("A"), pass("B"))}}
	res, err := NewLoop(rev, jr, 0).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, &identityPasses{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 0 || len(rev.calls) != 0 {
		t.Errorf("maxIters 0 must be report-only: iters=%d calls=%v", res.Iterations, rev.calls)
	}
	if res.Report.AllPass() {
		t.Errorf("report-only must still surface the failing verdict")
	}
}

func TestRun_NilPasses_Works(t *testing.T) {
	rev := &fakeReviser{}
	jr := &seqReviewer{reports: []judge.Report{
		report(fail("A"), pass("B")),
		report(pass("A"), pass("B")),
	}}
	res, err := NewLoop(rev, jr, 3).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Iterations != 1 || !res.Report.AllPass() {
		t.Errorf("nil passes should still loop: iters=%d allpass=%v", res.Iterations, res.Report.AllPass())
	}
}

func TestRun_ReviserError_Propagates(t *testing.T) {
	rev := &fakeReviser{err: errors.New("llm down")}
	jr := &seqReviewer{reports: []judge.Report{report(fail("A"), pass("B"))}}
	if _, err := NewLoop(rev, jr, 3).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, &identityPasses{}); err == nil {
		t.Errorf("a reviser error must propagate, not be swallowed")
	}
}

func TestRun_JudgeError_Propagates(t *testing.T) {
	jr := &seqReviewer{err: errors.New("judge down")}
	if _, err := NewLoop(&fakeReviser{}, jr, 3).Run(context.Background(), twoProse(), preDoc(), kb.Snapshot{}, nil, &identityPasses{}); err == nil {
		t.Errorf("a judge error must propagate")
	}
}

// proseOf returns the concatenated prose text of the section with the
// given heading (test helper).
func proseOf(doc ir.Document, heading string) string {
	for _, s := range doc.Sections {
		if s.Heading != heading {
			continue
		}
		for _, b := range s.Blocks {
			if pb, ok := b.(ir.ProseBlock); ok {
				return pb.Text
			}
		}
	}
	return ""
}
