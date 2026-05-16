// Package mediawiki implements backends.Backend by rendering an
// ir.Document to MediaWiki wikitext.
//
// This exists because pushing the Markdown backend's output into a
// MediaWiki target rendered every heading as a list item (`#`/`##`
// are wikitext list syntax, not headings) and printed the YAML
// frontmatter as literal body text. Wikitext needs its own backend.
//
// Output shape:
//
//	<!-- mykb-curator spec_hash=... kb_commit=... generated_at=... -->
//
//	== <section heading> ==
//
//	<prose paragraph>
//
//	<!-- CURATOR:BEGIN block=... zone=... provenance=... -->
//	<machine block body>
//	<!-- CURATOR:END block=... -->
//
//	<!-- mykb-curator footer run-id=... kb-commit=... -->
//
// Notes:
//   - The page title is the wiki page name (set by the wiki target),
//     so it is NOT emitted as a body heading.
//   - Provenance/footer are inert HTML comments — wikitext ignores
//     them, same as the reconciler's CURATOR markers.
//   - The CURATOR marker convention is byte-identical to the
//     markdown backend's: the reconciler matches on it and must not
//     see a per-backend variation.
//
// Determinism: pure function — same ir.Document → identical bytes.
// No I/O, no time.Now() (times come from the IR).
package mediawiki

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Backend renders IR to MediaWiki wikitext.
type Backend struct{}

// New constructs a Backend. No configurable state today.
func New() *Backend { return &Backend{} }

// Name returns "mediawiki".
func (*Backend) Name() string { return "mediawiki" }

// Render produces the wikitext bytes for doc.
func (*Backend) Render(doc ir.Document) ([]byte, error) {
	var buf bytes.Buffer
	writeProvenance(&buf, doc.Frontmatter)

	for _, sec := range doc.Sections {
		writeSection(&buf, sec)
	}

	writeFooter(&buf, doc.Footer)
	return buf.Bytes(), nil
}

