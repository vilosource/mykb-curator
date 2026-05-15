package markdown

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// Compile-time check that the Backend interface is satisfied.
var _ backends.Backend = (*Backend)(nil)

func TestBackend_Name(t *testing.T) {
	if got := New().Name(); got != "markdown" {
		t.Errorf("Name = %q, want %q", got, "markdown")
	}
}

func TestRender_EmptyDocumentProducesFrontmatterAndFooter(t *testing.T) {
	doc := ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:       "Empty",
			SpecHash:    "spec-abc",
			KBCommit:    "kb-def",
			GeneratedAt: time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC),
		},
		Footer: ir.Footer{
			RunID:       "run-1",
			KBCommit:    "kb-def",
			LastCurated: time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC),
		},
	}
	out, err := New().Render(doc)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, want := range []string{"title: Empty", "spec_hash: spec-abc", "kb_commit: kb-def", "run-1"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q\n---\n%s\n---", want, s)
		}
	}
}

func TestRender_Determinism_SameIRSameBytes(t *testing.T) {
	doc := sampleDoc()
	a, err := New().Render(doc)
	if err != nil {
		t.Fatalf("Render a: %v", err)
	}
	b, err := New().Render(doc)
	if err != nil {
		t.Fatalf("Render b: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("non-deterministic output: byte-equal Render(doc) twice expected")
	}
}

func TestRender_ProseBlock_IsParagraph(t *testing.T) {
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Notes",
			Blocks: []ir.Block{
				ir.ProseBlock{Text: "Hello world."},
			},
		}},
	}
	out, _ := New().Render(doc)
	if !strings.Contains(string(out), "Hello world.") {
		t.Errorf("prose missing from output:\n%s", out)
	}
	if !strings.Contains(string(out), "## Notes") {
		t.Errorf("section heading missing from output:\n%s", out)
	}
}

func TestRender_MachineBlock_BodyOnly_NoInlineMarkers(t *testing.T) {
	// Per the refactor: backends render IR mechanically. Markers come
	// from MarkerBlock (produced by the ApplyZoneMarkers pass), NOT
	// from inline emission inside MachineBlock rendering. Asserting
	// no marker text in output of a MachineBlock alone proves the
	// separation of concerns.
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Resource Groups",
			Blocks: []ir.Block{
				ir.MachineBlock{
					BlockID: "rg-table",
					Kind_:   "rg-table",
					Body:    "table body here",
					Prov:    ir.Provenance{Sources: []string{"area/infra-azure"}, InputHash: "abc123"},
				},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if strings.Contains(s, "CURATOR:BEGIN") {
		t.Errorf("backend must NOT emit BEGIN marker on its own (responsibility moved to ApplyZoneMarkers pass):\n%s", s)
	}
	if strings.Contains(s, "CURATOR:END") {
		t.Errorf("backend must NOT emit END marker on its own:\n%s", s)
	}
	if !strings.Contains(s, "table body here") {
		t.Errorf("missing block body (backend should still render body):\n%s", s)
	}
}

func TestRender_MarkerBlock_RendersAsHTMLComment(t *testing.T) {
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Resource Groups",
			Blocks: []ir.Block{
				ir.MarkerBlock{Position: ir.MarkerBegin, BlockID: "rg-table", Prov: ir.Provenance{InputHash: "abc123"}},
				ir.MachineBlock{BlockID: "rg-table", Body: "table body", Prov: ir.Provenance{InputHash: "abc123"}},
				ir.MarkerBlock{Position: ir.MarkerEnd, BlockID: "rg-table", Prov: ir.Provenance{InputHash: "abc123"}},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "<!-- CURATOR:BEGIN block=rg-table provenance=abc123 -->") {
		t.Errorf("missing BEGIN marker rendering:\n%s", s)
	}
	if !strings.Contains(s, "<!-- CURATOR:END block=rg-table -->") {
		t.Errorf("missing END marker rendering:\n%s", s)
	}
	if !strings.Contains(s, "table body") {
		t.Errorf("missing machine block body:\n%s", s)
	}
}

func TestRender_TableBlock_RendersAsMarkdownTable(t *testing.T) {
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "RGs",
			Blocks: []ir.Block{
				ir.TableBlock{
					Columns: []string{"Name", "Region"},
					Rows: [][]string{
						{"rg-1", "swedencentral"},
						{"rg-2", "westeurope"},
					},
				},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "| Name | Region |") {
		t.Errorf("missing header row:\n%s", s)
	}
	if !strings.Contains(s, "| --- | --- |") {
		t.Errorf("missing alignment row:\n%s", s)
	}
	if !strings.Contains(s, "| rg-1 | swedencentral |") {
		t.Errorf("missing data row 1:\n%s", s)
	}
	if !strings.Contains(s, "| rg-2 | westeurope |") {
		t.Errorf("missing data row 2:\n%s", s)
	}
}

func TestRender_KBRefBlock_HasResolvableMarker(t *testing.T) {
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Refs",
			Blocks: []ir.Block{
				ir.KBRefBlock{Area: "vault", ID: "fact:abc123", Mode: "inline"},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "vault") || !strings.Contains(s, "fact:abc123") {
		t.Errorf("kbref didn't reference area+id:\n%s", s)
	}
}

func TestRender_DiagramBlock_MermaidUnresolvedRendersFencedCode(t *testing.T) {
	// Pre-RenderDiagrams pass: AssetRef is empty so the source is
	// rendered as a fenced mermaid code block — readable in markdown
	// viewers that support it (GitHub, etc.).
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Diagram",
			Blocks: []ir.Block{
				ir.DiagramBlock{
					Lang:   "mermaid",
					Source: "graph TD; A-->B;",
				},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "```mermaid") {
		t.Errorf("missing fenced mermaid block:\n%s", s)
	}
	if !strings.Contains(s, "graph TD; A-->B;") {
		t.Errorf("missing diagram source:\n%s", s)
	}
}

func TestRender_DiagramBlock_ResolvedRendersImageRef(t *testing.T) {
	// Post-RenderDiagrams pass: AssetRef populated, source kept as
	// HTML comment for traceability.
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Diagram",
			Blocks: []ir.Block{
				ir.DiagramBlock{
					Lang:     "mermaid",
					Source:   "graph TD; A-->B;",
					AssetRef: "diagrams/foo.png",
				},
			},
		}},
	}
	out, _ := New().Render(doc)
	if !strings.Contains(string(out), "![](diagrams/foo.png)") {
		t.Errorf("missing image ref:\n%s", out)
	}
}

