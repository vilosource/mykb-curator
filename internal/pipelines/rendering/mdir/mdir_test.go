package mdir_test

import (
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/mdir"
)

func TestParse_HeadingsFencesProvenance(t *testing.T) {
	md := "Lead prose.\n\n## One\n\nbody one\n\n### Sub\n\nsub body\n\n```mermaid\ngraph TD; A-->B\n```\n"
	secs := mdir.Parse(md, "architecture", "h1")

	// lead (no-heading) + One + Sub; the fence stays inside Sub
	// (a fence does not start a new section).
	if len(secs) != 3 {
		t.Fatalf("sections=%d, want 3 got %+v", len(secs), secs)
	}
	if secs[0].Heading != "" {
		t.Errorf("pre-heading prose must be a no-heading section, got %q", secs[0].Heading)
	}
	if secs[1].Heading != "One" || secs[2].Heading != "Sub" {
		t.Errorf("ATX headings (incl ###) must start sections: %q %q", secs[1].Heading, secs[2].Heading)
	}
	pb, ok := secs[0].Blocks[0].(ir.ProseBlock)
	if !ok || pb.Prov.SpecSection != "architecture-section" || pb.Prov.InputHash != "h1" {
		t.Errorf("prose provenance prefix wrong: %+v", secs[0].Blocks[0])
	}
	// the mermaid fence becomes a DiagramBlock with the prefixed prov
	var diag *ir.DiagramBlock
	for _, s := range secs {
		for _, b := range s.Blocks {
			if d, ok := b.(ir.DiagramBlock); ok {
				dd := d
				diag = &dd
			}
		}
	}
	if diag == nil || diag.Lang != "mermaid" || diag.Prov.SpecSection != "architecture-diagram" {
		t.Errorf("fenced mermaid → DiagramBlock with prefixed prov, got %+v", diag)
	}
	if strings.Contains(md, "```") && diag != nil && strings.Contains(diag.Source, "`") {
		t.Errorf("fence markers must not be in diagram source: %q", diag.Source)
	}
}

func TestParse_Empty(t *testing.T) {
	if got := mdir.Parse("   \n", "x", "h"); len(got) != 0 {
		t.Errorf("blank input → no sections, got %+v", got)
	}
}
