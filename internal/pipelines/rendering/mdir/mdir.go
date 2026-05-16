// Package mdir converts LLM-authored markdown into IR sections.
//
// Extracted from the editorial frontend so every LLM-driven
// frontend (editorial, architecture, …) shares the same hard-won
// handling: ATX headings of any level start a section (LLMs do not
// reliably restrict to ##; flattening beats leaked "### Foo"
// markup), and fenced code blocks become DiagramBlocks (```mermaid
// → rendered+uploaded by the RenderDiagrams pass; other langs →
// <syntaxhighlight>) so a fence never leaks into prose as literal
// text. The IR Section model is flat; heading depth is a future IR
// change.
//
// provPrefix labels block provenance: prose blocks get
// "<prefix>-section", diagram blocks "<prefix>-diagram".
package mdir

import (
	"strings"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Parse splits md into IR sections. Content before the first heading
// becomes a no-heading section so nothing is dropped.
func Parse(md, provPrefix, inputHash string) []ir.Section {
	lines := strings.Split(md, "\n")
	var sections []ir.Section
	var current ir.Section
	var buf strings.Builder
	flush := func() {
		text := strings.TrimSpace(buf.String())
		if text != "" {
			current.Blocks = append(current.Blocks, ir.ProseBlock{
				Text: text,
				Prov: ir.Provenance{SpecSection: provPrefix + "-section", InputHash: inputHash},
			})
		}
		buf.Reset()
	}
	startSection := func(heading string) {
		flush()
		if current.Heading != "" || len(current.Blocks) > 0 {
			sections = append(sections, current)
		}
		current = ir.Section{Heading: heading}
	}

	inFence := false
	fenceLang := ""
	var fenceBuf strings.Builder
	addDiagram := func() {
		current.Blocks = append(current.Blocks, ir.DiagramBlock{
			Lang:   fenceLang,
			Source: strings.TrimRight(fenceBuf.String(), "\n"),
			Prov:   ir.Provenance{SpecSection: provPrefix + "-diagram", InputHash: inputHash},
		})
		fenceBuf.Reset()
		fenceLang = ""
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inFence {
				addDiagram()
				inFence = false
			} else {
				flush()
				fenceLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
				if fenceLang == "" {
					fenceLang = "text"
				}
				inFence = true
			}
			continue
		}
		if inFence {
			fenceBuf.WriteString(line)
			fenceBuf.WriteByte('\n')
			continue
		}
		if h, ok := atxHeading(line); ok {
			startSection(h)
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	if inFence { // unclosed fence — don't lose the content
		addDiagram()
	}
	flush()
	if current.Heading != "" || len(current.Blocks) > 0 {
		sections = append(sections, current)
	}
	return sections
}

// atxHeading reports whether line is a markdown ATX heading of level
// 2–6 and returns the trimmed heading text. Level 1 is intentionally
// not a boundary (the page title is set separately).
func atxHeading(line string) (string, bool) {
	s := strings.TrimRight(line, " \t")
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n < 2 || n > 6 || n >= len(s) || s[n] != ' ' {
		return "", false
	}
	return strings.TrimSpace(s[n+1:]), true
}
