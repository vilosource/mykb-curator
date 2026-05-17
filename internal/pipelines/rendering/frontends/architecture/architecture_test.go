package architecture

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/sources"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// fakeResolver is a deterministic non-kb source resolver for tests.
type fakeResolver struct {
	scheme string
	res    sources.Resolved
	ok     bool
	err    error
}

func (f *fakeResolver) Scheme() string { return f.scheme }
func (f *fakeResolver) Resolve(_ context.Context, _ docspec.Source) (sources.Resolved, bool, error) {
	return f.res, f.ok, f.err
}

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

func TestRender_GitResolverGroundsProseAndTable(t *testing.T) {
	gitR := &fakeResolver{
		scheme: "git",
		ok:     true,
		res: sources.Resolved{
			Digest: "### git: infra/vault @ abc123\nVAULT_RAFT_NODES=5\n",
			Rows:   [][]string{{"file", "git:infra/vault@abc123:compose.yml", "vault:1.15"}},
			Refs:   []string{"git:infra/vault@abc123"},
		},
	}
	llmC := &seqLLM{resp: []string{"Synthesised from the repo."}}
	page := docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{
			{Title: "Source Code", Render: "table", Sources: []docspec.Source{src(t, "git:infra/vault")}},
			{Title: "Build", Intent: "How it is built.", Sources: []docspec.Source{src(t, "git:infra/vault")}},
		},
	}

	doc, err := New(llmC, "m", gitR).Render(context.Background(), page, vaultSnap())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// render:table → resolver rows, NOT a pending row.
	tbl := doc.Sections[0].Blocks[0].(ir.TableBlock)
	if len(tbl.Rows) != 1 || tbl.Rows[0][1] != "git:infra/vault@abc123:compose.yml" {
		t.Errorf("git rows must replace the pending row: %+v", tbl.Rows)
	}
	// prose section → resolver digest is in the prompt, and the
	// source is NOT declared as unresolved.
	p := llmC.seen[0].Prompt
	if !strings.Contains(p, "VAULT_RAFT_NODES=5") {
		t.Errorf("git digest must ground the prose prompt:\n%s", p)
	}
	if strings.Contains(p, "not yet machine-resolvable") {
		t.Errorf("a resolved git source must not be declared pending:\n%s", p)
	}
}

func TestRender_ResolverErrorIsFatal(t *testing.T) {
	gitR := &fakeResolver{scheme: "git", err: errors.New("git boom")}
	llmC := &seqLLM{resp: []string{"x"}}
	page := docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{
			{Title: "Build", Intent: "i", Sources: []docspec.Source{src(t, "git:infra/vault")}},
		},
	}
	if _, err := New(llmC, "m", gitR).Render(context.Background(), page, vaultSnap()); err == nil {
		t.Fatal("a resolver hard error must abort the page, not silently degrade")
	}
}

func TestRender_NoResolverForScheme_StaysPending(t *testing.T) {
	// git resolver configured, but the source is ssh: → no resolver
	// for that scheme → honest pending, no fabrication, no error.
	gitR := &fakeResolver{scheme: "git", ok: true}
	llmC := &seqLLM{resp: []string{"body"}}
	page := docspec.DocPage{
		Page: "P", Kind: "architecture",
		Sections: []docspec.DocSection{
			{Title: "Probe", Render: "table", Sources: []docspec.Source{src(t, "ssh:host=vault-1")}},
		},
	}
	doc, err := New(llmC, "m", gitR).Render(context.Background(), page, vaultSnap())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	tbl := doc.Sections[0].Blocks[0].(ir.TableBlock)
	if len(tbl.Rows) != 1 || tbl.Rows[0][0] != "ssh" || !strings.Contains(tbl.Rows[0][2], "pending") {
		t.Errorf("scheme without a resolver must stay an honest pending row: %+v", tbl.Rows)
	}
}

func TestSectionGrounding_IsTheKBDigestSynthesisWasGiven(t *testing.T) {
	// The report-only Judge must verify claims against EXACTLY the
	// grounding synthesis received. SectionGrounding exposes that
	// kb digest; it must equal what composeSectionPrompt embeds, so
	// the Judge stops false-positiving kb-backed facts as ungrounded.
	snap := vaultSnap()
	sec := docspec.DocSection{
		Title: "System Architecture", Intent: "topology",
		Sources: []docspec.Source{src(t, "kb:area=vault tag=ha,raft")},
	}
	g := SectionGrounding(snap, sec)

	if !strings.Contains(g, "### Area: vault — Vault") {
		t.Errorf("grounding missing area header:\n%s", g)
	}
	if !strings.Contains(g, "[fact/f1] 5-node Raft cluster") {
		t.Errorf("grounding missing the resolved entry:\n%s", g)
	}
	if strings.Contains(g, "Day-2 only thing") {
		t.Errorf("grounding must honour the source's tag filter:\n%s", g)
	}
	// Single source of truth: it is byte-identical to the kb digest
	// the synthesis prompt is grounded in for the same section.
	prompt := composeSectionPrompt(docspec.DocPage{Page: "P"}, sec, g, nil)
	if !strings.Contains(prompt, strings.TrimSpace(g)) {
		t.Errorf("SectionGrounding must match the digest fed to synthesis:\ng=%q", g)
	}

	// Non-kb scheme contributes nothing here (kb is the grounding the
	// Judge needs); empty when no kb source resolves.
	none := SectionGrounding(snap, docspec.DocSection{
		Sources: []docspec.Source{src(t, "git:repo/x")},
	})
	if none != "" {
		t.Errorf("non-kb source must yield no kb grounding, got %q", none)
	}
}

