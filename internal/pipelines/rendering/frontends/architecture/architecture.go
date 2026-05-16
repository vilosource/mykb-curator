// Package architecture implements the doc-spec-native frontend that
// produces human-curated, narrative architecture pages (the kind an
// engineer hand-writes), NOT a 1:1 kb dump.
//
// It consumes a docspec.DocPage: for each prose section it resolves
// that section's declared kb sources, then asks the LLM to write the
// section body to satisfy the section's intent, in a tone driven by
// the page audience. The hard-won markdown→IR handling is shared via
// the mdir package.
//
// Slice 2 scope: prose sections with kb: sources. render:table and
// render:child-index sections, and non-kb source schemes
// (git/cmd/ssh/file), are recognised but not resolved here —
// cluster orchestration (slice 3) and the tool-using resolvers
// (slice 4) own those. They are skipped without error, never
// fabricated.
package architecture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/mdir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Frontend renders an architecture/runbook/integration DocPage.
type Frontend struct {
	llm   llm.Client
	model string
}

// New constructs a Frontend bound to an LLM client + model.
func New(client llm.Client, model string) *Frontend {
	return &Frontend{llm: client, model: model}
}

// Name returns "architecture-frontend".
func (*Frontend) Name() string { return "architecture-frontend" }

// Render produces the IR Document for one DocPage. Pure given the
// LLM (inject a deterministic client in tests).
func (f *Frontend) Render(ctx context.Context, page docspec.DocPage, snap kb.Snapshot) (ir.Document, error) {
	doc := ir.Document{
		Frontmatter: ir.Frontmatter{
			Title:    page.Page,
			SpecHash: hashStr(page.Page + "\x00" + page.Intent),
			KBCommit: snap.Commit,
		},
	}
	sys := persona(page.Audience)

	for i := range page.Sections {
		sec := page.Sections[i]
		switch sec.Render {
		case "child-index", "table":
			// Owned by cluster orchestration (slice 3) / structured
			// resolvers (slice 4). Skip here — never fabricate.
			continue
		}

		kbDigest, nonKB := f.resolveSources(sec.Sources, snap)
		prompt := composeSectionPrompt(page, sec, kbDigest, nonKB)
		resp, err := f.llm.Complete(ctx, llm.Request{
			Model:     f.model,
			System:    sys,
			Prompt:    prompt,
			MaxTokens: 3072,
		})
		if err != nil {
			return ir.Document{}, fmt.Errorf("architecture: section %q: llm: %w", sec.Title, err)
		}

		hash := hashStr(page.Page + "\x00" + sec.Title + "\x00" + sec.Intent + "\x00" + rawSources(sec.Sources))
		body := mdir.Parse(strings.TrimSpace(resp.Text), "architecture", hash)
		doc.Sections = append(doc.Sections, foldSection(sec.Title, body, hash)...)
	}
	return doc, nil
}

// foldSection turns the LLM's parsed output into IR sections under
// the declared section title. The first no-heading chunk becomes the
// declared section's body; any sub-headings the LLM added (despite
// instructions) become flattened sibling sections (content + label
// preserved, never leaked markup). Empty output yields a visible
// gap marker, not a silently dropped section.
func foldSection(title string, parsed []ir.Section, hash string) []ir.Section {
	out := []ir.Section{{Heading: title}}
	for i, p := range parsed {
		if i == 0 && p.Heading == "" {
			out[0].Blocks = p.Blocks
			continue
		}
		out = append(out, p)
	}
	if len(out[0].Blocks) == 0 && len(out) == 1 {
		out[0].Blocks = []ir.Block{ir.ProseBlock{
			Text: "_No content was available for this section from the declared sources._",
			Prov: ir.Provenance{SpecSection: "architecture-gap", InputHash: hash},
		}}
	}
	return out
}

// resolveSources returns a kb digest string for kb: sources and the
// list of non-kb source Raw strings (declared but not machine-
// resolvable yet).
func (f *Frontend) resolveSources(srcs []docspec.Source, snap kb.Snapshot) (string, []string) {
	var digest strings.Builder
	var nonKB []string
	for _, s := range srcs {
		if s.Scheme != "kb" {
			nonKB = append(nonKB, s.Raw)
			continue
		}
		a, entries, ok := ResolveKB(snap, s)
		if !ok {
			continue
		}
		fmt.Fprintf(&digest, "### Area: %s — %s\n", a.ID, a.Name)
		if a.Summary != "" {
			fmt.Fprintf(&digest, "Summary: %s\n", a.Summary)
		}
		for _, e := range entries {
			fmt.Fprintf(&digest, "- [%s/%s] %s\n", e.Type, e.ID, e.Text)
			if e.Why != "" {
				fmt.Fprintf(&digest, "    Why: %s\n", e.Why)
			}
		}
		digest.WriteByte('\n')
	}
	return digest.String(), nonKB
}

