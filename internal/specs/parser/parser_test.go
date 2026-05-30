package parser

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/specs"
)

func TestParse_MinimalValidSpec(t *testing.T) {
	body := `---
wiki: acme
page: Vault_Architecture
kind: projection
version: 1
include:
  areas: [vault]
---

This page is the projection of the vault area.
`
	spec, err := Parse("vault-area.spec.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if spec.ID != "vault-area.spec.md" {
		t.Errorf("ID = %q, want %q", spec.ID, "vault-area.spec.md")
	}
	if spec.Wiki != "acme" {
		t.Errorf("Wiki = %q, want %q", spec.Wiki, "acme")
	}
	if spec.Page != "Vault_Architecture" {
		t.Errorf("Page = %q, want %q", spec.Page, "Vault_Architecture")
	}
	if spec.Kind != "projection" {
		t.Errorf("Kind = %q, want %q", spec.Kind, "projection")
	}
	if !reflect.DeepEqual(spec.Include.Areas, []string{"vault"}) {
		t.Errorf("Include.Areas = %v, want [vault]", spec.Include.Areas)
	}
	if !strings.Contains(spec.Body, "projection of the vault area") {
		t.Errorf("Body missing markdown content: %q", spec.Body)
	}
	if spec.Hash == "" {
		t.Errorf("Hash should be non-empty")
	}
}

func TestParse_RichSpec(t *testing.T) {
	body := `---
wiki: acme
page: Azure_Infrastructure
kind: editorial
version: 1
include:
  areas: [networking, vault, harbor]
  workspaces: [dr, hetzner]
  exclude_zones: [incoming, archive]
fact_check:
  link_rot: every-run
  external_truth: quarterly
protected_blocks: [executive-summary]
---

Cover all the Azure infrastructure topics.
`
	spec, err := Parse("hub.spec.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := spec.Include.Workspaces; !reflect.DeepEqual(got, []string{"dr", "hetzner"}) {
		t.Errorf("Workspaces = %v", got)
	}
	if got := spec.Include.ExcludeZones; !reflect.DeepEqual(got, []string{"incoming", "archive"}) {
		t.Errorf("ExcludeZones = %v", got)
	}
	wantFC := map[string]string{"link_rot": "every-run", "external_truth": "quarterly"}
	if !reflect.DeepEqual(spec.FactCheck, wantFC) {
		t.Errorf("FactCheck = %v, want %v", spec.FactCheck, wantFC)
	}
}

func TestParse_HubSpec(t *testing.T) {
	body := `---
wiki: mykb
page: OptiscanGroup/Azure_Infrastructure
kind: hub
hub:
  sections:
    - title: Core Infrastructure
      links:
        - page: OptiscanGroup/Azure_Infrastructure/Networking
          label: Networking
          desc: Hub-and-spoke + WG S2S
        - page: OptiscanGroup/Azure_Infrastructure/Vault
          area: vault
    - title: Operations
      links:
        - page: OptiscanGroup/Azure_Infrastructure/DR
---

Index of the Azure infrastructure.
`
	spec, err := Parse("hub.spec.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if spec.Kind != "hub" || spec.Hub == nil {
		t.Fatalf("hub not parsed: kind=%q hub=%v", spec.Kind, spec.Hub)
	}
	if len(spec.Hub.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(spec.Hub.Sections))
	}
	s0 := spec.Hub.Sections[0]
	if s0.Title != "Core Infrastructure" || len(s0.Links) != 2 {
		t.Fatalf("section0 wrong: %+v", s0)
	}
	want := specs.HubLink{Page: "OptiscanGroup/Azure_Infrastructure/Networking", Label: "Networking", Desc: "Hub-and-spoke + WG S2S"}
	if !reflect.DeepEqual(s0.Links[0], want) {
		t.Errorf("link0 = %+v, want %+v", s0.Links[0], want)
	}
	if s0.Links[1].Area != "vault" {
		t.Errorf("link1.Area = %q, want vault", s0.Links[1].Area)
	}
}

func TestParse_HubValidation(t *testing.T) {
	cases := map[string]string{
		"hub kind, no hub block": "wiki: m\npage: P\nkind: hub\n",
		"hub, empty sections":    "wiki: m\npage: P\nkind: hub\nhub:\n  sections: []\n",
		"section with no links":  "wiki: m\npage: P\nkind: hub\nhub:\n  sections:\n    - title: T\n",
		"link missing page":      "wiki: m\npage: P\nkind: hub\nhub:\n  sections:\n    - title: T\n      links:\n        - label: x\n",
	}
	for name, fm := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("x.spec.md", []byte("---\n"+fm+"---\n")); err == nil {
				t.Errorf("expected validation error for %s", name)
			}
		})
	}
	// A non-hub spec must not require a hub block.
	if _, err := Parse("p.spec.md", []byte("---\nwiki: m\npage: P\nkind: projection\n---\n")); err != nil {
		t.Errorf("non-hub spec should not require hub: %v", err)
	}
}

