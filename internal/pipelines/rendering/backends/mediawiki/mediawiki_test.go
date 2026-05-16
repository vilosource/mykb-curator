package mediawiki_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/mediawiki"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestBackend_Name(t *testing.T) {
	if got := mediawiki.New().Name(); got != "mediawiki" {
		t.Errorf("Name = %q, want %q", got, "mediawiki")
	}
}

// The whole reason this backend exists: Markdown-into-MediaWiki
// rendered headings as list items and printed YAML frontmatter as
// body text. These assertions lock that out.
func TestRender_NoMarkdownArtifacts(t *testing.T) {
	doc := ir.Document{
		Frontmatter: ir.Frontmatter{Title: "Azure Infrastructure", SpecHash: "h1"},
		Sections: []ir.Section{{
			Heading: "Infrastructure Service Stacks",
			Blocks:  []ir.Block{ir.ProseBlock{Text: "Vault is the secrets manager."}},
		}},
	}
	out, err := mediawiki.New().Render(doc)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)

	// No YAML frontmatter fence as visible body.
	if strings.HasPrefix(strings.TrimSpace(s), "---") {
		t.Errorf("output starts with a YAML frontmatter fence:\n%s", s)
	}
	for _, bad := range []string{"\n---\n", "title: Azure", "spec_hash:"} {
		if strings.Contains(s, bad) {
			t.Errorf("output contains markdown/frontmatter artifact %q\n---\n%s", bad, s)
		}
	}
	// No markdown ATX headings (#, ##, ###) at line starts.
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "#") {
			t.Errorf("markdown heading leaked into wikitext: %q", line)
		}
	}
	// Section heading is a wikitext H2; page title is NOT emitted in
	// body (the wiki page name is the title).
	if !strings.Contains(s, "== Infrastructure Service Stacks ==") {
		t.Errorf("section heading not rendered as wikitext H2:\n%s", s)
	}
	if strings.Contains(s, "Azure Infrastructure\n") && strings.Contains(s, "= Azure Infrastructure =") {
		t.Errorf("page title should not be emitted as a body heading:\n%s", s)
	}
}

func TestRender_ProseIsBlankLineSeparatedParagraph(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "Notes",
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "First para."},
			ir.ProseBlock{Text: "Second para."},
		},
	}}}
	out, _ := mediawiki.New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "First para.\n\nSecond para.") {
		t.Errorf("paragraphs must be blank-line separated:\n%s", s)
	}
}

func TestRender_MultiParagraphProse_NotMerged(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "S",
		Blocks:  []ir.Block{ir.ProseBlock{Text: "Para one\nwrapped.\n\nPara two."}},
	}}}
	out, _ := mediawiki.New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "Para one wrapped.\n\nPara two.") {
		t.Errorf("paragraphs merged or wrap not joined:\n%s", s)
	}
}

func TestRender_LeadingMarkupGuarded(t *testing.T) {
	// Residual markdown the frontend didn't strip must not render as
	// a wikitext list/heading.
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "S",
		Blocks:  []ir.Block{ir.ProseBlock{Text: "# leaked heading text"}},
	}}}
	out, _ := mediawiki.New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "<nowiki/># leaked heading text") {
		t.Errorf("leading markup not guarded with <nowiki/>:\n%s", s)
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "#") {
			t.Errorf("unguarded markup line leaked: %q", line)
		}
	}
}

func TestRender_MarkerBlocks_AreInertHTMLComments(t *testing.T) {
	// Reconciler depends on the EXACT marker convention; it must be
	// byte-identical to the markdown backend's (HTML comments are
	// inert in wikitext too).
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "RG",
		Blocks: []ir.Block{
			ir.MarkerBlock{Position: ir.MarkerBegin, BlockID: "rg", OfZone: "machine", Prov: ir.Provenance{InputHash: "h1"}},
			ir.MachineBlock{BlockID: "rg", Body: "data row"},
			ir.MarkerBlock{Position: ir.MarkerEnd, BlockID: "rg"},
		},
	}}}
	out, _ := mediawiki.New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "<!-- CURATOR:BEGIN block=rg zone=machine provenance=h1 -->") {
		t.Errorf("BEGIN marker convention changed:\n%s", s)
	}
	if !strings.Contains(s, "<!-- CURATOR:END block=rg -->") {
		t.Errorf("END marker convention changed:\n%s", s)
	}
}

