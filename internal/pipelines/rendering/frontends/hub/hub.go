// Package hub implements a deterministic Frontend for index / hub
// pages — the navigation backbone of a progressive-disclosure wiki
// (top-level orientation pages that link down into detail).
//
// No LLM. Pure function of (spec, snapshot): the page STRUCTURE is
// authored in the spec (hub.sections → links); link DESCRIPTIONS may
// be sourced from the kb (a link with `area:` and no `desc:` gets
// that area's summary), so the index stays fresh from the brain
// without ever being LLM-guessed. Same inputs → byte-identical
// output.
//
// Output shape: an optional intro paragraph from spec.Body, then one
// Section per hub.section, each containing a single IndexBlock of
// internal links. Link targets are validated by the ValidateLinks
// pass (a hub's job is correct navigation, so broken links must be
// caught, not shipped).
package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Frontend is the hub-mode frontend.
type Frontend struct{}

// New constructs a Frontend.
func New() *Frontend { return &Frontend{} }

// Name returns "hub-frontend".
func (*Frontend) Name() string { return "hub-frontend" }

// Kind returns "hub" — the spec.Kind value this frontend handles.
func (*Frontend) Kind() string { return "hub" }

// Build turns a hub spec into an index Document. Deterministic.
func (*Frontend) Build(_ context.Context, spec specs.Spec, snap kb.Snapshot) (ir.Document, error) {
	if spec.Hub == nil {
		return ir.Document{}, fmt.Errorf("hub: spec %q has kind=hub but no hub structure", spec.ID)
	}

	doc := ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:    spec.Page,
			SpecHash: spec.Hash,
			KBCommit: snap.Commit,
		},
	}

	// Optional intro. ProseBlock (editorial zone) so a human can
	// polish the blurb while the link sections stay machine-owned.
	if body := strings.TrimSpace(spec.Body); body != "" {
		doc.Sections = append(doc.Sections, ir.Section{
			Blocks: []ir.Block{ir.ProseBlock{
				Text: body,
				Prov: ir.Provenance{SpecSection: "hub.intro", InputHash: hashStr(body)},
			}},
		})
	}

	for i, sec := range spec.Hub.Sections {
		entries := make([]ir.IndexEntry, 0, len(sec.Links))
		var sources []string
		for _, l := range sec.Links {
			desc := l.Desc
			if desc == "" && l.Area != "" {
				if a := snap.Area(l.Area); a != nil {
					desc = a.Summary
					sources = append(sources, "area/"+a.ID)
				}
			}
			entries = append(entries, ir.IndexEntry{
				Page:  l.Page,
				Label: l.Label,
				Desc:  desc,
			})
		}
		blocks := make([]ir.Block, 0, 2)
		if d := strings.TrimSpace(sec.Desc); d != "" {
			// The per-section "Focus:" blurb. ProseBlock so a human
			// can polish it; it sits above the link list.
			blocks = append(blocks, ir.ProseBlock{
				Text: d,
				Prov: ir.Provenance{
					SpecSection: fmt.Sprintf("hub.section[%d].desc", i),
					InputHash:   hashStr(d),
				},
			})
		}
		blocks = append(blocks, ir.IndexBlock{
			Entries: entries,
			Prov: ir.Provenance{
				SpecSection: fmt.Sprintf("hub.section[%d]", i),
				Sources:     sources,
				InputHash:   hashEntries(entries),
			},
		})
		doc.Sections = append(doc.Sections, ir.Section{
			Heading: sec.Title,
			Blocks:  blocks,
		})
	}

	return doc, nil
}

func hashStr(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// hashEntries is a canonical, order-sensitive digest of the rendered
// link list so the reconciler can detect a changed index.
func hashEntries(es []ir.IndexEntry) string {
	var b strings.Builder
	for _, e := range es {
		b.WriteString(e.Page)
		b.WriteByte('\x00')
		b.WriteString(e.Label)
		b.WriteByte('\x00')
		b.WriteString(e.Desc)
		b.WriteByte('\n')
	}
	return hashStr(b.String())
}
