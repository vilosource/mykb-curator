package zonemarkers

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
)

// Compile-time check that ApplyZoneMarkers satisfies the Pass interface.
var _ passes.Pass = (*ApplyZoneMarkers)(nil)

func TestName_IsStable(t *testing.T) {
	p := New()
	if p.Name() != "apply-zone-markers" {
		t.Errorf("Name = %q, want %q", p.Name(), "apply-zone-markers")
	}
}

func TestApply_EmptyDocument_NoOp(t *testing.T) {
	out, err := New().Apply(context.Background(), ir.Document{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out.Sections) != 0 {
		t.Errorf("expected empty sections, got %d", len(out.Sections))
	}
}

func TestApply_NoMachineBlocks_PreservesBlocksUntouched(t *testing.T) {
	in := ir.Document{Sections: []ir.Section{{
		Heading: "S",
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "prose"},
			ir.Callout{Severity: "note", Body: "callout"},
		},
	}}}
	out, _ := New().Apply(context.Background(), in)
	if got := len(out.Sections[0].Blocks); got != 2 {
		t.Fatalf("len(Blocks) = %d, want 2 (no markers should be added)", got)
	}
	if _, ok := out.Sections[0].Blocks[0].(ir.ProseBlock); !ok {
		t.Errorf("block[0] is not ProseBlock; type=%T", out.Sections[0].Blocks[0])
	}
}

func TestApply_WrapsSingleMachineBlock(t *testing.T) {
	mb := ir.MachineBlock{
		BlockID: "rg-table",
		Kind_:   "rg-table",
		Body:    "table body",
		Prov:    ir.Provenance{InputHash: "h1", Sources: []string{"area/infra-azure"}},
	}
	in := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{mb},
	}}}
	out, _ := New().Apply(context.Background(), in)

	blocks := out.Sections[0].Blocks
	if len(blocks) != 3 {
		t.Fatalf("len(Blocks) = %d, want 3 (begin marker, machine, end marker); got %+v", len(blocks), blocks)
	}

	begin, ok := blocks[0].(ir.MarkerBlock)
	if !ok || begin.Position != ir.MarkerBegin {
		t.Errorf("blocks[0]: want MarkerBlock{begin}, got %T %+v", blocks[0], blocks[0])
	}
	if begin.BlockID != "rg-table" {
		t.Errorf("begin.BlockID = %q, want %q", begin.BlockID, "rg-table")
	}
	if begin.Prov.InputHash != "h1" {
		t.Errorf("begin.Prov.InputHash = %q, want %q (must carry the machine block's provenance)", begin.Prov.InputHash, "h1")
	}

	if _, ok := blocks[1].(ir.MachineBlock); !ok {
		t.Errorf("blocks[1]: want MachineBlock, got %T", blocks[1])
	}

	end, ok := blocks[2].(ir.MarkerBlock)
	if !ok || end.Position != ir.MarkerEnd {
		t.Errorf("blocks[2]: want MarkerBlock{end}, got %T %+v", blocks[2], blocks[2])
	}
	if end.BlockID != "rg-table" {
		t.Errorf("end.BlockID = %q, want %q", end.BlockID, "rg-table")
	}
}

func TestApply_WrapsEachMachineBlockIndependently(t *testing.T) {
	in := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.MachineBlock{BlockID: "a", Body: "A", Prov: ir.Provenance{InputHash: "ha"}},
			ir.MachineBlock{BlockID: "b", Body: "B", Prov: ir.Provenance{InputHash: "hb"}},
		},
	}}}
	out, _ := New().Apply(context.Background(), in)
	blocks := out.Sections[0].Blocks
	if len(blocks) != 6 {
		t.Fatalf("len(Blocks) = %d, want 6 (begin-a, A, end-a, begin-b, B, end-b)", len(blocks))
	}
	wantIDs := []string{"a", "a", "a", "b", "b", "b"}
	for i, want := range wantIDs {
		var got string
		switch b := blocks[i].(type) {
		case ir.MarkerBlock:
			got = b.BlockID
		case ir.MachineBlock:
			got = b.BlockID
		}
		if got != want {
			t.Errorf("blocks[%d]: id = %q, want %q", i, got, want)
		}
	}
}

func TestApply_PreservesInterleavedEditorialBlocks(t *testing.T) {
	in := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "intro"},
			ir.MachineBlock{BlockID: "tbl", Body: "rows", Prov: ir.Provenance{InputHash: "h"}},
			ir.ProseBlock{Text: "outro"},
		},
	}}}
	out, _ := New().Apply(context.Background(), in)
	blocks := out.Sections[0].Blocks
	// expected: prose, begin, machine, end, prose
	if len(blocks) != 5 {
		t.Fatalf("len(Blocks) = %d, want 5; got %+v", len(blocks), blocks)
	}
	if _, ok := blocks[0].(ir.ProseBlock); !ok {
		t.Errorf("blocks[0] should remain ProseBlock; type=%T", blocks[0])
	}
	if mb, ok := blocks[1].(ir.MarkerBlock); !ok || mb.Position != ir.MarkerBegin {
		t.Errorf("blocks[1] should be MarkerBlock{begin}; got %T %+v", blocks[1], blocks[1])
	}
	if _, ok := blocks[4].(ir.ProseBlock); !ok {
		t.Errorf("blocks[4] should remain ProseBlock; type=%T", blocks[4])
	}
}

func TestApply_Idempotent_DoesNotReWrapExistingMarkers(t *testing.T) {
	// Running the pass twice must produce the same output as running
	// it once. Without this, a re-run on already-marked IR would
	// double-wrap.
	in := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.MachineBlock{BlockID: "m", Body: "body", Prov: ir.Provenance{InputHash: "h"}},
		},
	}}}
	once, _ := New().Apply(context.Background(), in)
	twice, _ := New().Apply(context.Background(), once)
	if got := len(twice.Sections[0].Blocks); got != 3 {
		t.Errorf("len(Blocks) after re-apply = %d, want 3 (no double-wrap)", got)
	}
}
