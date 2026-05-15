// Package resolvekbrefs implements the ResolveKBRefs pass.
//
// Frontends emit KBRefBlocks pointing at kb entries by (area, id).
// This pass turns each KBRefBlock into a ProseBlock containing the
// referenced entry's text (and decision-specific Why/Rejected when
// applicable). Unresolved refs become visible UNRESOLVED placeholders
// — the rendered page makes the broken ref impossible to miss, and
// the run report catches it for follow-up.
//
// Closes over the kb.Snapshot via constructor, so the pass is
// per-run. The orchestrator builds the pipeline using the snapshot
// it just pulled.
package resolvekbrefs

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// ResolveKBRefs is the Pass impl.
type ResolveKBRefs struct {
	snap kb.Snapshot
}

// New constructs a ResolveKBRefs pass bound to the given snapshot.
func New(snap kb.Snapshot) *ResolveKBRefs {
	return &ResolveKBRefs{snap: snap}
}

// Name returns "resolve-kb-refs".
func (*ResolveKBRefs) Name() string { return "resolve-kb-refs" }

// Apply walks every block, replacing KBRefBlocks with ProseBlocks
// containing the resolved (or placeholder) content.
func (r *ResolveKBRefs) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	for i, sec := range doc.Sections {
		out := make([]ir.Block, 0, len(sec.Blocks))
		for _, b := range sec.Blocks {
			ref, ok := b.(ir.KBRefBlock)
			if !ok {
				out = append(out, b)
				continue
			}
			out = append(out, r.resolve(ref))
		}
		doc.Sections[i].Blocks = out
	}
	return doc, nil
}

// resolve returns a ProseBlock with either the entry's text or a
// visible UNRESOLVED placeholder if the (area, id) doesn't exist.
func (r *ResolveKBRefs) resolve(ref ir.KBRefBlock) ir.Block {
	area := r.snap.Area(ref.Area)
	if area == nil {
		return placeholder(ref, "area not in snapshot")
	}
	for _, e := range area.Entries {
		if e.ID == ref.ID {
			return ir.ProseBlock{
				Text: formatEntry(e),
				Prov: ir.Provenance{
					SpecSection: "resolved-kbref",
					Sources:     []string{fmt.Sprintf("area/%s/%s/%s", ref.Area, e.Type, e.ID)},
					InputHash:   e.Updated,
				},
			}
		}
	}
	return placeholder(ref, "entry id not in area")
}

func placeholder(ref ir.KBRefBlock, why string) ir.Block {
	return ir.ProseBlock{
		Text: fmt.Sprintf("[UNRESOLVED kb-ref: area=%s id=%s — %s]", ref.Area, ref.ID, why),
		Prov: ir.Provenance{
			SpecSection: "unresolved-kbref",
			Sources:     []string{fmt.Sprintf("area/%s/?/%s", ref.Area, ref.ID)},
		},
	}
}

func formatEntry(e kb.Entry) string {
	text := e.Text
	switch e.Type {
	case "decision":
		if e.Why != "" {
			text += "\nWhy: " + e.Why
		}
		if e.Rejected != "" {
			text += "\nRejected: " + e.Rejected
		}
	case "link":
		if e.URL != "" {
			text += "\n→ " + e.URL
		}
	}
	return text
}
