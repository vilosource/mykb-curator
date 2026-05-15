//go:build contract

// Package contract_test holds pyramid level 3 tests — shared
// contract suites that every implementation of an interface must
// satisfy.
//
// This file is the BackendContractSuite. Every Backend impl is
// registered in `allBackends` and exercised through the same
// behavioural assertions. Adding a backend = adding one line to
// allBackends + ensuring it passes the suite. LSP enforcement by
// construction.
package contract_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// allBackends is the registry of every Backend impl. Each entry must
// pass BackendContractSuite below.
var allBackends = map[string]backends.Backend{
	"markdown": markdown.New(),
	// "mediawiki": mediawiki.New(),   // lands v0.1
	// "confluence": confluence.New(), // lands v2+
}

func TestBackend_Contract(t *testing.T) {
	for name, b := range allBackends {
		t.Run(name, func(t *testing.T) {
			BackendContractSuite(t, b)
		})
	}
}

// BackendContractSuite asserts behavioural properties every backend
// must satisfy regardless of target format. The properties are
// derived from the interface's documented contract (purity,
// determinism, handling of all IR block kinds).
func BackendContractSuite(t *testing.T, b backends.Backend) {
	t.Helper()

	t.Run("Name is non-empty and stable", func(t *testing.T) {
		n := b.Name()
		if n == "" {
			t.Errorf("Name() is empty")
		}
		if n != b.Name() {
			t.Errorf("Name() is not stable across calls")
		}
	})

	t.Run("Render is deterministic", func(t *testing.T) {
		doc := contractSampleDoc()
		a, errA := b.Render(doc)
		if errA != nil {
			t.Fatalf("Render: %v", errA)
		}
		c, errC := b.Render(doc)
		if errC != nil {
			t.Fatalf("Render: %v", errC)
		}
		if !bytes.Equal(a, c) {
			t.Errorf("Render is non-deterministic for the same input")
		}
	})

	t.Run("Render handles empty document without panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic on empty document: %v", r)
			}
		}()
		_, err := b.Render(ir.Document{})
		if err != nil {
			t.Errorf("Render(empty): %v", err)
		}
	})

	t.Run("Render produces non-empty output for non-trivial document", func(t *testing.T) {
		out, err := b.Render(contractSampleDoc())
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if len(out) == 0 {
			t.Errorf("Render produced empty output for non-trivial document")
		}
	})

	t.Run("Render includes document title somewhere in output", func(t *testing.T) {
		title := "ContractSuiteTitle-Unique-12345"
		doc := ir.Document{Frontmatter: ir.Frontmatter{Title: title}}
		out, err := b.Render(doc)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if !strings.Contains(string(out), title) {
			t.Errorf("output does not contain document title %q\n%s", title, out)
		}
	})

	t.Run("Render handles every IR block kind without erroring", func(t *testing.T) {
		// Every backend must handle every block kind. Unknown blocks
		// may render as placeholders; they may not return errors or
		// panic.
		doc := ir.Document{Sections: []ir.Section{{
			Heading: "AllBlocks",
			Blocks: []ir.Block{
				ir.ProseBlock{Text: "prose"},
				ir.MachineBlock{BlockID: "m1", Body: "body", Prov: ir.Provenance{InputHash: "h"}},
				ir.KBRefBlock{Area: "vault", ID: "fact:x"},
				ir.TableBlock{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "2"}}},
				ir.DiagramBlock{Lang: "mermaid", Source: "graph TD; A-->B;"},
				ir.Callout{Severity: "note", Body: "callout"},
				ir.EscapeHatch{Backend: b.Name(), Raw: "raw"},
			},
		}}}
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic rendering full block taxonomy: %v", r)
			}
		}()
		if _, err := b.Render(doc); err != nil {
			t.Errorf("Render failed on full block taxonomy: %v", err)
		}
	})

	t.Run("EscapeHatch is scoped to the matching backend", func(t *testing.T) {
		// Backend X must not leak content from EscapeHatch{Backend:"Y"}.
		needle := "OTHER-BACKEND-RAW-CONTENT-DO-NOT-LEAK"
		doc := ir.Document{Sections: []ir.Section{{
			Blocks: []ir.Block{
				ir.EscapeHatch{Backend: "_other_backend_that_does_not_exist", Raw: needle},
			},
		}}}
		out, err := b.Render(doc)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if strings.Contains(string(out), needle) {
			t.Errorf("backend %q leaked content from foreign EscapeHatch:\n%s", b.Name(), out)
		}
	})
}

func contractSampleDoc() ir.Document {
	ts := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	return ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:       "ContractSuiteDoc",
			SpecHash:    "spec-h",
			KBCommit:    "kb-h",
			GeneratedAt: ts,
		},
		Sections: []ir.Section{
			{Heading: "S1", Blocks: []ir.Block{ir.ProseBlock{Text: "prose"}}},
		},
		Footer: ir.Footer{RunID: "r1", KBCommit: "kb-h", LastCurated: ts},
	}
}
