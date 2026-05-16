package architecture

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// seqLLM returns canned responses in order and records every prompt
// it was given, so tests can assert per-section prompt composition.
type seqLLM struct {
	resp  []string
	i     int
	seen  []llm.Request
	err   error
	errAt int // 1-based call index that should fail; 0 = never
}

func (s *seqLLM) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	s.seen = append(s.seen, req)
	s.i++
	if s.err != nil && s.i == s.errAt {
		return llm.Response{}, s.err
	}
	if s.i-1 < len(s.resp) {
		return llm.Response{Text: s.resp[s.i-1]}, nil
	}
	return llm.Response{Text: ""}, nil
}

func vaultSnap() kb.Snapshot {
	return kb.Snapshot{
		Commit: "deadbeef",
		Areas: []kb.Area{{
			ID: "vault", Name: "Vault", Summary: "HA secrets manager",
			Entries: []kb.Entry{
				{ID: "f1", Type: "fact", Text: "5-node Raft cluster", Tags: []string{"ha", "raft"}, Zone: "established"},
				{ID: "d1", Type: "decision", Text: "Auto-unseal via Azure KV", Why: "no manual unseal", Tags: []string{"ha"}, Zone: "established"},
				{ID: "g1", Type: "gotcha", Text: "Day-2 only thing", Tags: []string{"ops"}, Zone: "active"},
			},
		}},
	}
}

func src(t *testing.T, raw string) docspec.Source {
	t.Helper()
	d, err := docspec.Parse([]byte(
		"topic: T\nparent:\n  page: P\n  kind: architecture\n  sections:\n    - title: S\n      sources: [\"" + raw + "\"]\n"))
	if err != nil {
		t.Fatalf("docspec.Parse(%q): %v", raw, err)
	}
	return d.Parent.Sections[0].Sources[0]
}

func TestResolveKB_AreaTagZoneFilter(t *testing.T) {
	snap := vaultSnap()

	a, all, ok := ResolveKB(snap, src(t, "kb:area=vault"))
	if !ok || a == nil || len(all) != 3 {
		t.Fatalf("no filter → whole area: ok=%v a=%v n=%d", ok, a, len(all))
	}

	_, tagged, _ := ResolveKB(snap, src(t, "kb:area=vault tag=raft"))
	if len(tagged) != 1 || tagged[0].ID != "f1" {
		t.Errorf("tag=raft → only f1, got %+v", tagged)
	}

	_, zoned, _ := ResolveKB(snap, src(t, "kb:area=vault zone=active"))
	if len(zoned) != 1 || zoned[0].ID != "g1" {
		t.Errorf("zone=active → only g1, got %+v", zoned)
	}

	if _, _, ok := ResolveKB(snap, src(t, "kb:area=missing")); ok {
		t.Error("missing area must report ok=false")
	}
	if _, _, ok := ResolveKB(snap, src(t, "git:repo/x")); ok {
		t.Error("non-kb scheme must report ok=false")
	}
}

