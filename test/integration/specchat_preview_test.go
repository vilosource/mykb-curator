//go:build integration

// Package integration_test, pyramid level 2: the real specchat
// Previewer wiring (docspec.Parse -> cluster.Render -> markdown ->
// judge.Review -> aggregate) exercised with deterministic stubs for
// the two LLM-bearing edges, against the real hand-authored
// vault.doc.yaml. No network.
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/cluster"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specchat"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// scRenderer emits one prose block per declared section so the
// Judge has bodies to review — deterministic, no LLM.
type scRenderer struct{}

func (scRenderer) Render(_ context.Context, p docspec.DocPage, _ kbpkg.Snapshot) (ir.Document, error) {
	doc := ir.Document{Frontmatter: ir.Frontmatter{Title: p.Page}}
	for _, sec := range p.Sections {
		doc.Sections = append(doc.Sections, ir.Section{
			Heading: sec.Title,
			Blocks:  []ir.Block{ir.ProseBlock{Text: "Rendered body for " + sec.Title + "."}},
		})
	}
	return doc, nil
}

// scLLM returns a fixed pass verdict for every Judge call.
type scLLM struct{}

func (scLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{Text: `{"pass": true, "reason": "grounded", "ungrounded_claims": []}`}, nil
}

type scKB struct{}

func (scKB) Pull(context.Context) (kbpkg.Snapshot, error) {
	return kbpkg.Snapshot{Commit: "test", Areas: []kbpkg.Area{
		{ID: "vault", Name: "Vault"}, {ID: "disaster-recovery", Name: "DR"},
		{ID: "docker-swarm", Name: "Swarm"}, {ID: "backup", Name: "Backup"},
		{ID: "iac", Name: "IaC"},
	}}, nil
}

func scGolden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "specs", "docspec", "docedit", "testdata", "vault.doc.yaml"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return b
}

func TestPreviewer_RealWiring(t *testing.T) {
	p := specchat.NewPreviewer(scKB{}, cluster.New(scRenderer{}), judge.New(scLLM{}, "m"))

	res, err := p.Preview(context.Background(), scGolden(t))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(res.Pages) == 0 {
		t.Fatal("no rendered pages")
	}
	// Parent page first, markdown rendered with its title heading.
	if res.Pages[0].Page != "Vault Architecture" ||
		!strings.Contains(res.Pages[0].Markdown, "# Vault Architecture") {
		t.Fatalf("parent markdown wrong: %q", res.Pages[0].Markdown)
	}
	// Every intent-bearing prose section judged; all pass here.
	if !res.AllPass || len(res.Verdicts) == 0 {
		t.Fatalf("expected all-pass with verdicts, got AllPass=%v n=%d",
			res.AllPass, len(res.Verdicts))
	}
	for _, v := range res.Verdicts {
		if !v.Pass {
			t.Fatalf("unexpected failing verdict: %+v", v)
		}
	}
}

func TestPreviewer_BadCandidateErrors(t *testing.T) {
	p := specchat.NewPreviewer(scKB{}, cluster.New(scRenderer{}), judge.New(scLLM{}, "m"))
	if _, err := p.Preview(context.Background(), []byte("topic: \"unterminated")); err == nil {
		t.Fatal("want parse error for malformed candidate")
	}
}
