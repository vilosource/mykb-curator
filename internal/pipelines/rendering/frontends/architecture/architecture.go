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
// Scope: prose sections are LLM-synthesised from kb: sources plus
// any non-kb source a configured resolver can ground (today: the
// read-only git: resolver); render:table is rendered
// deterministically (kb rows + resolver rows; a declared "pending"
// row for schemes with no resolver — cmd/ssh/az until slice 4b);
// render:child-index emits an empty, position-correct placeholder
// the cluster orchestrator fills with the topic's children. Source
// contents are always declared, never fabricated.
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
	"github.com/vilosource/mykb-curator/internal/sources"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Frontend renders an architecture/runbook/integration DocPage.
type Frontend struct {
	llm       llm.Client
	model     string
	resolvers map[string]sources.Resolver // by scheme; nil = none
}

// New constructs a Frontend bound to an LLM client + model. Optional
// non-kb source resolvers (today: the read-only git: resolver) are
// keyed by scheme; a scheme with no resolver keeps the honest
// "pending" placeholder rather than fabricating.
func New(client llm.Client, model string, resolvers ...sources.Resolver) *Frontend {
	m := make(map[string]sources.Resolver, len(resolvers))
	for _, r := range resolvers {
		if r != nil {
			m[r.Scheme()] = r
		}
	}
	return &Frontend{llm: client, model: model, resolvers: m}
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
		secHash := hashStr(page.Page + "\x00" + sec.Title + "\x00" + sec.Intent + "\x00" + rawSources(sec.Sources))
		switch sec.Render {
		case "child-index":
			// The list of children is cluster knowledge, not page
			// knowledge — emit an empty, position-correct placeholder
			// the cluster fills. Owning section ORDER here (one
			// place) while the cluster injects sibling/child data
			// keeps the two concerns cleanly separated.
			doc.Sections = append(doc.Sections, ir.Section{
				Heading: sec.Title,
				Blocks: []ir.Block{ir.IndexBlock{
					Prov: ir.Provenance{SpecSection: ChildIndexProv, InputHash: secHash},
				}},
			})
			continue
		case "table":
			tbl, err := f.tableFromSources(ctx, sec, snap, secHash)
			if err != nil {
				return ir.Document{}, fmt.Errorf("architecture: section %q: %w", sec.Title, err)
			}
			doc.Sections = append(doc.Sections, ir.Section{
				Heading: sec.Title,
				Blocks:  []ir.Block{tbl},
			})
			continue
		}

		kbDigest, nonKB, err := f.resolveSources(ctx, sec.Sources, snap)
		if err != nil {
			return ir.Document{}, fmt.Errorf("architecture: section %q: %w", sec.Title, err)
		}
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

		body := mdir.Parse(strings.TrimSpace(resp.Text), "architecture", secHash)
		doc.Sections = append(doc.Sections, foldSection(sec.Title, body, secHash)...)
	}
	return doc, nil
}

// ChildIndexProv marks an empty IndexBlock the cluster orchestrator
// must fill with the topic's children. The frontend owns section
// position; the cluster owns the child list.
const ChildIndexProv = "architecture-child-index"

// tableFromSources renders a render:table section deterministically
// (no LLM): kb sources expand to one row per entry; a non-kb scheme
// with a configured resolver (today: git:) expands to that
// resolver's rows; a non-kb scheme with no resolver produces a
// single honest "pending" row — declared, never fabricated.
func (f *Frontend) tableFromSources(ctx context.Context, sec docspec.DocSection, snap kb.Snapshot, hash string) (ir.TableBlock, error) {
	tb := ir.TableBlock{
		Columns: []string{"Type", "Ref", "Summary"},
		Prov:    ir.Provenance{SpecSection: "architecture-table", InputHash: hash},
	}
	for _, s := range sec.Sources {
		if s.Scheme != "kb" {
			if res, ok, err := f.resolveNonKB(ctx, s); err != nil {
				return ir.TableBlock{}, err
			} else if ok {
				tb.Rows = append(tb.Rows, res.Rows...)
				continue
			}
			tb.Rows = append(tb.Rows, []string{
				s.Scheme, s.Spec, "pending — no resolver configured for this scheme",
			})
			continue
		}
		a, entries, ok := ResolveKB(snap, s)
		if !ok {
			continue
		}
		for _, e := range entries {
			tb.Rows = append(tb.Rows, []string{e.Type, a.ID + "/" + e.ID, firstLine(e.Text)})
		}
	}
	return tb, nil
}

