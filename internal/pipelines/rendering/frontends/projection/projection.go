// Package projection implements a deterministic Frontend that
// renders an area (plus its linked workspaces — TBD v0.5) as a
// structured Document: area header, then one section per entry
// type, listing every entry as a ProseBlock.
//
// No LLM involved. No I/O beyond the kb snapshot already in memory.
// Pure function of (spec, snapshot) → ir.Document — same inputs
// give byte-identical output, every time.
//
// This is the simplest useful frontend: it gives every area a
// dedicated wiki page that's a faithful synthesis of its entries.
// More polished editorial pages live in EditorialFrontend (v0.5).
package projection

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Frontend is the projection-mode frontend.
type Frontend struct{}

// New constructs a Frontend.
func New() *Frontend { return &Frontend{} }

// Name returns "projection-frontend".
func (*Frontend) Name() string { return "projection-frontend" }

// Kind returns "projection" — the spec.Kind value this frontend handles.
func (*Frontend) Kind() string { return "projection" }

// entryTypes is the canonical order entry-type sections appear in.
// Fixing the order is what makes the frontend deterministic (no map
// iteration affecting output).
var entryTypes = []struct {
	tag     string
	heading string
}{
	{"fact", "Facts"},
	{"decision", "Decisions"},
	{"gotcha", "Gotchas"},
	{"pattern", "Patterns"},
	{"link", "Links"},
}

// Build produces the IR Document from spec + kb snapshot.
func (*Frontend) Build(_ context.Context, spec specs.Spec, snap kb.Snapshot) (ir.Document, error) {
	doc := ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:    spec.Page,
			SpecHash: spec.Hash,
			KBCommit: snap.Commit,
		},
	}

	for _, areaID := range spec.Include.Areas {
		a := snap.Area(areaID)
		if a == nil {
			return ir.Document{}, fmt.Errorf("projection: area %q from spec include list not present in kb snapshot", areaID)
		}
		doc.Sections = append(doc.Sections, buildAreaSections(a, spec)...)
	}

	return doc, nil
}

// buildAreaSections produces the section sequence for one area:
// header (area name + summary), then one section per entry type that
// actually has entries.
func buildAreaSections(a *kb.Area, spec specs.Spec) []ir.Section {
	var out []ir.Section

	// Area header section. Always present so every projected area has
	// at least one visible section.
	header := ir.Section{
		Heading: areaHeading(a),
		Blocks:  []ir.Block{ir.ProseBlock{Text: a.Summary, Prov: ir.Provenance{SpecSection: "area-header", Sources: []string{"area/" + a.ID}}}},
	}
	out = append(out, header)

	// One section per entry type with content. Skip empty types so
	// the projection doesn't render empty section headings.
	for _, et := range entryTypes {
		entries := a.EntriesByType(et.tag)
		if len(entries) == 0 {
			continue
		}
		sec := ir.Section{Heading: et.heading}
		for _, e := range entries {
			sec.Blocks = append(sec.Blocks, entryToBlock(e, spec))
		}
		out = append(out, sec)
	}

	return out
}

// areaHeading is the heading shown for an area's lead section.
// Name preferred; falls back to ID.
func areaHeading(a *kb.Area) string {
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}

// entryToBlock renders one entry as a ProseBlock with provenance
// pointing at the kb-ref. Decision entries get Why / Rejected /
// Context inlined; link entries get the URL appended.
func entryToBlock(e kb.Entry, spec specs.Spec) ir.Block {
	text := e.Text
	switch e.Type {
	case "decision":
		if e.Why != "" {
			text += "\nWhy: " + e.Why
		}
		if e.Rejected != "" {
			text += "\nRejected: " + e.Rejected
		}
		if e.Context != "" {
			text += "\nContext: " + e.Context
		}
	case "link":
		if e.URL != "" {
			text += "\n→ " + e.URL
		}
	}
	return ir.ProseBlock{
		Text: text,
		Prov: ir.Provenance{
			SpecSection: "entry/" + e.Type,
			Sources:     []string{"area/" + e.Area + "/" + e.Type + "/" + e.ID},
			InputHash:   spec.Hash + ":" + e.ID + ":" + e.Updated,
		},
	}
}
