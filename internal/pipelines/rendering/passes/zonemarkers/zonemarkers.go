// Package zonemarkers implements the ApplyZoneMarkers pass.
//
// Wraps every block (machine AND editorial) with MarkerBlock{Begin}
// and MarkerBlock{End} carrying a stable BlockID + the block's
// provenance hash. Backends render MarkerBlocks in format-appropriate
// syntax (HTML comments for markdown / wikitext / Confluence).
//
// Why wrap editorial blocks too: the reconciler uses the markers on
// the existing wiki page to find "the block we wrote last time" and
// decide whether to preserve human polish (editorial; same prov hash
// → keep) or overwrite (machine; or different prov hash → use new).
//
// BlockID assignment: position-based, "s{section}b{block}", stable
// across runs as long as the spec's section structure is. Existing
// blocks carrying their own BlockID (currently only MachineBlock)
// keep theirs.
//
// Idempotent: blocks already preceded/followed by matching markers
// are not re-wrapped. Safe to run twice.
package zonemarkers

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// ApplyZoneMarkers is the Pass impl.
type ApplyZoneMarkers struct{}

// New constructs an ApplyZoneMarkers pass.
func New() *ApplyZoneMarkers { return &ApplyZoneMarkers{} }

// Name returns "apply-zone-markers".
func (*ApplyZoneMarkers) Name() string { return "apply-zone-markers" }

// Apply wraps each block in begin/end MarkerBlocks.
func (*ApplyZoneMarkers) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	for i, sec := range doc.Sections {
		doc.Sections[i].Blocks = wrapBlocks(sec.Blocks, i)
	}
	return doc, nil
}

// wrapBlocks walks the block list and emits a new slice with markers
// inserted around each (un-marked) block. MarkerBlocks themselves
// pass through unchanged so the pass is idempotent.
func wrapBlocks(in []ir.Block, sectionIdx int) []ir.Block {
	out := make([]ir.Block, 0, len(in))
	blockIdx := 0
	for i, b := range in {
		if _, isMarker := b.(ir.MarkerBlock); isMarker {
			out = append(out, b)
			continue
		}
		if alreadyWrapped(in, i) {
			out = append(out, b)
			blockIdx++
			continue
		}
		id := blockIDFor(b, sectionIdx, blockIdx)
		blockIdx++
		zone := zoneLabel(b)
		out = append(out,
			ir.MarkerBlock{Position: ir.MarkerBegin, BlockID: id, Prov: b.Provenance(), OfZone: zone},
			b,
			ir.MarkerBlock{Position: ir.MarkerEnd, BlockID: id, Prov: b.Provenance(), OfZone: zone},
		)
	}
	return out
}

// zoneLabel renders a block's zone for embedding in a marker.
// "editorial" tells the reconciler to preserve content (when
// provenance matches across runs); "machine" tells it to overwrite.
func zoneLabel(b ir.Block) string {
	if b.Zone() == ir.ZoneEditorial {
		return "editorial"
	}
	return "machine"
}

// blockIDFor returns a stable, position-derived ID for a block.
// MachineBlocks keep their explicit BlockID; everything else gets a
// synthesised "s{section}-b{block}" ID. Stable across runs as long
// as the spec's section structure is.
func blockIDFor(b ir.Block, sectionIdx, blockIdx int) string {
	if mb, ok := b.(ir.MachineBlock); ok && mb.BlockID != "" {
		return mb.BlockID
	}
	return fmt.Sprintf("s%d-b%d", sectionIdx, blockIdx)
}

// alreadyWrapped reports whether the block at position i is already
// preceded and followed by matching markers. Defends against double-
// wrapping when the pass runs twice.
func alreadyWrapped(blocks []ir.Block, i int) bool {
	if i == 0 || i == len(blocks)-1 {
		return false
	}
	prev, prevOK := blocks[i-1].(ir.MarkerBlock)
	next, nextOK := blocks[i+1].(ir.MarkerBlock)
	if !prevOK || !nextOK {
		return false
	}
	return prev.Position == ir.MarkerBegin &&
		next.Position == ir.MarkerEnd &&
		prev.BlockID == next.BlockID
}
