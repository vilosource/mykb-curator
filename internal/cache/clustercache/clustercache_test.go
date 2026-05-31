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
