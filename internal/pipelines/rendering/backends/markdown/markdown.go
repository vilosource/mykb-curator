// Package markdown implements backends.Backend by rendering an
// ir.Document to GitHub-Flavoured Markdown.
//
// Output shape:
//
//	---
//	title: ...
//	spec_hash: ...
//	kb_commit: ...
//	generated_at: ...
//	---
//
//	## <section heading>
//
//	<prose paragraphs>
//
//	<!-- CURATOR:BEGIN block=... provenance=... -->
//	<machine block body>
//	<!-- CURATOR:END block=... -->
//
//	(footer comment with run-id + kb-commit)
//
// Determinism: pure function — given the same ir.Document, returns
// identical bytes. No I/O, no map iteration that affects output, no
// time.Now(). Time values come from the IR.
package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Backend renders IR to Markdown.
type Backend struct{}

// New constructs a Backend. The markdown backend has no configurable
// state today; this returns a singleton-equivalent value.
func New() *Backend { return &Backend{} }

// Name returns "markdown".
func (*Backend) Name() string { return "markdown" }

// Render produces the markdown bytes for doc.
func (*Backend) Render(doc ir.Document) ([]byte, error) {
	var buf bytes.Buffer
	writeFrontmatter(&buf, doc.Frontmatter)
	buf.WriteByte('\n')

	if doc.Frontmatter.Title != "" {
		fmt.Fprintf(&buf, "# %s\n\n", doc.Frontmatter.Title)
	}

	for _, sec := range doc.Sections {
		writeSection(&buf, sec)
	}

	writeFooter(&buf, doc.Footer)
	return buf.Bytes(), nil
}