func TestFoldSection_HeadingLedResponseStillFillsDeclaredSection(t *testing.T) {
	// The model led with a sub-heading and gave no lead prose (the
	// contract-hardened prompt nudges enumerated/structured output).
	// The spec-declared section must still OWN that content — a
	// section the model produced prose for must never be reported
	// empty, and the gap marker must NOT fire when real content exists.
	parsed := []ir.Section{
		{Heading: "Phase 1", Blocks: []ir.Block{ir.ProseBlock{Text: "validate the snapshot"}}},
		{Heading: "Phase 2", Blocks: []ir.Block{ir.ProseBlock{Text: "bootstrap the node"}}},
	}
	out := foldSection("7-Phase Restore Procedure", parsed, "h")

	if len(out) == 0 || out[0].Heading != "7-Phase Restore Procedure" {
		t.Fatalf("declared title must head the section: %+v", out)
	}
	if len(out[0].Blocks) == 0 {
		t.Fatalf("declared section must own the model's first chunk, got empty: %+v", out)
	}
	if pb, ok := out[0].Blocks[0].(ir.ProseBlock); !ok ||
		!strings.Contains(pb.Text, "validate the snapshot") {
		t.Fatalf("declared section body should be the model's content: %+v", out[0].Blocks)
	}
	if pb, ok := out[0].Blocks[0].(ir.ProseBlock); ok &&
		strings.Contains(pb.Text, "No content was available") {
		t.Fatalf("spurious gap marker despite real content: %+v", out)
	}
	// Subsequent chunks remain flattened siblings (design unchanged).
	if len(out) != 2 || out[1].Heading != "Phase 2" {
		t.Fatalf("later chunks must stay flattened siblings: %+v", out)
	}
}

func TestComposeSectionPrompt_EnforcesContractGroundingAndHonestGaps(t *testing.T) {
	page := docspec.DocPage{Page: "Vault Architecture", Intent: "Understand Vault."}
	sec := docspec.DocSection{Title: "System Architecture", Intent: "3-node Raft + auto-unseal; include a diagram."}
	p := composeSectionPrompt(page, sec, "### Area: vault\n- [fact/f1] 5-node Raft cluster\n", nil)

	// Page/section framing + the grounding digest must survive intact.
	for _, want := range []string{
		"Vault Architecture",        // page
		"Understand Vault.",         // page intent
		"System Architecture",       // section title
		"3-node Raft + auto-unseal", // section intent, verbatim
		"5-node Raft cluster",       // kb digest preserved
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	low := strings.ToLower(p)
	// 1. The intent is a must-cover-EVERY-item contract, not a soft "convey";
	//    preamble-only / generic-overview output is explicitly forbidden.
	if !strings.Contains(low, "every") ||
		(!strings.Contains(low, "checklist") && !strings.Contains(low, "contract")) {
		t.Errorf("prompt must frame the intent as a cover-every-item contract:\n%s", p)
	}
	if !strings.Contains(low, "preamble") {
		t.Errorf("prompt must forbid stopping at a preamble:\n%s", p)
	}
	// 2. Substance must be the supplied org-specifics, not generic filler.
	if !strings.Contains(low, "generic") && !strings.Contains(low, "textbook") {
		t.Errorf("prompt must forbid generic filler in place of org specifics:\n%s", p)
	}
	// 3. An unsupported required item is flagged explicitly — not omitted
	//    (silent intent FAIL) and not fabricated (ungrounded FAIL).
	if !strings.Contains(p, "_Not covered by current sources:") {
		t.Errorf("prompt must instruct an explicit per-item gap note:\n%s", p)
	}
}

func TestComposeSectionPrompt_ForcesPerItemCoverageLedger(t *testing.T) {
	// Run-4 residue: under rich grounding the model wrote fluent but
	// OFF-CONTRACT prose and treated the gap-note as optional. The
	// prompt must make the per-item reconciliation a hard closing
	// step and name that exact failure as unacceptable.
	page := docspec.DocPage{Page: "P", Intent: "i"}
	sec := docspec.DocSection{Title: "Deployment & Operations",
		Intent: "rolling updates, the vault-to-swarm-sync bridge, daily Raft snapshots to GRS"}
	p := strings.ToLower(composeSectionPrompt(page, sec, "### Area: vault\n- [fact/f1] bridge\n", nil))

	if !strings.Contains(p, "before finishing") && !strings.Contains(p, "before you finish") {
		t.Errorf("prompt must require an explicit end-of-write contract reconciliation:\n%s", p)
	}
	if !strings.Contains(p, "each item") {
		t.Errorf("prompt must force a per-item check:\n%s", p)
	}
	if !strings.Contains(p, "off-contract") && !strings.Contains(p, "off contract") &&
		!strings.Contains(p, "adjacent") && !strings.Contains(p, "related but") {
		t.Errorf("prompt must name off-contract substitution as the failure to avoid:\n%s", p)
	}
}

func TestPersonaBase_CarriesFullContractRule(t *testing.T) {
	low := strings.ToLower(persona(""))
	if !strings.Contains(low, "contract") {
		t.Errorf("persona base must carry the deliver-the-full-contract rule: %q", persona(""))
	}
	// The audience lever must still be appended on top of the new rule.
	if !strings.Contains(low, "operating this system") {
		t.Errorf("human-operator audience suffix lost: %q", persona(""))
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
