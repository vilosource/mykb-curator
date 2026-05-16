package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// the real frontend must satisfy the cluster's Renderer contract.
var _ Renderer = (*architecture.Frontend)(nil)

// stubRenderer returns a doc keyed by page title. The parent doc
// carries a child-index placeholder exactly as the architecture
// frontend would emit it.
type stubRenderer struct {
	docs map[string]ir.Document
	err  error
}

func (s *stubRenderer) Render(_ context.Context, p docspec.DocPage, _ kb.Snapshot) (ir.Document, error) {
	if s.err != nil {
		return ir.Document{}, s.err
	}
	return s.docs[p.Page], nil
}

func placeholderDoc(title string) ir.Document {
	return ir.Document{
		Frontmatter: ir.Frontmatter{Title: title},
		Sections: []ir.Section{
			{Heading: "Overview", Blocks: []ir.Block{ir.ProseBlock{Text: "body"}}},
			{Heading: "Operational Runbooks", Blocks: []ir.Block{ir.IndexBlock{
				Prov: ir.Provenance{SpecSection: architecture.ChildIndexProv},
			}}},
		},
	}
}

func vaultSpec() docspec.DocSpec {
	return docspec.DocSpec{
		Topic: "Vault",
		Parent: docspec.DocPage{
			Page:       "OptiscanGroup/Azure_Infrastructure/Vault_Architecture",
			Kind:       "architecture",
			Related:    []string{"OptiscanGroup/Azure_Infrastructure/Docker_Swarm"},
			Categories: []string{"Azure Infrastructure", "Vault"},
		},
		Children: []docspec.DocPage{
			{Page: "OptiscanGroup/Azure_Infrastructure/Vault_Operations", Kind: "runbook", Intent: "Day-2 ops."},
			{Page: "OptiscanGroup/Azure_Infrastructure/Vault_Reference", Kind: "reference", Intent: "LLM dump."},
		},
	}
}

func TestRender_FillsParentChildIndexInPlace(t *testing.T) {
	spec := vaultSpec()
	st := &stubRenderer{docs: map[string]ir.Document{
		spec.Parent.Page:      placeholderDoc(spec.Parent.Page),
		spec.Children[0].Page: {Sections: []ir.Section{{Heading: "Ops", Blocks: []ir.Block{ir.ProseBlock{Text: "x"}}}}},
		spec.Children[1].Page: {Sections: []ir.Section{{Heading: "Ref", Blocks: []ir.Block{ir.ProseBlock{Text: "y"}}}}},
	}}

	pages, err := New(st).Render(context.Background(), spec, kb.Snapshot{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(pages) != 3 || pages[0].Page != spec.Parent.Page {
		t.Fatalf("parent must come first, got %d pages: %+v", len(pages), pages)
	}

	// child-index placeholder filled, IN PLACE (still section index 1).
	idxSec := pages[0].Doc.Sections[1]
	if idxSec.Heading != "Operational Runbooks" {
		t.Fatalf("placeholder section moved/renamed: %q", idxSec.Heading)
	}
	ib := idxSec.Blocks[0].(ir.IndexBlock)
	if len(ib.Entries) != 2 {
		t.Fatalf("child-index not filled: %+v", ib.Entries)
	}
	if ib.Entries[0].Page != spec.Children[0].Page ||
		ib.Entries[0].Label != "Vault Operations" ||
		ib.Entries[0].Desc != "Day-2 ops." {
		t.Errorf("child entry 0 wrong: %+v", ib.Entries[0])
	}
	if ib.Entries[1].Label != "Vault Reference" {
		t.Errorf("child entry 1 label wrong: %+v", ib.Entries[1])
	}
}

func TestRender_ChildrenGetPartOfBacklink(t *testing.T) {
	spec := vaultSpec()
	st := &stubRenderer{docs: map[string]ir.Document{
		spec.Parent.Page:      placeholderDoc(spec.Parent.Page),
		spec.Children[0].Page: {Sections: []ir.Section{{Heading: "Ops", Blocks: []ir.Block{ir.ProseBlock{Text: "x"}}}}},
		spec.Children[1].Page: {Sections: []ir.Section{{Heading: "Ref", Blocks: []ir.Block{ir.ProseBlock{Text: "y"}}}}},
	}}
	pages, err := New(st).Render(context.Background(), spec, kb.Snapshot{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	child := pages[1]
	if child.Doc.Sections[0].Heading != "Part of" {
		t.Fatalf("child must lead with a Part of backlink: %+v", child.Doc.Sections[0])
	}
	ib := child.Doc.Sections[0].Blocks[0].(ir.IndexBlock)
	if len(ib.Entries) != 1 || ib.Entries[0].Page != spec.Parent.Page || ib.Entries[0].Label != "Vault" {
		t.Errorf("Part of backlink wrong: %+v", ib.Entries)
	}
	// parent itself gets NO Part of section.
	if pages[0].Doc.Sections[0].Heading == "Part of" {
		t.Error("parent must not have a Part of backlink")
	}
}

func TestRender_RelatedAndCategoriesAppended(t *testing.T) {
	spec := vaultSpec()
	st := &stubRenderer{docs: map[string]ir.Document{
		spec.Parent.Page:      placeholderDoc(spec.Parent.Page),
		spec.Children[0].Page: {Sections: []ir.Section{{Heading: "Ops"}}},
		spec.Children[1].Page: {Sections: []ir.Section{{Heading: "Ref"}}},
	}}
	pages, err := New(st).Render(context.Background(), spec, kb.Snapshot{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	secs := pages[0].Doc.Sections
	rel := secs[len(secs)-2]
	cat := secs[len(secs)-1]
	if rel.Heading != "Related pages" {
		t.Fatalf("expected trailing Related pages section, got %q", rel.Heading)
	}
	rib := rel.Blocks[0].(ir.IndexBlock)
	if len(rib.Entries) != 1 || rib.Entries[0].Label != "Docker Swarm" {
		t.Errorf("related entry wrong: %+v", rib.Entries)
	}
	if cat.Heading != "" {
		t.Errorf("categories block must be a no-heading trailing section, got %q", cat.Heading)
	}
	cb, ok := cat.Blocks[0].(ir.CategoryBlock)
	if !ok || len(cb.Names) != 2 || cb.Names[0] != "Azure Infrastructure" {
		t.Errorf("categories wrong: %+v", cat.Blocks)
	}

	// a child with neither related nor categories gets neither.
	for _, s := range pages[1].Doc.Sections {
		if s.Heading == "Related pages" {
			t.Error("child without related must not get a Related pages section")
		}
		for _, b := range s.Blocks {
			if _, isCat := b.(ir.CategoryBlock); isCat {
				t.Error("child without categories must not get a CategoryBlock")
			}
		}
	}
}

func TestRender_PropagatesRendererError(t *testing.T) {
	st := &stubRenderer{err: errors.New("boom")}
	if _, err := New(st).Render(context.Background(), vaultSpec(), kb.Snapshot{}); err == nil {
		t.Fatal("renderer error must abort the whole cluster")
	}
}

func TestPageLabel(t *testing.T) {
	cases := map[string]string{
		"OptiscanGroup/Azure_Infrastructure/Vault_Operations": "Vault Operations",
		"Flat_Page":    "Flat Page",
		"NoUnderscore": "NoUnderscore",
	}
	for in, want := range cases {
		if got := pageLabel(in); got != want {
			t.Errorf("pageLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
