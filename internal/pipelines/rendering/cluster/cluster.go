// Package cluster orchestrates one docspec.DocSpec (a topic = parent
// page + N cross-linked children) into a set of rendered IR
// Documents.
//
// Division of responsibility (kept deliberately clean):
//   - the architecture Frontend owns a single page: section order,
//     prose synthesis, render:table. It cannot know the cluster it
//     belongs to, so for render:child-index it emits an empty,
//     position-correct placeholder.
//   - the cluster owns relationships: it fills the parent's
//     child-index placeholders with the real children, and appends
//     the generated cross-links every page needs — "Part of" (child
//     → parent backlink), "Related pages", and the category tags.
//
// Everything the cluster adds is derived from the spec, never
// fabricated. The cluster is deterministic given its Renderer.
package cluster

import (
	"context"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Renderer renders a single DocPage. *architecture.Frontend
// satisfies it; tests inject a deterministic stub.
type Renderer interface {
	Render(ctx context.Context, page docspec.DocPage, snap kb.Snapshot) (ir.Document, error)
}

// Cluster expands a DocSpec into rendered pages with cross-links.
type Cluster struct {
	fe Renderer
}

// New binds a Cluster to a page Renderer.
func New(fe Renderer) *Cluster { return &Cluster{fe: fe} }

// RenderedPage is one wiki page ready for the pass pipeline + backend.
type RenderedPage struct {
	Page string
	Kind string
	Doc  ir.Document
}

// Render produces one RenderedPage per docspec page (parent first,
// then children in declared order). The parent's child-index
// placeholders are filled with the children; every page gets its
// generated "Part of" / "Related pages" / category cross-links.
func (c *Cluster) Render(ctx context.Context, spec docspec.DocSpec, snap kb.Snapshot) ([]RenderedPage, error) {
	pages := append([]docspec.DocPage{spec.Parent}, spec.Children...)
	out := make([]RenderedPage, 0, len(pages))

	for idx, p := range pages {
		doc, err := c.fe.Render(ctx, p, snap)
		if err != nil {
			return nil, err
		}
		if idx == 0 {
			fillChildIndex(&doc, spec)
		} else {
			prependPartOf(&doc, spec.Topic, spec.Parent.Page)
		}
		appendRelated(&doc, p.Related)
		appendCategories(&doc, p.Categories)

		out = append(out, RenderedPage{Page: p.Page, Kind: p.Kind, Doc: doc})
	}
	return out, nil
}

// fillChildIndex replaces every empty child-index placeholder
// (recognised by its provenance) with an IndexBlock listing the
// cluster's children. The placeholder's position — chosen by the
// spec author and preserved by the frontend — is kept.
func fillChildIndex(doc *ir.Document, spec docspec.DocSpec) {
	entries := make([]ir.IndexEntry, 0, len(spec.Children))
	for _, ch := range spec.Children {
		entries = append(entries, ir.IndexEntry{
			Page:  ch.Page,
			Label: pageLabel(ch.Page),
			Desc:  ch.Intent,
		})
	}
	for si := range doc.Sections {
		for bi, blk := range doc.Sections[si].Blocks {
			ib, ok := blk.(ir.IndexBlock)
			if !ok || ib.Prov.SpecSection != architecture.ChildIndexProv {
				continue
			}
			ib.Entries = entries
			doc.Sections[si].Blocks[bi] = ib
		}
	}
}

// prependPartOf adds a leading "Part of" backlink section to a child
// page so a reader (and the wiki) always know the parent topic.
func prependPartOf(doc *ir.Document, topic, parentPage string) {
	sec := ir.Section{
		Heading: "Part of",
		Blocks: []ir.Block{ir.IndexBlock{
			Entries: []ir.IndexEntry{{Page: parentPage, Label: topic}},
			Prov:    ir.Provenance{SpecSection: "cluster-part-of"},
		}},
	}
	doc.Sections = append([]ir.Section{sec}, doc.Sections...)
}

// appendRelated adds a trailing "Related pages" section for the
// spec-declared related links. Labels are derived from the page
// title; the spec carries no descriptions for related links.
func appendRelated(doc *ir.Document, related []string) {
	var entries []ir.IndexEntry
	for _, r := range related {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		entries = append(entries, ir.IndexEntry{Page: r, Label: pageLabel(r)})
	}
	if len(entries) == 0 {
		return
	}
	doc.Sections = append(doc.Sections, ir.Section{
		Heading: "Related pages",
		Blocks: []ir.Block{ir.IndexBlock{
			Entries: entries,
			Prov:    ir.Provenance{SpecSection: "cluster-related"},
		}},
	})
}

// appendCategories adds the spec-declared taxonomy as a trailing
// no-heading CategoryBlock (MediaWiki convention: categories at the
// foot of the page).
func appendCategories(doc *ir.Document, cats []string) {
	var names []string
	for _, c := range cats {
		if c = strings.TrimSpace(c); c != "" {
			names = append(names, c)
		}
	}
	if len(names) == 0 {
		return
	}
	doc.Sections = append(doc.Sections, ir.Section{
		Blocks: []ir.Block{ir.CategoryBlock{
			Names: names,
			Prov:  ir.Provenance{SpecSection: "cluster-categories"},
		}},
	})
}

// pageLabel turns a wiki page path into human link text:
// "OptiscanGroup/Azure_Infrastructure/Vault_Operations" →
// "Vault Operations". The last path segment is the page; underscores
// are MediaWiki's spaces.
func pageLabel(page string) string {
	seg := page
	if i := strings.LastIndexByte(page, '/'); i >= 0 {
		seg = page[i+1:]
	}
	return strings.ReplaceAll(seg, "_", " ")
}
