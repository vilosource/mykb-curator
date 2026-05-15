// Package zonemarkers implements the ApplyZoneMarkers pass.
//
// Wraps every MachineBlock with MarkerBlock{Begin} and MarkerBlock{End}
// carrying the machine block's id + provenance. Backends render
// MarkerBlocks in format-appropriate syntax (HTML comments for
// markdown / wikitext / Confluence).
//
// This is the architectural lynchpin of the soft-read-only contract:
// the markers let the reconciler identify machine-owned regions on
// the wiki at the next run without parsing the entire page.
//
// Idempotent: machine blocks already preceded/followed by matching
// markers are not re-wrapped. Safe to run twice.
package zonemarkers

import (
	"context"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// ApplyZoneMarkers is the Pass impl.
type ApplyZoneMarkers struct{}

// New constructs an ApplyZoneMarkers pass.
func New() *ApplyZoneMarkers { return &ApplyZoneMarkers{} }

// Name returns "apply-zone-markers".
func (*ApplyZoneMarkers) Name() string { return "apply-zone-markers" }

// Apply wraps each MachineBlock in begin/end MarkerBlocks. Editorial
// blocks (prose, callout, etc.) are passed through untouched.
func (*ApplyZoneMarkers) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	for i, sec := range doc.Sections {
		doc.Sections[i].Blocks = wrapBlocks(sec.Blocks)
	}
	return doc, nil
}

// wrapBlocks walks the block list and emits a new slice with markers
// inserted around each (un-marked) MachineBlock.
func wrapBlocks(in []ir.Block) []ir.Block {
	out := make([]ir.Block, 0, len(in))
	for i, b := range in {
		mb, isMachine := b.(ir.MachineBlock)
		if !isMachine {
			out = append(out, b)
			continue
		}
		if alreadyWrapped(in, i) {
			out = append(out, b)
			continue
		}
		out = append(out,
			ir.MarkerBlock{
				Position: ir.MarkerBegin,
				BlockID:  mb.BlockID,
				Prov:     mb.Prov,
			},
			mb,
			ir.MarkerBlock{
				Position: ir.MarkerEnd,
				BlockID:  mb.BlockID,
				Prov:     mb.Prov,
			},
		)
	}
	return out
}

// alreadyWrapped reports whether the MachineBlock at position i in
// the original list already has matching markers immediately before
// and after it. Used to make the pass idempotent.
func alreadyWrapped(blocks []ir.Block, i int) bool {
	if i == 0 || i == len(blocks)-1 {
		return false
	}
	mb, ok := blocks[i].(ir.MachineBlock)
	if !ok {
		return false
	}
	prev, prevOK := blocks[i-1].(ir.MarkerBlock)
	next, nextOK := blocks[i+1].(ir.MarkerBlock)
	if !prevOK || !nextOK {
		return false
	}
	return prev.Position == ir.MarkerBegin && prev.BlockID == mb.BlockID &&
		next.Position == ir.MarkerEnd && next.BlockID == mb.BlockID
}
