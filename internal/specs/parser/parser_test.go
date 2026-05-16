package parser

import (
	"reflect"
	"strings"
	"testing"
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
  exclude_zones: [incoming, archived]
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
	if got := spec.Include.ExcludeZones; !reflect.DeepEqual(got, []string{"incoming", "archived"}) {
		t.Errorf("ExcludeZones = %v", got)
	}
	wantFC := map[string]string{"link_rot": "every-run", "external_truth": "quarterly"}
	if !reflect.DeepEqual(spec.FactCheck, wantFC) {
		t.Errorf("FactCheck = %v, want %v", spec.FactCheck, wantFC)
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
