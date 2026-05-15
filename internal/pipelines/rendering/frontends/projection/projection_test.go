package projection

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

var _ frontends.Frontend = (*Frontend)(nil)

func TestNameAndKind(t *testing.T) {
	f := New()
	if f.Kind() != "projection" {
		t.Errorf("Kind = %q, want %q", f.Kind(), "projection")
	}
	if f.Name() == "" {
		t.Errorf("Name is empty")
	}
}

func TestBuild_SpecHashCopiedIntoFrontmatter(t *testing.T) {
	spec := specs.Spec{
		ID:   "page-a",
		Wiki: "acme",
		Page: "Vault_Architecture",
		Kind: "projection",
		Hash: "spec-hash-abc",
		Include: specs.IncludeFilter{
			Areas: []string{"vault"},
		},
	}
	snap := kb.Snapshot{
		Commit: "kb-commit-def",
		Areas: []kb.Area{
			{ID: "vault", Name: "Vault Architecture", Summary: "secrets mgr"},
		},
	}
	doc, err := New().Build(context.Background(), spec, snap)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.Frontmatter.Title != "Vault_Architecture" {
		t.Errorf("Title = %q, want %q", doc.Frontmatter.Title, "Vault_Architecture")
	}
	if doc.Frontmatter.SpecHash != "spec-hash-abc" {
		t.Errorf("SpecHash = %q, want %q", doc.Frontmatter.SpecHash, "spec-hash-abc")
	}
	if doc.Frontmatter.KBCommit != "kb-commit-def" {
		t.Errorf("KBCommit = %q, want %q", doc.Frontmatter.KBCommit, "kb-commit-def")
	}
}

func TestBuild_ProducesSectionPerEntryType(t *testing.T) {
	spec := specs.Spec{
		ID: "vault", Wiki: "acme", Page: "Vault", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID:      "vault",
		Name:    "Vault",
		Summary: "secrets",
		Entries: []kb.Entry{
			{ID: "f1", Type: "fact", Text: "fact one", Zone: "active"},
			{ID: "d1", Type: "decision", Text: "decision one", Why: "because", Zone: "active"},
			{ID: "g1", Type: "gotcha", Text: "watch out", Zone: "active"},
		},
	}}}
	doc, _ := New().Build(context.Background(), spec, snap)

	wantHeadings := []string{"Vault", "Facts", "Decisions", "Gotchas"}
	gotHeadings := []string{}
	for _, sec := range doc.Sections {
		gotHeadings = append(gotHeadings, sec.Heading)
	}
	for _, w := range wantHeadings {
		found := false
		for _, g := range gotHeadings {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing section heading %q; got %v", w, gotHeadings)
		}
	}
}

func TestBuild_EntryTextLandsInProseBlocks(t *testing.T) {
	spec := specs.Spec{
		ID: "vault", Wiki: "acme", Page: "Vault", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "f1", Type: "fact", Text: "Vault HA on Raft", Zone: "active"},
		},
	}}}
	doc, _ := New().Build(context.Background(), spec, snap)

	got := serializeProseBlocks(doc)
	if !strings.Contains(got, "Vault HA on Raft") {
		t.Errorf("entry text not in prose blocks:\n%s", got)
	}
}

func TestBuild_DecisionRendersWithWhyAndRejected(t *testing.T) {
	spec := specs.Spec{
		ID: "vault", Wiki: "acme", Page: "Vault", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID: "vault",
		Entries: []kb.Entry{
			{ID: "d1", Type: "decision", Text: "DEC-001", Why: "for reasons", Rejected: "the alt"},
		},
	}}}
	doc, _ := New().Build(context.Background(), spec, snap)

	got := serializeProseBlocks(doc)
	if !strings.Contains(got, "DEC-001") {
		t.Errorf("decision text missing: %q", got)
	}
	if !strings.Contains(got, "for reasons") {
		t.Errorf("decision Why missing: %q", got)
	}
	if !strings.Contains(got, "the alt") {
		t.Errorf("decision Rejected missing: %q", got)
	}
}

func TestBuild_ExcludesAreasNotInSpecInclude(t *testing.T) {
	spec := specs.Spec{
		ID: "vault-only", Wiki: "acme", Page: "X", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{
		{ID: "vault", Name: "Vault", Entries: []kb.Entry{{Type: "fact", Text: "vault fact"}}},
		{ID: "harbor", Name: "Harbor", Entries: []kb.Entry{{Type: "fact", Text: "harbor fact"}}},
	}}
	doc, _ := New().Build(context.Background(), spec, snap)

	got := serializeProseBlocks(doc)
	if strings.Contains(got, "harbor fact") {
		t.Errorf("harbor content leaked into vault-only spec:\n%s", got)
	}
}

func TestBuild_MissingArea_IsError(t *testing.T) {
	spec := specs.Spec{
		ID: "x", Wiki: "acme", Page: "P", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"nonexistent"}},
	}
	_, err := New().Build(context.Background(), spec, kb.Snapshot{})
	if err == nil {
		t.Errorf("expected error for area not in snapshot, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("err = %v, want mention of missing area id", err)
	}
}

func TestBuild_EmptyArea_NoEntries_ProducesHeadingsOnly(t *testing.T) {
	spec := specs.Spec{
		ID: "x", Wiki: "acme", Page: "Empty", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"empty"}},
	}
	snap := kb.Snapshot{Areas: []kb.Area{
		{ID: "empty", Name: "Empty Area", Summary: ""},
	}}
	doc, err := New().Build(context.Background(), spec, snap)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Top section (the area header) should still exist; entry-type
	// subsections may be omitted since there are no entries.
	if len(doc.Sections) == 0 {
		t.Errorf("expected at least the area header section")
	}
}

func TestBuild_Deterministic_SameInputsSameOutput(t *testing.T) {
	spec := specs.Spec{
		ID: "x", Wiki: "acme", Page: "P", Kind: "projection",
		Include: specs.IncludeFilter{Areas: []string{"vault"}},
		Hash:    "h",
	}
	snap := kb.Snapshot{Areas: []kb.Area{{
		ID:   "vault",
		Name: "Vault",
		Entries: []kb.Entry{
			{ID: "f1", Type: "fact", Text: "one"},
			{ID: "f2", Type: "fact", Text: "two"},
		},
	}}}

	a, _ := New().Build(context.Background(), spec, snap)
	b, _ := New().Build(context.Background(), spec, snap)

	if serializeProseBlocks(a) != serializeProseBlocks(b) {
		t.Errorf("non-deterministic output for identical inputs")
	}
}

// serializeProseBlocks concatenates the .Text of every ProseBlock in
// the document. Used to assert content reaches the IR without
// committing to a specific section/block structure beyond headings.
func serializeProseBlocks(doc ir.Document) string {
	var sb strings.Builder
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			if p, ok := b.(ir.ProseBlock); ok {
				sb.WriteString(p.Text)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}