func TestRender_Table_IsWikitextTable(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "Inventory",
		Blocks: []ir.Block{ir.TableBlock{
			Columns: []string{"Name", "Region"},
			Rows:    [][]string{{"rg-vault", "swedencentral"}},
		}},
	}}}
	out, _ := mediawiki.New().Render(doc)
	s := string(out)
	for _, want := range []string{`{| class="wikitable"`, "! Name !! Region", "|-", "| rg-vault || swedencentral", "|}"} {
		if !strings.Contains(s, want) {
			t.Errorf("wikitext table missing %q\n---\n%s", want, s)
		}
	}
}

func TestRender_Diagram_AssetRefIsFileEmbed(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "Topology",
		Blocks:  []ir.Block{ir.DiagramBlock{Lang: "mermaid", AssetRef: "File:diagram-abc.png"}},
	}}}
	out, _ := mediawiki.New().Render(doc)
	if !strings.Contains(string(out), "[[File:diagram-abc.png]]") {
		t.Errorf("asset-ref diagram must embed as [[File:...]]:\n%s", out)
	}
}

func TestRender_ProvenanceIsLeadingHTMLComment(t *testing.T) {
	doc := ir.Document{Frontmatter: ir.Frontmatter{Title: "P", SpecHash: "sh", KBCommit: "kc"}}
	out, _ := mediawiki.New().Render(doc)
	s := string(out)
	if !strings.HasPrefix(s, "<!-- mykb-curator") {
		t.Errorf("provenance should lead as an inert HTML comment, got:\n%s", s)
	}
	if !strings.Contains(s, "spec_hash=sh") || !strings.Contains(s, "kb_commit=kc") {
		t.Errorf("provenance comment missing fields:\n%s", s)
	}
}

func TestRender_Determinism(t *testing.T) {
	d := sampleDoc()
	a, _ := mediawiki.New().Render(d)
	b, _ := mediawiki.New().Render(d)
	if !bytes.Equal(a, b) {
		t.Errorf("non-deterministic output")
	}
}

func TestRender_Golden(t *testing.T) {
	out, err := mediawiki.New().Render(sampleDoc())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	golden := filepath.Join("testdata", "full_document.golden.wiki")
	if *updateGolden {
		if err := os.WriteFile(golden, out, 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run -update first): %v", err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("golden mismatch.\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

func sampleDoc() ir.Document {
	ts := time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC)
	return ir.Document{
		Frontmatter: ir.Frontmatter{Title: "Vault Architecture", SpecHash: "spec-abc123", KBCommit: "kb-def456", GeneratedAt: ts},
		Sections: []ir.Section{
			{Heading: "Overview", Blocks: []ir.Block{ir.ProseBlock{Text: "Vault is the centralised secrets manager."}}},
			{Heading: "Resource Groups", Blocks: []ir.Block{
				ir.MarkerBlock{Position: ir.MarkerBegin, BlockID: "rg-table", OfZone: "machine", Prov: ir.Provenance{InputHash: "h1"}},
				ir.MachineBlock{BlockID: "rg-table", Kind_: "rg-table", Body: "data"},
				ir.MarkerBlock{Position: ir.MarkerEnd, BlockID: "rg-table"},
			}},
			{Heading: "Topology", Blocks: []ir.Block{ir.DiagramBlock{Lang: "mermaid", Source: "graph TD; A-->B;"}}},
			{Heading: "Notes", Blocks: []ir.Block{ir.Callout{Severity: "note", Body: "Auto-unseal uses Azure Key Vault."}}},
		},
		Footer: ir.Footer{RunID: "run-1", KBCommit: "kb-def456", LastCurated: ts},
	}
}
