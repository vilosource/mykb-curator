package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/docspecs"
	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/cluster"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/refine"
	"github.com/vilosource/mykb-curator/internal/reporter"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

type fakeDocSpecs struct {
	files []docspecs.File
	err   error
}

func (f fakeDocSpecs) Pull(context.Context) ([]docspecs.File, error) { return f.files, f.err }
func (fakeDocSpecs) Whoami() string                                  { return "fakeDocSpecs" }

// fakeRenderer is a cluster.Renderer: one prose block per page,
// plus a child-index placeholder on the parent so the cluster's
// in-place fill is exercised end-to-end.
type fakeRenderer struct{}

func (fakeRenderer) Render(_ context.Context, p docspec.DocPage, _ kb.Snapshot) (ir.Document, error) {
	secs := []ir.Section{{Heading: "Overview", Blocks: []ir.Block{ir.ProseBlock{Text: "body of " + p.Page}}}}
	if p.Kind == "architecture" {
		secs = append(secs, ir.Section{
			Heading: "Runbooks",
			Blocks: []ir.Block{ir.IndexBlock{
				Prov: ir.Provenance{SpecSection: "architecture-child-index"},
			}},
		})
	}
	return ir.Document{Frontmatter: ir.Frontmatter{Title: p.Page}, Sections: secs}, nil
}

// judgeLLM fails the "Overview" section so the report-only warning
// path is exercised without blocking the push.
type judgeLLM struct{}

func (judgeLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{Text: `{"pass": false, "reason": "ungrounded version", "ungrounded_claims": ["X 1.2"]}`}, nil
}

func vaultCluster() docspec.DocSpec {
	return docspec.DocSpec{
		Topic: "Vault",
		Parent: docspec.DocPage{
			Page: "Vault Architecture", Kind: "architecture",
			Intent: "Understand Vault.",
			Sections: []docspec.DocSection{
				{Title: "Overview", Intent: "Explain what Vault is."},
				{Title: "Runbooks", Render: "child-index"},
			},
		},
		Children: []docspec.DocPage{
			{Page: "Vault Operations", Kind: "runbook", Intent: "Day-2."},
		},
	}
}

func TestRun_DocSpecCluster_RendersPushesAndJudges(t *testing.T) {
	captured := map[string][]byte{}
	o := New(Deps{
		Wiki:       "acme",
		KB:         fakeKB{commit: "abc"},
		Specs:      fakeSpecs{}, // no legacy specs
		WikiTarget: fakeWiki{},
		Backend:    fakeBackend{},
		DocSpecs:   fakeDocSpecs{files: []docspecs.File{{ID: "vault.doc.yaml", Spec: vaultCluster()}}},
		Cluster:    cluster.New(fakeRenderer{}),
		Judge:      judge.New(judgeLLM{}, "m"),
		OnRendered: func(id string, b []byte, _ ir.Document) error { captured[id] = b; return nil },
	})

	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// parent + one child = 2 page results, both rendered+pushed.
	var ids []string
	for _, s := range rep.Specs {
		ids = append(ids, s.ID)
		if s.Status != reporter.StatusRendered {
			t.Errorf("%s status = %q, want rendered", s.ID, s.Status)
		}
	}
	if len(rep.Specs) != 2 {
		t.Fatalf("want 2 cluster page results, got %d (%v)", len(rep.Specs), ids)
	}
	if !contains(ids, "vault.doc.yaml::Vault Architecture") ||
		!contains(ids, "vault.doc.yaml::Vault Operations") {
		t.Errorf("synthetic per-page spec IDs wrong: %v", ids)
	}

	// child-index was filled by the cluster before backend render.
	parent := captured["vault.doc.yaml::Vault Architecture"]
	if !strings.Contains(string(parent), "body of Vault Architecture") {
		t.Errorf("parent page content missing: %s", parent)
	}

	// Judge is report-only: a FAIL verdict surfaces as a warning,
	// never a failed/blocked page.
	joined := strings.Join(rep.Warnings, " | ")
	if !strings.Contains(joined, "judge") || !strings.Contains(joined, "FAIL") {
		t.Errorf("expected a report-only judge warning, got warnings: %v", rep.Warnings)
	}
}

