// Package editorial implements an LLM-driven Frontend.
//
// Reads spec.Body (the human-authored intent) + kb content from
// spec.Include.Areas, constructs a structured prompt, asks the LLM
// for a markdown page, and parses the response back into IR.
//
// LLM contract: the prompt instructs the LLM to produce markdown
// with `## ` section headings and plain prose paragraphs. The
// parser is forgiving but the prompt is strict.
//
// Determinism + caching: the frontend itself is deterministic-given-
// the-LLM-response. The LLM is wrapped with CacheDecorator at the
// composition root, so repeated runs with unchanged inputs reuse the
// recorded response. The frontend doesn't manage caching itself.
package editorial

import (
	"context"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Frontend is the editorial-mode (LLM-driven) frontend.
type Frontend struct {
	llm   llm.Client
	model string
}

// New constructs a Frontend bound to an LLM client + model.
func New(client llm.Client, model string) *Frontend {
	return &Frontend{llm: client, model: model}
}

// Name returns "editorial-frontend".
func (*Frontend) Name() string { return "editorial-frontend" }

// Kind returns "editorial".
func (*Frontend) Kind() string { return "editorial" }

// Build produces the IR Document by:
//  1. Composing a prompt from spec body + included kb entries.
//  2. Asking the LLM for a markdown page.
//  3. Parsing the markdown into sections + prose blocks.
//
// Empty LLM responses are an error — empty pages on the wiki are
// always worse than a failed render that surfaces in the report.
func (f *Frontend) Build(ctx context.Context, spec specs.Spec, snap kb.Snapshot) (ir.Document, error) {
	prompt := composePrompt(spec, snap)
	resp, err := f.llm.Complete(ctx, llm.Request{
		Model:     f.model,
		System:    systemPrompt,
		Prompt:    prompt,
		MaxTokens: 4096,
	})
	if err != nil {
		return ir.Document{}, fmt.Errorf("editorial: llm: %w", err)
	}
	if strings.TrimSpace(resp.Text) == "" {
		return ir.Document{}, fmt.Errorf("editorial: llm returned empty response — refusing to push empty page")
	}

	return ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:    spec.Page,
			SpecHash: spec.Hash,
			KBCommit: snap.Commit,
		},
		Sections: parseMarkdown(resp.Text, spec.Hash),
	}, nil
}

// systemPrompt is the persona instruction sent on every editorial
// frontend call. Kept terse — the per-page intent lives in the user
// message.
const systemPrompt = `You are a wiki editor for an engineering organisation. Your job is to write clear, factual wiki pages that capture institutional knowledge for a technical reader.

Style:
- Markdown output only. No preamble, no postscript, no code fences around the entire response.
- Use ## for section headings. Do not use # — the page title is set separately.
- Plain prose paragraphs. No bullet lists, no tables, no code blocks in v0.5.
- Write what the kb supports. Do not invent facts.`

// composePrompt assembles the per-page user message from intent +
// kb digest.
func composePrompt(spec specs.Spec, snap kb.Snapshot) string {
	var sb strings.Builder
	sb.WriteString("# Page: ")
	sb.WriteString(spec.Page)
	sb.WriteString("\n\n")

	if body := strings.TrimSpace(spec.Body); body != "" {
		sb.WriteString("## Intent\n\n")
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Available kb content\n\n")
	sb.WriteString("Use only the following knowledge to write the page. Do not invent facts beyond this.\n\n")
	for _, areaID := range spec.Include.Areas {
		a := snap.Area(areaID)
		if a == nil {
			continue
		}
		fmt.Fprintf(&sb, "### Area: %s — %s\n\n", a.ID, a.Name)
		if a.Summary != "" {
			fmt.Fprintf(&sb, "Summary: %s\n\n", a.Summary)
		}
		for _, e := range a.Entries {
			fmt.Fprintf(&sb, "- [%s/%s] %s\n", e.Type, e.ID, e.Text)
			if e.Why != "" {
				fmt.Fprintf(&sb, "    Why: %s\n", e.Why)
			}
			if e.Rejected != "" {
				fmt.Fprintf(&sb, "    Rejected: %s\n", e.Rejected)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Now write the page body, starting with the first ## section heading.\n")
	return sb.String()
}

// parseMarkdown splits LLM output into IR sections by scanning for
// `## ` headings. Content before the first heading becomes a single
// no-heading section so prose isn't dropped.
func parseMarkdown(md, specHash string) []ir.Section {
	lines := strings.Split(md, "\n")
	var sections []ir.Section
	var current ir.Section
	var buf strings.Builder
	flush := func() {
		text := strings.TrimSpace(buf.String())
		if text != "" {
			current.Blocks = append(current.Blocks, ir.ProseBlock{
				Text: text,
				Prov: ir.Provenance{
					SpecSection: "editorial-section",
					InputHash:   specHash,
				},
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

	for _, line := range lines {
		// Any ATX heading (## … ######) starts a section. LLMs don't
		// reliably restrict themselves to ## despite the system
		// prompt; treating only ## as a boundary leaked "### Foo"
		// markdown verbatim into prose (and thence as broken
		// wikitext). Hierarchy is flattened — the IR Section model is
		// flat — which is acceptable and far better than leaked
		// markup; preserving depth is a future IR change.
		if h, ok := atxHeading(line); ok {
			startSection(h)
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()
	if current.Heading != "" || len(current.Blocks) > 0 {
		sections = append(sections, current)
	}
	return sections
}

// atxHeading reports whether line is a markdown ATX heading of level
// 2–6 (## … ######) and, if so, returns the trimmed heading text.
// Level 1 (#) is intentionally not a section boundary — the page
// title is set separately and the system prompt forbids #.
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
