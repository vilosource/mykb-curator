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
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
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
