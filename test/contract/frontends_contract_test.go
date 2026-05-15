//go:build contract

package contract_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
)

var allFrontends = map[string]frontends.Frontend{
	"projection": projection.New(),
	// "editorial": editorial.New(...), // lands v0.5 with LLM client
}

func TestFrontend_Contract(t *testing.T) {
	for name, f := range allFrontends {
		t.Run(name, func(t *testing.T) {
			FrontendContractSuite(t, f)
		})
	}
}

// FrontendContractSuite asserts behavioural properties every Frontend
// must satisfy regardless of strategy. Editorial frontends (LLM-
// backed) are expected to satisfy the same contract via a replay LLM.
func FrontendContractSuite(t *testing.T, f frontends.Frontend) {
	t.Helper()

	t.Run("Name + Kind are non-empty and stable", func(t *testing.T) {
		if f.Name() == "" || f.Kind() == "" {
			t.Errorf("Name=%q Kind=%q — both must be non-empty", f.Name(), f.Kind())
		}
		if f.Name() != f.Name() || f.Kind() != f.Kind() {
			t.Errorf("Name/Kind not stable")
		}
	})

	t.Run("Build with empty include + empty snapshot does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic on empty inputs: %v", r)
			}
		}()
		_, _ = f.Build(context.Background(), specs.Spec{Wiki: "acme", Kind: f.Kind()}, kb.Snapshot{})
	})

	t.Run("Build copies spec hash into frontmatter", func(t *testing.T) {
		spec := specs.Spec{
			Wiki: "acme", Page: "P", Kind: f.Kind(), Hash: "h-unique-9999",
			Include: specs.IncludeFilter{Areas: []string{"vault"}},
		}
		snap := kb.Snapshot{Commit: "kb-h", Areas: []kb.Area{{ID: "vault", Name: "Vault"}}}
		doc, err := f.Build(context.Background(), spec, snap)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if doc.Frontmatter.SpecHash != "h-unique-9999" {
			t.Errorf("SpecHash = %q, want %q", doc.Frontmatter.SpecHash, "h-unique-9999")
		}
	})

	t.Run("Build references spec.Page somewhere in document", func(t *testing.T) {
		spec := specs.Spec{
			Wiki: "acme", Page: "FrontendContractPage_Unique12345", Kind: f.Kind(),
			Include: specs.IncludeFilter{Areas: []string{"vault"}},
		}
		snap := kb.Snapshot{Commit: "kb-h", Areas: []kb.Area{{ID: "vault", Name: "Vault"}}}
		doc, err := f.Build(context.Background(), spec, snap)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !strings.Contains(doc.Frontmatter.Title, spec.Page) {
			t.Errorf("Title %q does not contain spec.Page %q", doc.Frontmatter.Title, spec.Page)
		}
	})

	t.Run("Build is deterministic for same inputs", func(t *testing.T) {
		spec := specs.Spec{
			Wiki: "acme", Page: "P", Kind: f.Kind(), Hash: "h",
			Include: specs.IncludeFilter{Areas: []string{"vault"}},
		}
		snap := kb.Snapshot{Commit: "kb-h", Areas: []kb.Area{{
			ID: "vault", Name: "Vault",
			Entries: []kb.Entry{
				{ID: "f1", Type: "fact", Text: "alpha"},
				{ID: "f2", Type: "fact", Text: "beta"},
			},
		}}}
		a, errA := f.Build(context.Background(), spec, snap)
		b, errB := f.Build(context.Background(), spec, snap)
		if errA != nil || errB != nil {
			t.Fatalf("Build: %v / %v", errA, errB)
		}
		if len(a.Sections) != len(b.Sections) {
			t.Errorf("section count differs: %d vs %d", len(a.Sections), len(b.Sections))
		}
		for i := range a.Sections {
			if a.Sections[i].Heading != b.Sections[i].Heading {
				t.Errorf("section[%d].Heading differs", i)
			}
		}
	})
}
