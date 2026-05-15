// merge.go — block-level reconciliation between the existing wiki
// page content and a freshly-rendered new content.
//
// The merge rules:
//   - Prologue/epilogue (text outside any block markers): use NEW
//     render's. These are frontmatter / titles / footers — owned by
//     the backend, not by humans.
//   - For each block in the new render:
//   - if matching block exists on wiki AND zone=editorial AND
//     provenance hashes match → keep wiki body (human polish
//     survives).
//   - otherwise (no match, or machine zone, or different prov):
//     use the new render's body.
//   - Orphan blocks (on wiki but not in new render): dropped. The
//     wiki reflects the current spec's intent; stale blocks would
//     accumulate forever otherwise.
//
// Inputs/outputs are opaque text: works for markdown, wikitext, and
// Confluence storage format alike (they all use the same HTML
// comment marker syntax).
package reconciler

import (
	"strings"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/reconciler/wikiparse"
)

// MergeBlocks composes the final wiki page content by combining the
// existing wiki content (for editorial blocks whose inputs haven't
// changed) with the new render (for machine blocks, new blocks, and
// editorial blocks whose inputs have changed).
//
// Both inputs are content bytes; the result is the merged content
// ready for upsert.
func MergeBlocks(existing, newContent []byte) string {
	newDoc := wikiparse.Parse(newContent)
	exDoc := wikiparse.Parse(existing)

	var sb strings.Builder
	sb.WriteString(newDoc.Prologue)

	for _, nb := range newDoc.Blocks {
		body := chooseBody(nb, &exDoc)
		writeBlock(&sb, nb, body)
		sb.WriteString(nb.FollowingText)
	}
	sb.WriteString(newDoc.Epilogue)
	return sb.String()
}

// chooseBody applies the merge rules for one new-render block.
func chooseBody(nb wikiparse.Block, existing *wikiparse.ParsedDoc) string {
	if nb.Zone != "editorial" {
		return nb.Body
	}
	matching, ok := existing.BlockByID(nb.ID)
	if !ok {
		return nb.Body
	}
	if matching.Provenance == "" || matching.Provenance != nb.Provenance {
		return nb.Body
	}
	// Editorial + matching provenance → preserve wiki body.
	return matching.Body
}

// writeBlock emits the BEGIN marker, body, and END marker for one
// block. Marker shape matches what the markdown backend produces.
func writeBlock(sb *strings.Builder, nb wikiparse.Block, body string) {
	if nb.Zone != "" || nb.Provenance != "" {
		sb.WriteString("<!-- CURATOR:BEGIN block=")
		sb.WriteString(nb.ID)
		if nb.Zone != "" {
			sb.WriteString(" zone=")
			sb.WriteString(nb.Zone)
		}
		if nb.Provenance != "" {
			sb.WriteString(" provenance=")
			sb.WriteString(nb.Provenance)
		}
		sb.WriteString(" -->\n")
	} else {
		sb.WriteString("<!-- CURATOR:BEGIN block=")
		sb.WriteString(nb.ID)
		sb.WriteString(" -->\n")
	}
	sb.WriteString(body)
	sb.WriteString("\n<!-- CURATOR:END block=")
	sb.WriteString(nb.ID)
	sb.WriteString(" -->\n")
}