// capturingJudgeLLM records every judge prompt so we can assert the
// resolved kb grounding actually reaches the Judge.
type capturingJudgeLLM struct{ prompts []string }

func (c *capturingJudgeLLM) Complete(_ context.Context, r llm.Request) (llm.Response, error) {
	c.prompts = append(c.prompts, r.Prompt)
	return llm.Response{Text: `{"pass": true, "reason": "ok", "ungrounded_claims": []}`}, nil
}

func TestRun_DocSpec_JudgeReceivesResolvedKBGrounding(t *testing.T) {
	spec, err := docspec.Parse([]byte(
		"topic: Vault\n" +
			"parent:\n" +
			"  page: Vault Architecture\n" +
			"  kind: architecture\n" +
			"  intent: Understand Vault.\n" +
			"  sections:\n" +
			"    - title: Overview\n" +
			"      intent: Topology and unseal.\n" +
			"      sources: [\"kb:area=vault\"]\n"))
	if err != nil {
		t.Fatalf("docspec.Parse: %v", err)
	}
	cj := &capturingJudgeLLM{}
	o := New(Deps{
		Wiki: "acme",
		KB: fakeKB{commit: "abc", areas: []kb.Area{{
			ID: "vault", Name: "Vault", Summary: "HA secrets manager",
			Entries: []kb.Entry{{ID: "f1", Type: "fact", Text: "3 replicas + 1 backup, image 1.19.5"}},
		}}},
		Specs:      fakeSpecs{},
		WikiTarget: fakeWiki{},
		Backend:    fakeBackend{},
		DocSpecs:   fakeDocSpecs{files: []docspecs.File{{ID: "vault.doc.yaml", Spec: spec}}},
		Cluster:    cluster.New(fakeRenderer{}),
		Judge:      judge.New(cj, "m"),
	})

	if _, err := o.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cj.prompts) == 0 {
		t.Fatal("judge was never called")
	}
	joined := strings.Join(cj.prompts, "\n====\n")
	// The resolved kb entry — not just the "kb:area=vault" identifier
	// — must be in the judge prompt so it verifies, not guesses.
	if !strings.Contains(joined, "3 replicas + 1 backup, image 1.19.5") ||
		!strings.Contains(joined, "### Area: vault") {
		t.Errorf("resolved kb grounding did not reach the Judge:\n%s", joined)
	}
}

func TestRun_DocSpec_Unwired_IsNoOp(t *testing.T) {
	o := New(Deps{
		Wiki: "acme", KB: fakeKB{commit: "abc"}, Specs: fakeSpecs{},
		WikiTarget: fakeWiki{},
	})
	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Specs) != 0 {
		t.Errorf("absent DocSpecs/Cluster must be a silent no-op, got %d results", len(rep.Specs))
	}
}

func TestRun_DocSpec_ClusterRenderError_FailsThatTopic(t *testing.T) {
	o := New(Deps{
		Wiki: "acme", KB: fakeKB{commit: "abc"}, Specs: fakeSpecs{},
		WikiTarget: fakeWiki{}, Backend: fakeBackend{},
		DocSpecs: fakeDocSpecs{files: []docspecs.File{{ID: "broken.doc.yaml", Spec: vaultCluster()}}},
		Cluster:  cluster.New(errRenderer{}),
	})
	rep, _ := o.Run(context.Background())
	if len(rep.Specs) != 1 || rep.Specs[0].Status != reporter.StatusFailed {
		t.Fatalf("a cluster render error must fail that topic: %+v", rep.Specs)
	}
}

type errRenderer struct{}