func TestParse_HashStableAndDifferentiating(t *testing.T) {
	a := []byte("---\nwiki: acme\npage: P\nkind: projection\n---\nbody1\n")
	b := []byte("---\nwiki: acme\npage: P\nkind: projection\n---\nbody1\n")
	c := []byte("---\nwiki: acme\npage: P\nkind: projection\n---\nbody2\n")

	sa, _ := Parse("a.spec.md", a)
	sb, _ := Parse("a.spec.md", b)
	sc, _ := Parse("a.spec.md", c)

	if sa.Hash != sb.Hash {
		t.Errorf("identical content hashed differently: %s vs %s", sa.Hash, sb.Hash)
	}
	if sa.Hash == sc.Hash {
		t.Errorf("different content hashed the same: %s", sa.Hash)
	}
}

func TestParse_WorkspacesAsSentinelString(t *testing.T) {
	// DESIGN.md §7.1 shows `workspaces: linked-to-areas` as a sentinel
	// meaning "all workspaces linked to any area in include.areas".
	// The parser must accept both a string sentinel and a list of ids.
	body := `---
wiki: acme
page: P
kind: projection
include:
  workspaces: linked-to-areas
---
`
	spec, err := Parse("x.spec.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"linked-to-areas"}
	if !reflect.DeepEqual(spec.Include.Workspaces, want) {
		t.Errorf("Workspaces = %v, want %v (sentinel preserved as single-element list)", spec.Include.Workspaces, want)
	}
}

func TestParse_RejectsMissingFrontmatter(t *testing.T) {
	body := `just markdown, no frontmatter`
	if _, err := Parse("x.spec.md", []byte(body)); err == nil {
		t.Errorf("expected error for missing frontmatter, got nil")
	}
}

func TestParse_RejectsRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no wiki", "---\npage: P\nkind: projection\n---\n"},
		{"no page", "---\nwiki: acme\nkind: projection\n---\n"},
		{"no kind", "---\nwiki: acme\npage: P\n---\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse("x.spec.md", []byte(tc.body)); err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}

func TestParse_RejectsUnknownKind(t *testing.T) {
	body := `---
wiki: acme
page: P
kind: completely-made-up
---
`
	if _, err := Parse("x.spec.md", []byte(body)); err == nil {
		t.Errorf("expected error for unknown kind, got nil")
	}
}

func TestParse_RejectsMalformedFrontmatter(t *testing.T) {
	body := "---\nwiki: : : :\nbroken yaml\n---\n"
	if _, err := Parse("x.spec.md", []byte(body)); err == nil {
		t.Errorf("expected YAML parse error, got nil")
	}
}

// A `nav` frontmatter block parses into Spec.Nav (declared placement;
// resolution happens later when the nav map is built).
func TestParse_NavPlacement(t *testing.T) {
	body := `---
wiki: personal
page: OptiscanGroup/Azure_Infrastructure/Vault
kind: projection
nav:
  parent: OptiscanGroup/Azure_Infrastructure
  section: Infrastructure Service Stacks
  order: 40
  label: Vault & Secrets
  blurb: The HashiCorp Vault secrets backend.
---
body`
	spec, err := Parse("leaf-vault.spec.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	n := spec.Nav
	if n.Parent != "OptiscanGroup/Azure_Infrastructure" || n.Section != "Infrastructure Service Stacks" ||
		n.Order != 40 || n.Label != "Vault & Secrets" || n.Blurb != "The HashiCorp Vault secrets backend." {
		t.Errorf("nav not parsed: %+v", n)
	}
}

// A spec with no `nav` block yields a zero placement (resolved from the
// title later).
func TestParse_NoNavIsZero(t *testing.T) {
	body := `---
wiki: personal
page: Some/Page
kind: projection
---
body`
	spec, err := Parse("x.spec.md", []byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if spec.Nav.Parent != "" || spec.Nav.Label != "" || spec.Nav.Order != 0 {
		t.Errorf("absent nav should be zero, got %+v", spec.Nav)
	}
}

// A members:auto hub may declare sections with no links (membership is
// auto-derived) — validation must accept it, where a non-auto hub with
// an empty-link section still fails.
func TestParse_AutoHubAllowsEmptySections(t *testing.T) {
	auto := `---
wiki: personal
page: OptiscanGroup/Azure_Infrastructure
kind: hub
hub:
  members: auto
  sections:
    - {title: Core, desc: "the base"}
---
body`
	if _, err := Parse("azi.spec.md", []byte(auto)); err != nil {
		t.Errorf("members:auto hub with link-less sections must parse, got: %v", err)
	}

	notAuto := `---
wiki: personal
page: H
kind: hub
hub:
  sections:
    - {title: Core}
---
body`
	if _, err := Parse("h.spec.md", []byte(notAuto)); err == nil {
		t.Errorf("non-auto hub with a link-less section must still fail")
	}
}
