package hub_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/hub"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

func hubSpec() specs.Spec {
	return specs.Spec{
		ID: "h.spec.md", Wiki: "mykb", Page: "OptiscanGroup/Azure_Infrastructure",
		Kind: "hub", Hash: "spec-h",
		Body: "Index of the Azure infrastructure.",
		Hub: &specs.HubSpec{Sections: []specs.HubSection{
			{Title: "Core Infrastructure", Links: []specs.HubLink{
				{Page: "OptiscanGroup/Azure_Infrastructure/Networking", Label: "Networking", Desc: "Hub-and-spoke + WG S2S"},
				{Page: "OptiscanGroup/Azure_Infrastructure/Vault", Label: "Vault", Area: "vault"},
				{Page: "OptiscanGroup/Azure_Infrastructure/Harbor", Label: "Harbor", Area: "vault", Desc: "explicit wins"},
			}},
			{Title: "Operations", Links: []specs.HubLink{
				{Page: "OptiscanGroup/Azure_Infrastructure/DR"},
			}},
		}},
	}
}

func snapWithVault() kb.Snapshot {
	return kb.Snapshot{Commit: "kbc", Areas: []kb.Area{
		{ID: "vault", Name: "Vault", Summary: "Centralised secrets manager on Raft HA"},
	}}
}

func TestNameAndKind(t *testing.T) {
	f := hub.New()
	if f.Name() != "hub-frontend" || f.Kind() != "hub" {
		t.Errorf("Name/Kind = %q/%q", f.Name(), f.Kind())
	}
}

func TestBuild_StructureAndKBDescFallback(t *testing.T) {
	doc, err := hub.New().Build(context.Background(), hubSpec(), snapWithVault())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.Frontmatter.Title != "OptiscanGroup/Azure_Infrastructure" ||
		doc.Frontmatter.SpecHash != "spec-h" || doc.Frontmatter.KBCommit != "kbc" {
		t.Errorf("frontmatter wrong: %+v", doc.Frontmatter)
	}
	// intro + 2 hub sections
	if len(doc.Sections) != 3 {
		t.Fatalf("sections = %d, want 3 (intro + 2)", len(doc.Sections))
	}
	if _, ok := doc.Sections[0].Blocks[0].(ir.ProseBlock); !ok {
		t.Errorf("section 0 should be the intro ProseBlock, got %T", doc.Sections[0].Blocks[0])
	}
	core := doc.Sections[1]
	if core.Heading != "Core Infrastructure" {
		t.Errorf("heading = %q", core.Heading)
	}
	ib, ok := core.Blocks[0].(ir.IndexBlock)
	if !ok {
		t.Fatalf("expected IndexBlock, got %T", core.Blocks[0])
	}
	if len(ib.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(ib.Entries))
	}
	// explicit desc preserved
	if ib.Entries[0].Desc != "Hub-and-spoke + WG S2S" {
		t.Errorf("entry0 desc = %q", ib.Entries[0].Desc)
	}
	// area fallback: no desc + area=vault → area summary
	if ib.Entries[1].Desc != "Centralised secrets manager on Raft HA" {
		t.Errorf("entry1 desc should fall back to kb area summary, got %q", ib.Entries[1].Desc)
	}
	// explicit desc beats area fallback
	if ib.Entries[2].Desc != "explicit wins" {
		t.Errorf("entry2 desc = %q, want explicit", ib.Entries[2].Desc)
	}
	// kb-sourced desc records provenance
	if !reflect.DeepEqual(ib.Prov.Sources, []string{"area/vault"}) {
		t.Errorf("Sources = %v, want [area/vault]", ib.Prov.Sources)
	}
	// IndexBlock is machine-owned (navigation always regenerated)
	if ib.Zone() != ir.ZoneMachine {
		t.Errorf("IndexBlock zone = %v, want machine", ib.Zone())
	}
}

func TestBuild_AreaMissingFromSnapshot_NoCrash_NoDesc(t *testing.T) {
	doc, err := hub.New().Build(context.Background(), hubSpec(), kb.Snapshot{Commit: "x"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ib := doc.Sections[1].Blocks[0].(ir.IndexBlock)
	if ib.Entries[1].Desc != "" {
		t.Errorf("area absent from snapshot ⇒ empty desc, got %q", ib.Entries[1].Desc)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	a, _ := hub.New().Build(context.Background(), hubSpec(), snapWithVault())
	b, _ := hub.New().Build(context.Background(), hubSpec(), snapWithVault())
	if !reflect.DeepEqual(a, b) {
		t.Errorf("hub frontend not deterministic")
	}
}

func TestBuild_NilHub_Errors(t *testing.T) {
	_, err := hub.New().Build(context.Background(),
		specs.Spec{ID: "x", Kind: "hub"}, kb.Snapshot{})
	if err == nil {
		t.Errorf("kind=hub with no hub structure must error")
	}
}
