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
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/mdir"
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
		Sections: mdir.Parse(resp.Text, "editorial", spec.Hash),
	}, nil
}

// systemPrompt is the persona instruction sent on every editorial
// frontend call. Kept terse — the per-page intent lives in the user
// message.
const systemPrompt = `You are a wiki editor for an engineering organisation. You write clear wiki pages that capture institutional knowledge and are understandable by a reader with ZERO prior knowledge of the subject.

Style:
- Markdown output only. No preamble, no postscript. Do not wrap the whole response in a code fence.
- Use ## (and ### for sub-topics) for headings. Do not use # — the page title is set separately.
- Lead a newcomer in: briefly explain what the technology is and the concepts needed to understand it, THEN the organisation's specifics.
- Prefer clear prose paragraphs. Include diagrams where they aid understanding using fenced ` + "```mermaid" + ` blocks (flowchart/sequence/etc.) — diagrams are rendered to images automatically.
- Mermaid rules (diagrams that break do not render — follow exactly):
  * One statement per line. Put the diagram type (e.g. graph TD) on its own first line; never put node/edge statements on the same line as it or as a subgraph.
  * Node/edge label text must contain NO parentheses, slashes, colons or backticks. Rephrase instead (write "Leader" not "(Leader)", "vault dot acme dot internal" or just "Vault endpoint" not a URL).
  * Quote every subgraph title: subgraph "My Title". Keep titles short and plain.
  * Keep diagrams small (a dozen nodes max); prefer several simple diagrams over one dense one.
- Ground every organisation-specific claim (versions, topology, decisions) in the supplied kb content; do not invent organisation specifics. General, well-known background about the technology itself may be explained to orient the reader.`

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

// Markdown→IR parsing now lives in the shared mdir package (reused
// by every LLM-driven frontend). editorial keeps its provenance
// label via the "editorial" prefix.