func TestRender_Callout_RendersAsBlockquote(t *testing.T) {
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Notes",
			Blocks: []ir.Block{
				ir.Callout{Severity: "warning", Body: "Be careful."},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "> **warning**") {
		t.Errorf("missing severity tag:\n%s", s)
	}
	if !strings.Contains(s, "> Be careful.") {
		t.Errorf("missing body:\n%s", s)
	}
}

func TestRender_EscapeHatch_MatchingBackendInlined_OthersOmitted(t *testing.T) {
	doc := ir.Document{
		Sections: []ir.Section{{
			Heading: "Esc",
			Blocks: []ir.Block{
				ir.EscapeHatch{Backend: "markdown", Raw: "RAW-MD-CONTENT"},
				ir.EscapeHatch{Backend: "mediawiki", Raw: "RAW-MW-CONTENT"},
			},
		}},
	}
	out, _ := New().Render(doc)
	s := string(out)
	if !strings.Contains(s, "RAW-MD-CONTENT") {
		t.Errorf("markdown escape-hatch should be inlined:\n%s", s)
	}
	if strings.Contains(s, "RAW-MW-CONTENT") {
		t.Errorf("mediawiki escape-hatch must NOT leak into markdown backend:\n%s", s)
	}
}

func TestRender_FullDocument_GoldenFile(t *testing.T) {
	out, err := New().Render(sampleDoc())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	goldenPath := filepath.Join("testdata", "full_document.golden.md")
	if *updateGolden {
		_ = os.MkdirAll(filepath.Dir(goldenPath), 0o755)
		if err := os.WriteFile(goldenPath, out, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden file %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("output does not match golden file %s\nrun with -update to regenerate, then review the diff", goldenPath)
		t.Logf("got:\n%s", out)
	}
}

// sampleDoc is a representative IR exercising every block kind —
// shared by the determinism test and the golden-file test so a single
// fixture proves both properties.
func sampleDoc() ir.Document {
	ts := time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC)
	return ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:       "Vault Architecture",
			SpecHash:    "spec-abc123",
			KBCommit:    "kb-def456",
			GeneratedAt: ts,
		},
		Sections: []ir.Section{
			{
				Heading: "Overview",
				Blocks: []ir.Block{
					ir.ProseBlock{Text: "Vault is the centralised secrets manager."},
				},
			},
			{
				// Post-ApplyZoneMarkers shape: machine block wrapped
				// in begin/end MarkerBlocks.
				Heading: "Resource Groups",
				Blocks: []ir.Block{
					ir.MarkerBlock{
						Position: ir.MarkerBegin,
						BlockID:  "rg-table",
						Prov:     ir.Provenance{InputHash: "h1"},
					},
					ir.MachineBlock{
						BlockID: "rg-table",
						Kind_:   "rg-table",
						Body:    "| rg-vault-prod | swedencentral | shared |",
						Prov:    ir.Provenance{Sources: []string{"area/infra-azure"}, InputHash: "h1"},
					},
					ir.MarkerBlock{
						Position: ir.MarkerEnd,
						BlockID:  "rg-table",
						Prov:     ir.Provenance{InputHash: "h1"},
					},
				},
			},
			{
				Heading: "Topology",
				Blocks: []ir.Block{
					ir.DiagramBlock{Lang: "mermaid", Source: "graph TD; A-->B;"},
				},
			},
			{
				Heading: "Notes",
				Blocks: []ir.Block{
					ir.Callout{Severity: "note", Body: "Auto-unseal uses Azure Key Vault."},
				},
			},
		},
		Footer: ir.Footer{
			RunID:       "run-1",
			KBCommit:    "kb-def456",
			LastCurated: ts,
		},
	}
}
