package editorial

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

var _ frontends.Frontend = (*Frontend)(nil)

// stubLLM returns a canned response and records the prompt it received.
type stubLLM struct {
	lastReq llm.Request
	resp    string
	err     error
}

func (s *stubLLM) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	s.lastReq = req
	if s.err != nil {
		return llm.Response{}, s.err
	}
	return llm.Response{Text: s.resp, TokensIn: 100, TokensOut: 50}, nil
}

func TestNameAndKind(t *testing.T) {
	f := New(&stubLLM{}, "claude-test")
	if f.Kind() != "editorial" {
		t.Errorf("Kind = %q, want %q", f.Kind(), "editorial")
	}
	if f.Name() == "" {
		t.Errorf("Name is empty")
	}
}

func TestBuild_PromptContainsSpecBody(t *testing.T) {
	llmC := &stubLLM{resp: "## Section\n\nbody\n"}
	spec := specs.Spec{
		Wiki: "acme", Page: "P", Kind: "editorial",
		Body:    "INTENT_BODY_MARKER_123",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{{ID: "vault", Name: "Vault"}}}
	_, err := New(llmC, "m").Build(context.Background(), spec, snap)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(llmC.lastReq.Prompt, "INTENT_BODY_MARKER_123") {
		t.Errorf("prompt missing spec body:\n%s", llmC.lastReq.Prompt)
	}
}

func TestBuild_PromptIncludesKBEntriesFromDeclaredAreas(t *testing.T) {
	llmC := &stubLLM{resp: "## S\n\n.\n"}
	spec := specs.Spec{
		Wiki: "acme", Page: "P", Kind: "editorial",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault", Entries: []kb.Entry{
			{ID: "f1", Type: "fact", Text: "UNIQUE_VAULT_FACT_TOKEN"},
		},
	}}}
	_, _ = New(llmC, "m").Build(context.Background(), spec, snap)
	if !strings.Contains(llmC.lastReq.Prompt, "UNIQUE_VAULT_FACT_TOKEN") {
		t.Errorf("prompt missing kb content:\n%s", llmC.lastReq.Prompt)
	}
}

func TestBuild_PromptExcludesAreasNotInInclude(t *testing.T) {
	llmC := &stubLLM{resp: "## S\n\n.\n"}
	spec := specs.Spec{
		Wiki: "acme", Page: "P", Kind: "editorial",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{
		{ID: "vault", Entries: []kb.Entry{{ID: "v", Type: "fact", Text: "vault-only"}}},
		{ID: "harbor", Entries: []kb.Entry{{ID: "h", Type: "fact", Text: "HARBOR_TOKEN_DO_NOT_LEAK"}}},
	}}
	_, _ = New(llmC, "m").Build(context.Background(), spec, snap)
	if strings.Contains(llmC.lastReq.Prompt, "HARBOR_TOKEN_DO_NOT_LEAK") {
		t.Errorf("harbor content leaked into vault-only spec prompt:\n%s", llmC.lastReq.Prompt)
	}
}

func TestBuild_ParsesResponseMarkdownIntoSections(t *testing.T) {
	llmC := &stubLLM{resp: `## Overview

Vault is the secrets manager.

## Topology

Three-node HA cluster.
`}
	spec := specs.Spec{Wiki: "acme", Page: "Vault", Kind: "editorial", Hash: "h"}
	doc, err := New(llmC, "m").Build(context.Background(), spec, kb.Snapshot{Commit: "kb-h"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.Frontmatter.Title != "Vault" {
		t.Errorf("Title = %q, want %q", doc.Frontmatter.Title, "Vault")
	}
	if doc.Frontmatter.SpecHash != "h" || doc.Frontmatter.KBCommit != "kb-h" {
		t.Errorf("frontmatter provenance not propagated: %+v", doc.Frontmatter)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("len(Sections) = %d, want 2", len(doc.Sections))
	}
	if doc.Sections[0].Heading != "Overview" || doc.Sections[1].Heading != "Topology" {
		t.Errorf("headings = %q,%q", doc.Sections[0].Heading, doc.Sections[1].Heading)
	}
	if p, ok := doc.Sections[0].Blocks[0].(ir.ProseBlock); !ok {
		t.Errorf("Sections[0].Blocks[0] is not ProseBlock: %T", doc.Sections[0].Blocks[0])
	} else if !strings.Contains(p.Text, "secrets manager") {
		t.Errorf("prose content lost: %q", p.Text)
	}
}

// LLMs do not perfectly obey "use ## only" — gemini emitted ### sub
// headings live, which parseMarkdown used to dump verbatim into a
// ProseBlock, leaking raw "### Vault" markdown into the wiki. Any
// ATX heading level (## … ######) must start a section so no
// heading markup ever survives into prose.
func TestBuild_DeeperHeadingsBecomeSections(t *testing.T) {
	llmC := &stubLLM{resp: "## Stacks\n\n### Vault\n\nVault prose.\n\n### Harbor\n\nHarbor prose.\n"}
	doc, err := New(llmC, "m").Build(context.Background(),
		specs.Spec{Wiki: "acme", Page: "P", Kind: "editorial", Hash: "h"}, kb.Snapshot{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var headings []string
	for _, s := range doc.Sections {
		headings = append(headings, s.Heading)
		for _, b := range s.Blocks {
			if p, ok := b.(ir.ProseBlock); ok && strings.Contains(p.Text, "#") {
				t.Errorf("heading markup leaked into prose: %q", p.Text)
			}
		}
	}
	want := []string{"Stacks", "Vault", "Harbor"}
	if !reflect.DeepEqual(headings, want) {
		t.Errorf("headings = %v, want %v", headings, want)
	}
}

func TestBuild_LLMError_PropagatesAsBuildError(t *testing.T) {
	llmC := &stubLLM{err: errMessage("simulated upstream failure")}
	_, err := New(llmC, "m").Build(context.Background(), specs.Spec{Wiki: "a", Page: "p", Kind: "editorial"}, kb.Snapshot{})
	if err == nil {
		t.Errorf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "simulated upstream failure") {
		t.Errorf("err = %v, want to include LLM error", err)
	}
}

func TestBuild_EmptyLLMResponse_FailsLoud(t *testing.T) {
	// LLM returned nothing meaningful — better to fail the spec than
	// to push an empty page to the wiki.
	llmC := &stubLLM{resp: "   \n\n   "}
	_, err := New(llmC, "m").Build(context.Background(), specs.Spec{Wiki: "a", Page: "p", Kind: "editorial"}, kb.Snapshot{})
	if err == nil {
		t.Errorf("expected error on empty response, got nil")
	}
}

func TestBuild_NoLeadingHeading_SingleSection(t *testing.T) {
	// LLM produced prose without a heading. Wrap in a single
	// (heading-less) section rather than dropping the content.
	llmC := &stubLLM{resp: "just some prose without sections"}
	doc, err := New(llmC, "m").Build(context.Background(), specs.Spec{Wiki: "a", Page: "p", Kind: "editorial"}, kb.Snapshot{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Fatalf("len = %d, want 1", len(doc.Sections))
	}
	if doc.Sections[0].Heading != "" {
		t.Errorf("Heading = %q, want empty for un-headed prose", doc.Sections[0].Heading)
	}
}

// errMessage is a quick error-from-string helper for the test.
type errMessage string

func (e errMessage) Error() string { return string(e) }
