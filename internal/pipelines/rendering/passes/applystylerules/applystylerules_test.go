package applystylerules_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/applystylerules"
)

func doc(secs ...ir.Section) ir.Document { return ir.Document{Sections: secs} }

func proseOf(t *testing.T, d ir.Document, si, bi int) string {
	t.Helper()
	pb, ok := d.Sections[si].Blocks[bi].(ir.ProseBlock)
	if !ok {
		t.Fatalf("section %d block %d is %T, want ProseBlock", si, bi, d.Sections[si].Blocks[bi])
	}
	return pb.Text
}

func TestName(t *testing.T) {
	if got := applystylerules.New().Name(); got != "apply-style-rules" {
		t.Errorf("Name() = %q, want apply-style-rules", got)
	}
}

func TestNoRules_Identity(t *testing.T) {
	in := doc(ir.Section{Heading: "Intro", Blocks: []ir.Block{ir.ProseBlock{Text: "we use k8s"}}})
	out, err := applystylerules.New().Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if proseOf(t, out, 0, 0) != "we use k8s" {
		t.Errorf("no-rules pass mutated content: %q", proseOf(t, out, 0, 0))
	}
}

func TestTerminologyRule_ProseHeadingCallout(t *testing.T) {
	rule := applystylerules.NewTerminologyRule(map[string]string{
		"k8s":    "Kubernetes",
		"github": "GitHub",
	})
	p := applystylerules.New(rule)

	in := doc(ir.Section{
		Heading: "k8s on github",
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "Deploy to k8s from github."},
			ir.Callout{Severity: "note", Body: "k8s only"},
			ir.MachineBlock{BlockID: "b1", Body: "k8s-raw-id"}, // machine: must NOT be rewritten
		},
	})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := out.Sections[0].Heading; got != "Kubernetes on GitHub" {
		t.Errorf("heading = %q, want %q", got, "Kubernetes on GitHub")
	}
	if got := proseOf(t, out, 0, 0); got != "Deploy to Kubernetes from GitHub." {
		t.Errorf("prose = %q", got)
	}
	if cb := out.Sections[0].Blocks[1].(ir.Callout); cb.Body != "Kubernetes only" {
		t.Errorf("callout = %q, want %q", cb.Body, "Kubernetes only")
	}
	if mb := out.Sections[0].Blocks[2].(ir.MachineBlock); mb.Body != "k8s-raw-id" {
		t.Errorf("machine block was rewritten (%q); structural content must be left alone", mb.Body)
	}
}

func TestTerminologyRule_WholeWordOnly(t *testing.T) {
	rule := applystylerules.NewTerminologyRule(map[string]string{"go": "Go"})
	p := applystylerules.New(rule)
	in := doc(ir.Section{Blocks: []ir.Block{ir.ProseBlock{Text: "go is good but goroutine and ego stay"}}})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := proseOf(t, out, 0, 0); got != "Go is good but goroutine and ego stay" {
		t.Errorf("whole-word boundary violated: %q", got)
	}
}

func TestTerminologyRule_Deterministic(t *testing.T) {
	// Map iteration order must not affect output: chained-ish entries
	// applied in a stable (sorted-key) order, run twice → identical.
	repl := map[string]string{"a": "X", "b": "Y", "c": "Z", "d": "W"}
	p := applystylerules.New(applystylerules.NewTerminologyRule(repl))
	mk := func() string {
		out, err := p.Apply(context.Background(), doc(ir.Section{Blocks: []ir.Block{ir.ProseBlock{Text: "a b c d a b c d"}}}))
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		return proseOf(t, out, 0, 0)
	}
	first, second := mk(), mk()
	if first != second {
		t.Errorf("terminology rule not deterministic across runs: %q vs %q", first, second)
	}
}

func TestHeadingCaseRule_Sentence(t *testing.T) {
	rule, err := applystylerules.NewHeadingCaseRule("sentence")
	if err != nil {
		t.Fatalf("NewHeadingCaseRule: %v", err)
	}
	p := applystylerules.New(rule)
	in := doc(ir.Section{Heading: "Core Infrastructure Setup"})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := out.Sections[0].Heading; got != "Core infrastructure setup" {
		t.Errorf("heading = %q, want sentence-case %q", got, "Core infrastructure setup")
	}
}

func TestHeadingCaseRule_Title(t *testing.T) {
	rule, err := applystylerules.NewHeadingCaseRule("title")
	if err != nil {
		t.Fatalf("NewHeadingCaseRule: %v", err)
	}
	p := applystylerules.New(rule)
	in := doc(ir.Section{Heading: "core infrastructure setup"})
	out, err := p.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := out.Sections[0].Heading; got != "Core Infrastructure Setup" {
		t.Errorf("heading = %q, want title-case %q", got, "Core Infrastructure Setup")
	}
}

func TestNewHeadingCaseRule_RejectsUnknownMode(t *testing.T) {
	if _, err := applystylerules.NewHeadingCaseRule("SHOUTING"); err == nil {
		t.Errorf("expected error for unknown heading-case mode")
	}
}

func TestRulesAppliedInOrder(t *testing.T) {
	// terminology first (k8s→Kubernetes), then sentence-case the
	// heading. Order is the slice order passed to New.
	term := applystylerules.NewTerminologyRule(map[string]string{"K8S": "Kubernetes"})
	hcase, err := applystylerules.NewHeadingCaseRule("sentence")
	if err != nil {
		t.Fatalf("NewHeadingCaseRule: %v", err)
	}
	p := applystylerules.New(term, hcase)
	out, err := p.Apply(context.Background(), doc(ir.Section{Heading: "K8S Cluster Notes"}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := out.Sections[0].Heading; got != "Kubernetes cluster notes" {
		t.Errorf("ordered rules = %q, want %q", got, "Kubernetes cluster notes")
	}
}