// resolveNonKB consults the configured resolver for a non-kb source.
// ok=false means no resolver / resolver declined (keep pending); a
// non-nil error is a hard failure that must abort the page.
func (f *Frontend) resolveNonKB(ctx context.Context, s docspec.Source) (sources.Resolved, bool, error) {
	r, has := f.resolvers[s.Scheme]
	if !has {
		return sources.Resolved{}, false, nil
	}
	return r.Resolve(ctx, s)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// foldSection turns the LLM's parsed output into IR sections under
// the declared section title. The model's FIRST chunk always becomes
// the declared section's body — even if the model leaked a leading
// sub-heading (the spec-declared title is authoritative; its heading
// is discarded, never the content). Any further sub-headings the
// model added become flattened sibling sections (content + label
// preserved, never leaked markup). A section the model produced prose
// for is therefore never reported empty; only genuinely empty output
// yields the visible gap marker, never a silently dropped section.
func foldSection(title string, parsed []ir.Section, hash string) []ir.Section {
	out := []ir.Section{{Heading: title}}
	for i, p := range parsed {
		if i == 0 {
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

// resolveSources returns the grounding digest (kb areas + any
// resolver-resolved non-kb sources, e.g. git:) and the list of
// non-kb source Raw strings still unresolved (no resolver) so the
// prompt can declare them as pending without fabricating.
func (f *Frontend) resolveSources(ctx context.Context, srcs []docspec.Source, snap kb.Snapshot) (string, []string, error) {
	var digest strings.Builder
	var nonKB []string
	for _, s := range srcs {
		if s.Scheme != "kb" {
			res, ok, err := f.resolveNonKB(ctx, s)
			if err != nil {
				return "", nil, err
			}
			if ok {
				digest.WriteString(res.Digest)
				if !strings.HasSuffix(res.Digest, "\n") {
					digest.WriteByte('\n')
				}
				digest.WriteByte('\n')
				continue
			}
			nonKB = append(nonKB, s.Raw)
			continue
		}
		a, entries, ok := ResolveKB(snap, s)
		if !ok {
			continue
		}
		writeKBDigest(&digest, a, entries)
	}
	return digest.String(), nonKB, nil
}

// writeKBDigest is the single source of truth for the kb grounding
// format. Synthesis (resolveSources) and the report-only Judge
// (SectionGrounding) both go through it, so the Judge verifies
// claims against byte-identical text to what synthesis was given.
func writeKBDigest(sb *strings.Builder, a *kb.Area, entries []kb.Entry) {
	fmt.Fprintf(sb, "### Area: %s — %s\n", a.ID, a.Name)
	if a.Summary != "" {
		fmt.Fprintf(sb, "Summary: %s\n", a.Summary)
	}
	for _, e := range entries {
		fmt.Fprintf(sb, "- [%s/%s] %s\n", e.Type, e.ID, e.Text)
		if e.Why != "" {
			fmt.Fprintf(sb, "    Why: %s\n", e.Why)
		}
	}
	sb.WriteByte('\n')
}

// SectionGrounding returns the kb grounding digest for a section's
// `kb:` sources — the exact text the architecture synthesis is
// grounded in — so the report-only Judge can verify organisation-
// specific claims against the real sources instead of guessing.
//
// Non-kb (resolver, e.g. git:) grounding is intentionally out of
// scope here: kb is the grounding whose absence made the Judge
// false-positive kb-backed facts. Extend when a non-kb-grounded
// section is empirically shown to false-positive.
func SectionGrounding(snap kb.Snapshot, sec docspec.DocSection) string {
	var sb strings.Builder
	for _, s := range sec.Sources {
		if s.Scheme != "kb" {
			continue
		}
		a, entries, ok := ResolveKB(snap, s)
		if !ok {
			continue
		}
		writeKBDigest(&sb, a, entries)
	}
	return sb.String()
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
		fmt.Fprintf(&sb, "Section contract (you MUST satisfy every item it calls for): %s\n", sec.Intent)
	}
	sb.WriteString("\nRequirements — all mandatory:\n")
	sb.WriteString("- Treat the contract as an explicit checklist. Deliver every distinct item it calls for: when it asks for a procedure, enumerate the concrete steps or phases; when it asks for a model or list, lay it out in full. Do not stop at a preamble or a generic overview.\n")
	sb.WriteString("- The substance MUST be the organisation-specific specifics in the supplied knowledge base content (versions, topology, hosts, decisions, procedures). Surface them; never substitute generic, textbook description of the technology for the organisation's own details.\n")
	sb.WriteString("- If and only if the supplied sources do not support a required contract item, state that explicitly and briefly for that item as `_Not covered by current sources: <item>._` Never omit a required item silently and never invent organisation specifics to fill it.\n")
	sb.WriteString("- Before finishing, reconcile your text against the contract item by item: EACH item must be either delivered with the supplied specifics OR carry its own `_Not covered by current sources: <item>._` line. Writing fluent but off-contract prose about the topic in general — covering adjacent material the grounding happens to be rich in while a contract item goes unaddressed — is the single most common failure here and is NOT acceptable; a present-in-grounding item must never be left out because other material was easier to write.\n\n")
	sb.WriteString("Do NOT output the section heading. Do NOT use # or ## headings. ")
	sb.WriteString("Use ### sparingly only for genuine sub-points. You MAY include one mermaid fenced block if it aids understanding.\n\n")
	if strings.TrimSpace(kbDigest) != "" {
		sb.WriteString("Ground every organisation-specific claim ONLY in the following knowledge base content. Do not invent organisation specifics.\n\n")
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
- Deliver the section's full declared contract: every item it calls for, built from the supplied sources' specifics. Generic technology background never substitutes for the organisation's own details, and a required item the sources cannot support must be flagged explicitly — never silently dropped or invented.
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