func writeFrontmatter(buf *bytes.Buffer, f ir.Frontmatter) {
	buf.WriteString("---\n")
	if f.Title != "" {
		fmt.Fprintf(buf, "title: %s\n", f.Title)
	}
	if f.SpecHash != "" {
		fmt.Fprintf(buf, "spec_hash: %s\n", f.SpecHash)
	}
	if f.KBCommit != "" {
		fmt.Fprintf(buf, "kb_commit: %s\n", f.KBCommit)
	}
	if !f.GeneratedAt.IsZero() {
		fmt.Fprintf(buf, "generated_at: %s\n", f.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	buf.WriteString("---\n")
}

func writeSection(buf *bytes.Buffer, sec ir.Section) {
	if sec.Heading != "" {
		fmt.Fprintf(buf, "## %s\n\n", sec.Heading)
	}
	for _, blk := range sec.Blocks {
		writeBlock(buf, blk)
		buf.WriteByte('\n')
	}
}

func writeBlock(buf *bytes.Buffer, blk ir.Block) {
	switch b := blk.(type) {
	case ir.ProseBlock:
		buf.WriteString(strings.TrimRight(b.Text, "\n"))
		buf.WriteByte('\n')
	case ir.MachineBlock:
		writeMachineBlock(buf, b)
	case ir.MarkerBlock:
		writeMarkerBlock(buf, b)
	case ir.KBRefBlock:
		writeKBRef(buf, b)
	case ir.TableBlock:
		writeTable(buf, b)
	case ir.DiagramBlock:
		writeDiagram(buf, b)
	case ir.Callout:
		writeCallout(buf, b)
	case ir.EscapeHatch:
		writeEscapeHatch(buf, b)
	default:
		// Unknown block kind: render a visible placeholder rather than
		// silently dropping content. Forces the issue to be seen.
		fmt.Fprintf(buf, "<!-- unknown block kind=%q -->\n", blk.Kind())
	}
}

// writeMachineBlock renders the body of a machine-owned block only.
// Markers around the block come from MarkerBlock siblings produced
// by the ApplyZoneMarkers pass — the backend does not emit marker
// policy itself, keeping the marker convention centralised.
func writeMachineBlock(buf *bytes.Buffer, b ir.MachineBlock) {
	buf.WriteString(strings.TrimRight(b.Body, "\n"))
	buf.WriteByte('\n')
}

// writeMarkerBlock renders a zone-region boundary as an HTML comment.
// HTML comments are inert in markdown, wikitext, and Confluence
// storage format, so the same syntax works across all three.
func writeMarkerBlock(buf *bytes.Buffer, b ir.MarkerBlock) {
	switch b.Position {
	case ir.MarkerBegin:
		fmt.Fprintf(buf, "<!-- CURATOR:BEGIN block=%s provenance=%s -->\n", b.BlockID, b.Prov.InputHash)
	case ir.MarkerEnd:
		fmt.Fprintf(buf, "<!-- CURATOR:END block=%s -->\n", b.BlockID)
	}
}

func writeKBRef(buf *bytes.Buffer, b ir.KBRefBlock) {
	// Pre-ResolveKBRefs pass: render a visible placeholder that
	// includes both area and id so the unresolved reference is
	// readable in raw markdown.
	fmt.Fprintf(buf, "[kb:%s/%s]\n", b.Area, b.ID)
}

func writeTable(buf *bytes.Buffer, b ir.TableBlock) {
	if len(b.Columns) == 0 {
		return
	}
	// header
	buf.WriteString("| ")
	buf.WriteString(strings.Join(b.Columns, " | "))
	buf.WriteString(" |\n")
	// alignment
	buf.WriteString("|")
	for range b.Columns {
		buf.WriteString(" --- |")
	}
	buf.WriteByte('\n')
	// rows
	for _, row := range b.Rows {
		buf.WriteString("| ")
		buf.WriteString(strings.Join(padRow(row, len(b.Columns)), " | "))
		buf.WriteString(" |\n")
	}
}

// padRow ensures a row has exactly n cells (pads with "" or trims).
// Misaligned rows are a spec/frontend bug; padding keeps the table
// renderable rather than crashing.
func padRow(row []string, n int) []string {
	if len(row) == n {
		return row
	}
	if len(row) > n {
		return row[:n]
	}
	out := make([]string, n)
	copy(out, row)
	return out
}

func writeDiagram(buf *bytes.Buffer, b ir.DiagramBlock) {
	if b.AssetRef != "" {
		fmt.Fprintf(buf, "![](%s)\n", b.AssetRef)
		return
	}
	fmt.Fprintf(buf, "```%s\n%s\n```\n", b.Lang, strings.TrimRight(b.Source, "\n"))
}

func writeCallout(buf *bytes.Buffer, b ir.Callout) {
	if b.Severity != "" {
		fmt.Fprintf(buf, "> **%s**\n>\n", b.Severity)
	}
	for _, line := range strings.Split(strings.TrimRight(b.Body, "\n"), "\n") {
		fmt.Fprintf(buf, "> %s\n", line)
	}
}

func writeEscapeHatch(buf *bytes.Buffer, b ir.EscapeHatch) {
	// Only inline escape hatches addressed to *this* backend. Hatches
	// for other backends are intentionally dropped (they don't
	// translate). The portability cost is per-spec, per-hatch, and
	// surfaced in the spec's frontmatter so authors see the trade-off.
	if b.Backend != "markdown" {
		return
	}
	buf.WriteString(strings.TrimRight(b.Raw, "\n"))
	buf.WriteByte('\n')
}

func writeFooter(buf *bytes.Buffer, f ir.Footer) {
	if f.RunID == "" && f.KBCommit == "" && f.LastCurated.IsZero() {
		return
	}
	buf.WriteString("\n<!-- mykb-curator footer\n")
	if f.RunID != "" {
		fmt.Fprintf(buf, "  run-id: %s\n", f.RunID)
	}
	if f.KBCommit != "" {
		fmt.Fprintf(buf, "  kb-commit: %s\n", f.KBCommit)
	}
	if !f.LastCurated.IsZero() {
		fmt.Fprintf(buf, "  last-curated: %s\n", f.LastCurated.UTC().Format("2006-01-02T15:04:05Z"))
	}
	buf.WriteString("-->\n")
}
