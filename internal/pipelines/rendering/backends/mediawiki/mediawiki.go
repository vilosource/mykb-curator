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
	"regexp"
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
	case ir.IndexBlock:
		writeIndex(buf, b)
	case ir.CategoryBlock:
		writeCategories(buf, b)
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

// writeParagraph emits prose as one or more wikitext paragraphs.
//
// Blank lines split paragraphs (a paragraph is NOT merged across
// them — collapsing everything into one physical line was a bug: a
// single line that begins with #/*/:/;/= is wikitext markup, so
// merged prose containing such a line rendered as a list/heading).
// Within a paragraph, hard-wrapped lines are joined with a space
// (MediaWiki treats a lone newline as a space anyway). If a
// paragraph still begins with a wikitext-significant character
// (residual markdown the frontend didn't strip), it is guarded with
// <nowiki/> so it renders as the literal text the author wrote.
func writeParagraph(buf *bytes.Buffer, text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var para []string
	inList := false

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		joined := strings.Join(para, " ")
		para = para[:0]
		joined = mdInline(joined)
		if joined == "" {
			return
		}
		if strings.ContainsRune("#*:;=|!", rune(joined[0])) {
			buf.WriteString("<nowiki/>")
		}
		buf.WriteString(joined)
		buf.WriteString("\n\n")
	}
	endList := func() {
		if inList {
			buf.WriteByte('\n')
			inList = false
		}
	}

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			flushPara()
			endList()
			continue
		}
		if item, ok := bulletItem(trimmed); ok {
			flushPara()
			inList = true
			fmt.Fprintf(buf, "* %s\n", mdInline(item))
			continue
		}
		if item, ok := numberedItem(trimmed); ok {
			flushPara()
			inList = true
			fmt.Fprintf(buf, "# %s\n", mdInline(item))
			continue
		}
		endList()
		para = append(para, trimmed)
	}
	flushPara()
	endList()
}

var (
	reBullet   = regexp.MustCompile(`^[-*+]\s+(.*)$`)
	reNumbered = regexp.MustCompile(`^\d+[.)]\s+(.*)$`)
	reMDLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reMDBold   = regexp.MustCompile(`(?:\*\*|__)([^*_]+?)(?:\*\*|__)`)
	reMDItalic = regexp.MustCompile(`(?:\*|_)([^*_]+?)(?:\*|_)`)
	reMDCode   = regexp.MustCompile("`([^`]+?)`")
)

func bulletItem(s string) (string, bool) {
	m := reBullet.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func numberedItem(s string) (string, bool) {
	m := reNumbered.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// mdInline converts the markdown inline syntax LLMs reliably emit
// (despite the prose-only prompt) into wikitext: links → [url text],
// **bold**/__bold__ → ”'…”', *italic*/_italic_ → ”…”, `code` →
// <code>…</code>. Code spans are protected first so their contents
// are not re-processed as emphasis.
func mdInline(s string) string {
	type tok struct{ ph, val string }
	var toks []tok
	s = reMDCode.ReplaceAllStringFunc(s, func(m string) string {
		inner := reMDCode.FindStringSubmatch(m)[1]
		ph := fmt.Sprintf("\x00C%d\x00", len(toks))
		toks = append(toks, tok{ph, "<code>" + inner + "</code>"})
		return ph
	})
	s = reMDLink.ReplaceAllString(s, "[$2 $1]")
	s = reMDBold.ReplaceAllString(s, "'''$1'''")
	s = reMDItalic.ReplaceAllString(s, "''$1''")
	for _, t := range toks {
		s = strings.ReplaceAll(s, t.ph, t.val)
	}
	return s
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

// writeIndex renders a curated link list (hub / index pages) as a
// wikitext bullet list of internal links. `[[Page|Label]]` when a
// distinct label is given, bare `[[Page]]` otherwise; optional
// " — Desc" suffix.
func writeIndex(buf *bytes.Buffer, b ir.IndexBlock) {
	for _, e := range b.Entries {
		link := "[[" + e.Page + "]]"
		if e.Label != "" && e.Label != e.Page {
			link = "[[" + e.Page + "|" + e.Label + "]]"
		}
		if e.Desc != "" {
			fmt.Fprintf(buf, "* %s — %s\n", link, e.Desc)
		} else {
			fmt.Fprintf(buf, "* %s\n", link)
		}
	}
	buf.WriteByte('\n')
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

// writeCategories emits MediaWiki category links. Order is preserved
// (spec-declared); blank names are skipped.
func writeCategories(buf *bytes.Buffer, b ir.CategoryBlock) {
	for _, n := range b.Names {
		if n = strings.TrimSpace(n); n != "" {
			fmt.Fprintf(buf, "[[Category:%s]]\n", n)
		}
	}
	buf.WriteByte('\n')
}

func writeDiagram(buf *bytes.Buffer, b ir.DiagramBlock) {
	if b.AssetRef != "" {
		// AssetRef from the wiki adapter's UploadFile is a "File:Name"
		// title; embed it.
		ref := strings.TrimPrefix(b.AssetRef, "File:")
		fmt.Fprintf(buf, "[[File:%s]]\n\n", ref)
		return
	}
	// Unrendered source (RenderDiagrams degraded / unsupported lang):
	// keep it visible + non-interpreted using CORE <pre> —
	// <syntaxhighlight> needs the SyntaxHighlight extension which a
	// vanilla wiki does not have (it would render as literal text).
	src := strings.TrimRight(b.Source, "\n")
	src = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(src)
	fmt.Fprintf(buf, "<pre>\n%s\n</pre>\n\n", src)
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
