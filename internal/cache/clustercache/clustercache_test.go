package clustercache

import (
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

func TestSetGet_RoundTripsIRAndVerdict(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []PageResult{{
		Page: "Vault Architecture", Kind: "architecture", Verdict: "all pass", Iters: 1,
		Doc: ir.Document{Sections: []ir.Section{{Heading: "S", Blocks: []ir.Block{ir.ProseBlock{Text: "hi"}}}}},
	}}
	if err := c.Set("k1", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get("k1")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if len(got) != 1 || got[0].Page != "Vault Architecture" || got[0].Verdict != "all pass" || got[0].Iters != 1 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got[0].Doc.Sections) != 1 || got[0].Doc.Sections[0].Heading != "S" {
		t.Errorf("IR did not round-trip: %+v", got[0].Doc)
	}
}

func TestGet_MissIsCleanFalse(t *testing.T) {
	c, _ := Open(t.TempDir())
	_, ok, err := c.Get("absent")
	if ok || err != nil {
		t.Errorf("miss should be (false,nil), got ok=%v err=%v", ok, err)
	}
}

// Every ir.Block type must be gob-registered, or Set fails at runtime
// (it did, silently, for CategoryBlock until this was caught live). A
// populated value of each type exercises the interface encoding.
func TestSet_AllBlockTypesEncode(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{Heading: "S", Blocks: []ir.Block{
		ir.ProseBlock{Text: "p"},
		ir.MachineBlock{Body: "m"},
		ir.KBRefBlock{Area: "a", ID: "x"},
		ir.TableBlock{},
		ir.IndexBlock{Entries: []ir.IndexEntry{{Page: "P", Label: "L"}}},
		ir.CategoryBlock{Names: []string{"Azure Infrastructure"}},
		ir.DiagramBlock{Lang: "mermaid"},
		ir.Callout{},
		ir.EscapeHatch{},
		ir.MarkerBlock{},
	}}}}
	c, _ := Open(t.TempDir())
	if err := c.Set("k", []PageResult{{Page: "P", Doc: doc, Verdict: "v", Iters: 2}}); err != nil {
		t.Fatalf("Set must encode every block type (incl CategoryBlock): %v", err)
	}
	got, ok, err := c.Get("k")
	if err != nil || !ok || len(got) != 1 || len(got[0].Doc.Sections[0].Blocks) != 10 {
		t.Fatalf("round-trip lost blocks: ok=%v err=%v got=%+v", ok, err, got)
	}
}
