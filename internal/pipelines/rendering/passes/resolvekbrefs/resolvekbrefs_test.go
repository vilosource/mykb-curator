package resolvekbrefs

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
)

var _ passes.Pass = (*ResolveKBRefs)(nil)

func TestName(t *testing.T) {
	if got := New(kb.Snapshot{}).Name(); got != "resolve-kb-refs" {
		t.Errorf("Name = %q, want %q", got, "resolve-kb-refs")
	}
}

func TestApply_EmptyDocument_NoOp(t *testing.T) {
	out, err := New(kb.Snapshot{}).Apply(context.Background(), ir.Document{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out.Sections) != 0 {
		t.Errorf("unexpected sections in empty document")
	}
}

func TestApply_NoKBRefBlocks_PreservesContent(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "S",
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "prose"},
			ir.Callout{Severity: "note", Body: "callout"},
		},
	}}}
	out, _ := New(kb.Snapshot{}).Apply(context.Background(), doc)
	if len(out.Sections[0].Blocks) != 2 {
		t.Errorf("len = %d, want 2 (no KBRefBlocks → no transform)", len(out.Sections[0].Blocks))
	}
}

func TestApply_KBRefBlock_ReplacedWithEntryText(t *testing.T) {
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault", Entries: []kb.Entry{
			{ID: "f1", Type: "fact", Text: "Vault HA on Raft"},
		},
	}}}
	doc := ir.Document{Sections: []ir.Section{{
		Heading: "Refs",
		Blocks: []ir.Block{
			ir.KBRefBlock{Area: "vault", ID: "f1", Mode: "inline"},
		},
	}}}
	out, err := New(snap).Apply(context.Background(), doc)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out.Sections[0].Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(out.Sections[0].Blocks))
	}
	p, ok := out.Sections[0].Blocks[0].(ir.ProseBlock)
	if !ok {
		t.Fatalf("block is not ProseBlock: type=%T", out.Sections[0].Blocks[0])
	}
	if !strings.Contains(p.Text, "Vault HA on Raft") {
		t.Errorf("resolved prose missing entry text: %q", p.Text)
	}
}

func TestApply_UnresolvedKBRef_LeavesPlaceholder(t *testing.T) {
	// Spec referenced a fact that's not in the kb snapshot. We can't
	// fail the pass (every kb-ref typo would break a render); instead
	// produce a visible placeholder so the operator sees it in the
	// rendered page AND in the run report.
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.KBRefBlock{Area: "vault", ID: "does-not-exist"},
		},
	}}}
	out, _ := New(kb.Snapshot{Areas: []kb.Area{{ID: "vault"}}}).Apply(context.Background(), doc)
	if len(out.Sections[0].Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1 (replaced with placeholder)", len(out.Sections[0].Blocks))
	}
	p, ok := out.Sections[0].Blocks[0].(ir.ProseBlock)
	if !ok {
		t.Fatalf("expected ProseBlock placeholder, got %T", out.Sections[0].Blocks[0])
	}
	if !strings.Contains(p.Text, "UNRESOLVED") {
		t.Errorf("placeholder text doesn't flag UNRESOLVED: %q", p.Text)
	}
	if !strings.Contains(p.Text, "vault") || !strings.Contains(p.Text, "does-not-exist") {
		t.Errorf("placeholder should name the missing ref: %q", p.Text)
	}
}

func TestApply_MissingArea_AlsoProducesPlaceholder(t *testing.T) {
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{ir.KBRefBlock{Area: "nonexistent", ID: "x"}},
	}}}
	out, _ := New(kb.Snapshot{}).Apply(context.Background(), doc)
	p := out.Sections[0].Blocks[0].(ir.ProseBlock)
	if !strings.Contains(p.Text, "UNRESOLVED") {
		t.Errorf("missing area should also unresolved-placeholder: %q", p.Text)
	}
}

func TestApply_PreservesNonRefBlocksAround(t *testing.T) {
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "v", Entries: []kb.Entry{{ID: "f", Type: "fact", Text: "FACT"}},
	}}}
	doc := ir.Document{Sections: []ir.Section{{
		Blocks: []ir.Block{
			ir.ProseBlock{Text: "before"},
			ir.KBRefBlock{Area: "v", ID: "f"},
			ir.ProseBlock{Text: "after"},
		},
	}}}
	out, _ := New(snap).Apply(context.Background(), doc)
	blocks := out.Sections[0].Blocks
	if len(blocks) != 3 {
		t.Fatalf("len = %d, want 3", len(blocks))
	}
	if blocks[0].(ir.ProseBlock).Text != "before" {
		t.Errorf("before lost: %+v", blocks[0])
	}
	if blocks[2].(ir.ProseBlock).Text != "after" {
		t.Errorf("after lost: %+v", blocks[2])
	}
}