// ResolveKB resolves a `kb:area=<id> [tag=a,b] [zone=x,y]` source
// against a snapshot. Returns the area, the filtered entries, and ok.
func ResolveKB(snap kb.Snapshot, s docspec.Source) (*kb.Area, []kb.Entry, bool) {
	if s.Scheme != "kb" {
		return nil, nil, false
	}
	var areaID string
	tagSet := map[string]bool{}
	zoneSet := map[string]bool{}
	for _, tok := range strings.Fields(s.Spec) {
		k, v, found := strings.Cut(tok, "=")
		if !found {
			continue
		}
		switch k {
		case "area":
			areaID = v
		case "tag":
			for _, t := range strings.Split(v, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tagSet[t] = true
				}
			}
		case "zone":
			for _, z := range strings.Split(v, ",") {
				if z = strings.TrimSpace(z); z != "" {
					zoneSet[z] = true
				}
			}
		}
	}
	if areaID == "" {
		return nil, nil, false
	}
	a := snap.Area(areaID)
	if a == nil {
		return nil, nil, false
	}
	if len(tagSet) == 0 && len(zoneSet) == 0 {
		return a, a.Entries, true
	}
	var out []kb.Entry
	for _, e := range a.Entries {
		if len(zoneSet) > 0 && !zoneSet[e.Zone] {
			continue
		}
		if len(tagSet) > 0 {
			hit := false
			for _, t := range e.Tags {
				if tagSet[t] {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		out = append(out, e)
	}
	return a, out, true
}

func composeSectionPrompt(page docspec.DocPage, sec docspec.DocSection, kbDigest string, nonKB []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Page: %s\nPage intent: %s\n\n", page.Page, page.Intent)
	fmt.Fprintf(&sb, "Write ONLY the prose body for the section titled %q.\n", sec.Title)
	if sec.Intent != "" {
		fmt.Fprintf(&sb, "This section must convey: %s\n", sec.Intent)
	}
	sb.WriteString("Do NOT output the section heading. Do NOT use # or ## headings. ")
	sb.WriteString("Use ### sparingly only for genuine sub-points. You MAY include one mermaid fenced block if it aids understanding.\n\n")
	if strings.TrimSpace(kbDigest) != "" {
		sb.WriteString("Ground every organisation-specific claim in the following knowledge base content. Do not invent organisation specifics.\n\n")
		sb.WriteString(kbDigest)
	} else {
		sb.WriteString("(No kb content resolved for this section's sources.)\n")
	}
	if len(nonKB) > 0 {
		fmt.Fprintf(&sb, "\nDeclared sources not yet machine-resolvable (mention only if essential, do not fabricate their contents): %s\n", strings.Join(nonKB, ", "))
	}
	return sb.String()
}

const personaBase = `You are an infrastructure documentation writer. You produce accurate, well-structured prose for an engineering wiki.

Rules:
- Markdown only. No preamble, no postscript, no wrapping code fence.
- No # or ## headings (the page/section headings are set for you).
- Ground every organisation-specific claim (versions, hosts, topology, decisions) ONLY in the supplied kb content. Do not invent organisation specifics.
- Mermaid (if used): one statement per line; diagram type on its own first line; no parentheses/slashes/colons/backticks inside node labels; quote subgraph titles; keep it small.`

func persona(audience string) string {
	switch audience {
	case "newcomer":
		return personaBase + "\n- Audience: a reader with ZERO prior knowledge. Briefly explain the concepts needed before the specifics."
	case "llm-reference":
		return personaBase + "\n- Audience: a machine/reference reader. Be terse, dense, and exhaustive over the supplied facts; minimal narrative."
	default: // human-operator
		return personaBase + "\n- Audience: an engineer operating this system. Be precise and operational: where it runs, how it is built, how to reach it."
	}
}

func rawSources(srcs []docspec.Source) string {
	parts := make([]string, 0, len(srcs))
	for _, s := range srcs {
		parts = append(parts, s.Raw)
	}
	return strings.Join(parts, "|")
}

func hashStr(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