func (errRenderer) Render(context.Context, docspec.DocPage, kb.Snapshot) (ir.Document, error) {
	return ir.Document{}, context.DeadlineExceeded
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

var _ docspecs.Store = fakeDocSpecs{}
var _ specs.Store = fakeSpecs{}

// fakeReviser re-synthesizes a section into fresh "revised" prose,
// recording each title it revised.
type fakeReviser struct{ calls []string }

func (r *fakeReviser) ReviseSection(_ context.Context, _ docspec.DocPage, sec docspec.DocSection, _ kb.Snapshot, _ architecture.SectionFeedback) (ir.Section, error) {
	r.calls = append(r.calls, sec.Title)
	return ir.Section{Heading: sec.Title, Blocks: []ir.Block{ir.ProseBlock{Text: "revised " + sec.Title}}}, nil
}

// flipJudgeLLM fails the first review and passes every review after,
// so the closed loop converges in exactly one refine round.
type flipJudgeLLM struct{ calls int }

func (j *flipJudgeLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	j.calls++
	if j.calls == 1 {
		return llm.Response{Text: `{"pass": false, "reason": "ungrounded version", "ungrounded_claims": ["X 1.2"]}`}, nil
	}
	return llm.Response{Text: `{"pass": true, "reason": "ok", "ungrounded_claims": []}`}, nil
}

func TestRun_DocSpec_RefinerConverges_RecordsIterationsAndClearsVerdict(t *testing.T) {
	rev := &fakeReviser{}
	o := New(Deps{
		Wiki:       "acme",
		KB:         fakeKB{commit: "abc"},
		Specs:      fakeSpecs{},
		WikiTarget: fakeWiki{},
		Backend:    fakeBackend{},
		DocSpecs:   fakeDocSpecs{files: []docspecs.File{{ID: "vault.doc.yaml", Spec: vaultCluster()}}},
		Cluster:    cluster.New(fakeRenderer{}),
		Refiner:    refine.NewLoop(rev, judge.New(&flipJudgeLLM{}, "m"), 3),
	})

	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	parent := specResult(rep, "vault.doc.yaml::Vault Architecture")
	if parent == nil {
		t.Fatalf("no result for parent page")
	}
	if parent.Status != reporter.StatusRendered {
		t.Errorf("parent status = %q, want rendered (non-blocking)", parent.Status)
	}
	if parent.JudgeIterations != 1 {
		t.Errorf("parent JudgeIterations = %d, want 1 (failed once, passed after a refine)", parent.JudgeIterations)
	}
	if parent.JudgeVerdict != "" {
		t.Errorf("parent JudgeVerdict = %q, want empty (final verdict passed)", parent.JudgeVerdict)
	}
	if len(rev.calls) != 1 || rev.calls[0] != "Overview" {
		t.Errorf("reviser calls = %v, want exactly [Overview] (only the failing section)", rev.calls)
	}
}

func TestRun_DocSpec_RefinerNonBlocking_PublishesBestEffortWithVerdict(t *testing.T) {
	rev := &fakeReviser{}
	o := New(Deps{
		Wiki:       "acme",
		KB:         fakeKB{commit: "abc"},
		Specs:      fakeSpecs{},
		WikiTarget: fakeWiki{},
		Backend:    fakeBackend{},
		DocSpecs:   fakeDocSpecs{files: []docspecs.File{{ID: "vault.doc.yaml", Spec: vaultCluster()}}},
		Cluster:    cluster.New(fakeRenderer{}),
		Refiner:    refine.NewLoop(rev, judge.New(judgeLLM{}, "m"), 3), // judgeLLM always fails
	})

	rep, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	parent := specResult(rep, "vault.doc.yaml::Vault Architecture")
	if parent == nil {
		t.Fatalf("no result for parent page")
	}
	// Never blocks: still rendered+pushed despite a persistent failure.
	if parent.Status != reporter.StatusRendered {
		t.Errorf("parent status = %q, want rendered (best-effort publish)", parent.Status)
	}
	// A persistent single-section failure triggers the no-progress stop
	// after one refine round; the final verdict is recorded.
	if parent.JudgeIterations != 1 {
		t.Errorf("parent JudgeIterations = %d, want 1 (no-progress stop)", parent.JudgeIterations)
	}
	if !strings.Contains(parent.JudgeVerdict, "Overview") {
		t.Errorf("parent JudgeVerdict should name the still-failing section, got %q", parent.JudgeVerdict)
	}
	joined := strings.Join(rep.Warnings, " | ")
	if !strings.Contains(joined, "best-effort") {
		t.Errorf("expected a best-effort warning, got: %v", rep.Warnings)
	}
}

func specResult(rep reporter.Report, id string) *reporter.SpecResult {
	for i := range rep.Specs {
		if rep.Specs[i].ID == id {
			return &rep.Specs[i]
		}
	}
	return nil
}