func TestRender_GroundsPromptAndFoldsUnderSectionTitles(t *testing.T) {
	llmC := &seqLLM{resp: []string{
		"Vault runs as a 5-node cluster.\n\n```mermaid\ngraph TD\nA-->B\n```\n",
		"Operational notes here.",
	}}
	page := docspec.DocPage{
		Page:     "OptiscanGroup/Azure_Infrastructure/Vault_Architecture",
		Kind:     "architecture",
		Audience: "human-operator",
		Intent:   "A human understands Vault.",
		Sections: []docspec.DocSection{
			{Title: "System Architecture", Intent: "Topology + unseal.", Sources: []docspec.Source{src(t, "kb:area=vault tag=ha,raft")}},
			{Title: "Source Code", Render: "table", Sources: []docspec.Source{src(t, "git:infra/vault")}},
			{Title: "Runbooks", Render: "child-index"},
			{Title: "Operations", Intent: "Day-2.", Sources: []docspec.Source{src(t, "kb:area=vault zone=active")}},
		},
	}

	doc, err := New(llmC, "m").Render(context.Background(), page, vaultSnap())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// render:table / render:child-index are deterministic — no LLM
	// call. Only the two prose sections hit the LLM.
	if len(llmC.seen) != 2 {
		t.Fatalf("expected 2 LLM calls (table/child-index are LLM-free), got %d", len(llmC.seen))
	}

	// prompt 1 grounded in tag-filtered kb + carries section intent +
	// page intent + audience persona.
	p0 := llmC.seen[0].Prompt
	if !strings.Contains(p0, "5-node Raft cluster") || strings.Contains(p0, "Day-2 only thing") {
		t.Errorf("section 1 prompt not tag-scoped to ha,raft:\n%s", p0)
	}
	if !strings.Contains(p0, "Topology + unseal.") || !strings.Contains(p0, "A human understands Vault.") {
		t.Errorf("section/page intent missing from prompt:\n%s", p0)
	}
	if !strings.Contains(llmC.seen[0].System, "operating this system") {
		t.Errorf("human-operator persona not applied: %q", llmC.seen[0].System)
	}

	// Frontmatter + section folding.
	if doc.Frontmatter.Title != page.Page || doc.Frontmatter.KBCommit != "deadbeef" {
		t.Errorf("frontmatter wrong: %+v", doc.Frontmatter)
	}
	// All 4 declared sections appear, in declared order.
	if len(doc.Sections) != 4 {
		t.Fatalf("want 4 rendered sections, got %d: %+v", len(doc.Sections), doc.Sections)
	}
	gotHeads := []string{
		doc.Sections[0].Heading, doc.Sections[1].Heading,
		doc.Sections[2].Heading, doc.Sections[3].Heading,
	}
	wantHeads := []string{"System Architecture", "Source Code", "Runbooks", "Operations"}
	for i := range wantHeads {
		if gotHeads[i] != wantHeads[i] {
			t.Errorf("section %d heading = %q, want %q", i, gotHeads[i], wantHeads[i])
		}
	}
	// render:table → TableBlock with the non-kb (git) source declared
	// as a pending row, never fabricated.
	tbl, ok := doc.Sections[1].Blocks[0].(ir.TableBlock)
	if !ok || len(tbl.Rows) != 1 || tbl.Rows[0][0] != "git" ||
		!strings.Contains(tbl.Rows[0][2], "pending") {
		t.Errorf("render:table section wrong: %+v", doc.Sections[1].Blocks)
	}
	// render:child-index → empty placeholder for the cluster to fill.
	idx, ok := doc.Sections[2].Blocks[0].(ir.IndexBlock)
	if !ok || len(idx.Entries) != 0 || idx.Prov.SpecSection != "architecture-child-index" {
		t.Errorf("render:child-index must be an empty cluster placeholder: %+v", doc.Sections[2].Blocks)
	}
	// LLM body became the declared section's blocks; mermaid → DiagramBlock.
	var hasProse, hasDiagram bool
	for _, b := range doc.Sections[0].Blocks {
		switch bl := b.(type) {
		case ir.ProseBlock:
			hasProse = strings.Contains(bl.Text, "5-node cluster")
		case ir.DiagramBlock:
			hasDiagram = bl.Lang == "mermaid" && !strings.Contains(bl.Source, "`")
		}
	}
	if !hasProse || !hasDiagram {
		t.Errorf("section 1 blocks wrong: %+v", doc.Sections[0].Blocks)
	}
}

func TestRender_EmptyLLMSectionGetsVisibleGapMarker(t *testing.T) {
	llmC := &seqLLM{resp: []string{"   "}}
	page := docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{
			{Title: "Lonely", Intent: "x", Sources: []docspec.Source{src(t, "kb:area=vault")}},
		},
	}
	doc, err := New(llmC, "m").Render(context.Background(), page, vaultSnap())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(doc.Sections) != 1 || doc.Sections[0].Heading != "Lonely" || len(doc.Sections[0].Blocks) != 1 {
		t.Fatalf("expected one section with a gap block: %+v", doc.Sections)
	}
	pb, ok := doc.Sections[0].Blocks[0].(ir.ProseBlock)
	if !ok || !strings.Contains(pb.Text, "No content") || pb.Prov.SpecSection != "architecture-gap" {
		t.Errorf("empty section must yield a visible gap marker, got %+v", doc.Sections[0].Blocks[0])
	}
}

func TestRender_NonKBSourceDeclaredButNotFabricated(t *testing.T) {
	llmC := &seqLLM{resp: []string{"body"}}
	page := docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{
			{Title: "S", Sources: []docspec.Source{src(t, "ssh:host=vault-1 cmd=vault status")}},
		},
	}
	if _, err := New(llmC, "m").Render(context.Background(), page, vaultSnap()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	p := llmC.seen[0].Prompt
	if !strings.Contains(p, "not yet machine-resolvable") || !strings.Contains(p, "ssh:host=vault-1 cmd=vault status") {
		t.Errorf("non-kb source must be declared as unresolved, not silently dropped:\n%s", p)
	}
}

func TestRender_LLMErrorIsFatal(t *testing.T) {
	llmC := &seqLLM{resp: []string{"ok"}, err: errors.New("boom"), errAt: 1}
	page := docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{{Title: "S", Sources: []docspec.Source{src(t, "kb:area=vault")}}},
	}
	if _, err := New(llmC, "m").Render(context.Background(), page, vaultSnap()); err == nil {
		t.Fatal("LLM error must fail the render — never push a half-empty page")
	}
}

func TestPersona_AudienceLever(t *testing.T) {
	if !strings.Contains(persona("newcomer"), "ZERO prior knowledge") {
		t.Error("newcomer persona missing")
	}
	if !strings.Contains(persona("llm-reference"), "terse") {
		t.Error("llm-reference persona missing")
	}
	if !strings.Contains(persona(""), "operating this system") {
		t.Error("default persona should be human-operator")
	}
}