// writeProvenance emits the spec/kb provenance as a leading inert
// HTML comment. Not YAML, not visible body — wikitext has no
// frontmatter concept.
func writeProvenance(buf *bytes.Buffer, f ir.Frontmatter) {
	parts := []string{"mykb-curator"}
	if f.SpecHash != "" {
		parts = append(parts, "spec_hash="+f.SpecHash)
	}
	if f.KBCommit != "" {
		parts = append(parts, "kb_commit="+f.KBCommit)
	}
	if !f.GeneratedAt.IsZero() {
		parts = append(parts, "generated_at="+f.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(buf, "<!-- %s -->\n\n", strings.Join(parts, " "))
}

func writeSection(buf *bytes.Buffer, sec ir.Section) {
	if sec.Heading != "" {
		// Wikitext H2 (single = is the page-title level, reserved).
		fmt.Fprintf(buf, "== %s ==\n\n", sec.Heading)
	}
	for _, blk := range sec.Blocks {
		writeBlock(buf, blk)
	}
}

func writeBlock(buf *bytes.Buffer, blk ir.Block) {
	switch b := blk.(type) {
	case ir.ProseBlock:
		writeParagraph(buf, b.Text)
	case ir.MachineBlock:
		buf.WriteString(strings.TrimRight(b.Body, "\n"))
		buf.WriteString("\n\n")
	case ir.MarkerBlock:
		writeMarkerBlock(buf, b)
	case ir.KBRefBlock:
		// Pre-ResolveKBRefs: visible, readable placeholder.
		fmt.Fprintf(buf, "[kb:%s/%s]\n\n", b.Area, b.ID)
	case ir.TableBlock:
		writeTable(buf, b)
	case ir.DiagramBlock:
		writeDiagram(buf, b)
	case ir.Callout:
		writeCallout(buf, b)
	case ir.EscapeHatch:
		// Only hatches addressed to this backend translate.
		if b.Backend == "mediawiki" {
			buf.WriteString(strings.TrimRight(b.Raw, "\n"))
			buf.WriteString("\n\n")
		}
	default:
		fmt.Fprintf(buf, "<!-- unknown block kind=%q -->\n\n", blk.Kind())
	}
}

// writeParagraph emits text as a wikitext paragraph. A blank line
// terminates the paragraph; single newlines inside the text are
// collapsed to spaces so the source's hard-wraps don't fragment the
// rendered paragraph (MediaWiki treats a lone newline as a space,
// but normalising keeps the wikitext clean and diff-stable).
func writeParagraph(buf *bytes.Buffer, text string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	t = strings.ReplaceAll(t, "\r\n", "\n")
	t = strings.Join(strings.Fields(strings.ReplaceAll(t, "\n", " ")), " ")
	buf.WriteString(t)
	buf.WriteString("\n\n")
}

// writeMarkerBlock renders the zone boundary identically to the
// markdown backend (HTML comments are inert in wikitext too). The
// reconciler matches on this exact text — do not vary it per backend.
func writeMarkerBlock(buf *bytes.Buffer, b ir.MarkerBlock) {
	switch b.Position {
	case ir.MarkerBegin:
		zone := b.OfZone
		if zone == "" {
			zone = "machine"
		}
		fmt.Fprintf(buf, "<!-- CURATOR:BEGIN block=%s zone=%s provenance=%s -->\n", b.BlockID, zone, b.Prov.InputHash)
	case ir.MarkerEnd:
		fmt.Fprintf(buf, "<!-- CURATOR:END block=%s -->\n", b.BlockID)
	}
}

func writeTable(buf *bytes.Buffer, b ir.TableBlock) {
	if len(b.Columns) == 0 {
		return
	}
	buf.WriteString("{| class=\"wikitable\"\n")
	buf.WriteString("! ")
	buf.WriteString(strings.Join(b.Columns, " !! "))
	buf.WriteByte('\n')
	for _, row := range b.Rows {
		buf.WriteString("|-\n")
		buf.WriteString("| ")
		buf.WriteString(strings.Join(padRow(row, len(b.Columns)), " || "))
		buf.WriteByte('\n')
	}
	buf.WriteString("|}\n\n")
}

func padRow(row []string, n int) []string {
	switch {
	case len(row) == n:
		return row
	case len(row) > n:
		return row[:n]
	default:
		out := make([]string, n)
		copy(out, row)
		return out
	}
}

func writeDiagram(buf *bytes.Buffer, b ir.DiagramBlock) {
	if b.AssetRef != "" {
		// AssetRef from the wiki adapter's UploadFile is a "File:Name"
		// title; embed it.
		ref := strings.TrimPrefix(b.AssetRef, "File:")
		fmt.Fprintf(buf, "[[File:%s]]\n\n", ref)
		return
	}
	// Unrendered source (RenderDiagrams not run / unsupported lang):
	// keep it visible + non-interpreted.
	fmt.Fprintf(buf, "<syntaxhighlight lang=\"%s\">\n%s\n</syntaxhighlight>\n\n", b.Lang, strings.TrimRight(b.Source, "\n"))
}

func writeCallout(buf *bytes.Buffer, b ir.Callout) {
	buf.WriteString("<blockquote>\n")
	if b.Severity != "" {
		fmt.Fprintf(buf, "'''%s:''' ", strings.ToUpper(b.Severity[:1])+b.Severity[1:])
	}
	buf.WriteString(strings.TrimSpace(strings.ReplaceAll(b.Body, "\n", " ")))
	buf.WriteString("\n</blockquote>\n\n")
}

func writeFooter(buf *bytes.Buffer, f ir.Footer) {
	if f.RunID == "" && f.KBCommit == "" && f.LastCurated.IsZero() {
		return
	}
	parts := []string{"mykb-curator", "footer"}
	if f.RunID != "" {
		parts = append(parts, "run-id="+f.RunID)
	}
	if f.KBCommit != "" {
		parts = append(parts, "kb-commit="+f.KBCommit)
	}
	if !f.LastCurated.IsZero() {
		parts = append(parts, "last-curated="+f.LastCurated.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(buf, "<!-- %s -->\n", strings.Join(parts, " "))
}
