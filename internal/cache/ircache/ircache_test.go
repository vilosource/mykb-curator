package ircache

import (
	"path/filepath"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

func newOpen(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestKey_StableForIdenticalInputs(t *testing.T) {
	a := Key("spec-h", "kb-h", "v1")
	b := Key("spec-h", "kb-h", "v1")
	if a != b {
		t.Errorf("Key not stable: %q vs %q", a, b)
	}
}

func TestKey_DistinctForDifferingInputs(t *testing.T) {
	base := Key("spec", "kb", "v1")
	if Key("other", "kb", "v1") == base {
		t.Errorf("spec-hash change should produce different key")
	}
	if Key("spec", "other", "v1") == base {
		t.Errorf("kb-hash change should produce different key")
	}
	if Key("spec", "kb", "v2") == base {
		t.Errorf("pipeline-version change should produce different key")
	}
}

func TestGet_Miss_ReturnsFalse(t *testing.T) {
	c := newOpen(t)
	doc, ok, err := c.Get("no-such-key")
	if err != nil {
		t.Errorf("Get error: %v", err)
	}
	if ok {
		t.Errorf("ok = true, want false for missing key")
	}
	if doc != nil {
		t.Errorf("doc = %+v, want nil", doc)
	}
}

func TestSetGet_RoundTrip_SimpleDoc(t *testing.T) {
	c := newOpen(t)
	want := ir.Document{
		Frontmatter: ir.Frontmatter{Title: "P", SpecHash: "h", KBCommit: "kb"},
		Sections: []ir.Section{
			{Heading: "S", Blocks: []ir.Block{ir.ProseBlock{Text: "hello"}}},
		},
	}
	if err := c.Set("k", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := c.Get("k")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.Frontmatter.Title != "P" {
		t.Errorf("Title = %q, want %q", got.Frontmatter.Title, "P")
	}
	if len(got.Sections) != 1 {
		t.Fatalf("Sections = %d, want 1", len(got.Sections))
	}
	prose, ok := got.Sections[0].Blocks[0].(ir.ProseBlock)
	if !ok {
		t.Fatalf("Blocks[0] = %T, want ProseBlock", got.Sections[0].Blocks[0])
	}
	if prose.Text != "hello" {
		t.Errorf("Text = %q, want %q", prose.Text, "hello")
	}
}

func TestSetGet_AllBlockKinds(t *testing.T) {
	c := newOpen(t)
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "prose"},
			ir.MachineBlock{BlockID: "m", Body: "body"},
			ir.KBRefBlock{Area: "a", ID: "1"},
			ir.TableBlock{Columns: []string{"c"}, Rows: [][]string{{"r"}}},
			ir.DiagramBlock{Lang: "mermaid", Source: "graph"},
			ir.Callout{Severity: "note", Body: "n"},
			ir.EscapeHatch{Backend: "markdown", Raw: "raw"},
			ir.MarkerBlock{Position: ir.MarkerBegin, BlockID: "x"},
		},
	}}}
	if err := c.Set("k", doc); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, _, _ := c.Get("k")
	if len(got.Sections[0].Blocks) != 8 {
		t.Fatalf("len = %d, want 8 (round-trip lost blocks)", len(got.Sections[0].Blocks))
	}
}

func TestPersistence_AcrossOpens(t *testing.T) {
	dir := t.TempDir()
	c1, _ := Open(dir)
	_ = c1.Set("k", ir.Document{Frontmatter: ir.Frontmatter{Title: "persisted"}})

	c2, _ := Open(dir)
	got, ok, _ := c2.Get("k")
	if !ok || got.Frontmatter.Title != "persisted" {
		t.Errorf("persistence broken: got=%+v ok=%v", got, ok)
	}
}

func TestHashKBSubset_StableSorted(t *testing.T) {
	// Same content in different area order should produce the same hash.
	a := HashKBSubset(kb.Snapshot{Areas: []kb.Area{
		{ID: "vault", Entries: []kb.Entry{{ID: "1", Text: "x"}}},
		{ID: "harbor", Entries: []kb.Entry{{ID: "2", Text: "y"}}},
	}}, specs.IncludeFilter{Areas: []string{"vault", "harbor"}})
	b := HashKBSubset(kb.Snapshot{Areas: []kb.Area{
		{ID: "harbor", Entries: []kb.Entry{{ID: "2", Text: "y"}}},
		{ID: "vault", Entries: []kb.Entry{{ID: "1", Text: "x"}}},
	}}, specs.IncludeFilter{Areas: []string{"harbor", "vault"}})
	if a != b {
		t.Errorf("HashKBSubset is order-dependent: %q vs %q", a, b)
	}
}

func TestHashKBSubset_DifferentWhenContentDiffers(t *testing.T) {
	snap := kb.Snapshot{Areas: []kb.Area{{ID: "vault", Entries: []kb.Entry{{ID: "1", Text: "alpha"}}}}}
	a := HashKBSubset(snap, specs.IncludeFilter{Areas: []string{"vault"}})

	snap.Areas[0].Entries[0].Text = "beta"
	b := HashKBSubset(snap, specs.IncludeFilter{Areas: []string{"vault"}})
	if a == b {
		t.Errorf("HashKBSubset should differ when content differs")
	}
}

func TestHashKBSubset_HonoursIncludeFilter(t *testing.T) {
	// Areas not in the include list must not affect the hash —
	// otherwise two specs sharing kb but declaring different
	// includes would invalidate each other's cache.
	snap := kb.Snapshot{Areas: []kb.Area{
		{ID: "vault", Entries: []kb.Entry{{ID: "v", Text: "vault content"}}},
		{ID: "harbor", Entries: []kb.Entry{{ID: "h", Text: "ORIGINAL harbor"}}},
	}}
	a := HashKBSubset(snap, specs.IncludeFilter{Areas: []string{"vault"}})

	// Mutate harbor only.
	snap.Areas[1].Entries[0].Text = "MUTATED harbor"
	b := HashKBSubset(snap, specs.IncludeFilter{Areas: []string{"vault"}})
	if a != b {
		t.Errorf("filtered-out areas should not affect hash: %q vs %q", a, b)
	}
}

// Sanity: cache file is created on disk in the right dir.
func TestSet_WritesFile(t *testing.T) {
	dir := t.TempDir()
	c, _ := Open(dir)
	_ = c.Set("hello", ir.Document{})

	// Should be a file named "hello.gob" (or similar) in dir.
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(matches) != 1 {
		t.Errorf("expected 1 cache file, found %d: %v", len(matches), matches)
	}
}
