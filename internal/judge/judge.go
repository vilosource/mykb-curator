// Package judge is the report-only output reviewer (adoption issues
// #1 + #2).
//
// For each narrative section of a rendered page it asks an LLM
// whether the section satisfies its spec-declared intent, and flags
// any organisation-specific claim not traceable to the supplied
// grounding. It NEVER blocks a push — the verdict is advisory,
// surfaced in the run report. Promoting it to a gate is a deliberate
// later policy change, not a code path here.
//
// Only prose-bearing sections that declare an intent are judged.
// Structural sections (child-index, related, categories, tables) and
// the cluster's generated cross-links carry no narrative contract,
// so they are out of scope by construction.
//
// Deterministic given the injected LLM. An unparseable LLM response
// yields an Inconclusive verdict — never a crash and never a
// silent pass.
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Verdict is the review of one section.
type Verdict struct {
	Section          string
	Pass             bool
	Inconclusive     bool // LLM response could not be parsed
	Reason           string
	UngroundedClaims []string
}

// Report is the per-page review. AllPass is false if any judged
// section failed or was inconclusive.
type Report struct {
	Page     string
	Verdicts []Verdict
}

// AllPass reports whether every judged section passed (no failures,
// no inconclusive verdicts). With no judged sections it is true
// (nothing to contradict).
func (r Report) AllPass() bool {
	for _, v := range r.Verdicts {
		if !v.Pass || v.Inconclusive {
			return false
		}
	}
	return true
}

// Judge reviews rendered pages. Report-only.
type Judge struct {
	llm   llm.Client
	model string
}

// New binds a Judge to an LLM client + model.
func New(client llm.Client, model string) *Judge {
	return &Judge{llm: client, model: model}
}

// Review judges every prose-bearing, intent-declaring section of doc
// against page. It returns a Report; it never returns an error for a
// failing verdict (failure is data, not an error). A non-nil error
// means the LLM call itself failed.
func (j *Judge) Review(ctx context.Context, page docspec.DocPage, doc ir.Document) (Report, error) {
	rep := Report{Page: page.Page}
	bodies := proseByHeading(doc)

	for _, sec := range page.Sections {
		if sec.Render != "" || strings.TrimSpace(sec.Intent) == "" {
			continue // structural or no contract to judge
		}
		body := strings.TrimSpace(bodies[sec.Title])
		if body == "" {
			rep.Verdicts = append(rep.Verdicts, Verdict{
				Section: sec.Title, Pass: false,
				Reason: "section is missing or empty in the rendered page",
			})
			continue
		}
		resp, err := j.llm.Complete(ctx, llm.Request{
			Model:     j.model,
			System:    systemPrompt,
			Prompt:    composePrompt(page, sec, body),
			MaxTokens: 1024,
		})
		if err != nil {
			return Report{}, fmt.Errorf("judge: section %q: llm: %w", sec.Title, err)
		}
		rep.Verdicts = append(rep.Verdicts, parseVerdict(sec.Title, resp.Text))
	}
	return rep, nil
}

// proseByHeading flattens each section's ProseBlocks (the only
// narrative-bearing block) into one string keyed by heading.
func proseByHeading(doc ir.Document) map[string]string {
	out := make(map[string]string, len(doc.Sections))
	for _, s := range doc.Sections {
		var b strings.Builder
		for _, blk := range s.Blocks {
			if pb, ok := blk.(ir.ProseBlock); ok {
				b.WriteString(pb.Text)
				b.WriteByte('\n')
			}
		}
		if t := strings.TrimSpace(b.String()); t != "" {
			out[s.Heading] = t
		}
	}
	return out
}

const systemPrompt = `You are a documentation reviewer. You judge whether a wiki section delivers on its stated intent and whether every organisation-specific claim is grounded in the supplied source material.

Reply with ONLY a JSON object, no prose around it:
{"pass": <bool>, "reason": "<one sentence>", "ungrounded_claims": ["<verbatim claim not supported by the sources>", ...]}

pass=false if the section does not satisfy the intent OR contains an organisation-specific claim (a version, host, topology, decision, credential) absent from the sources. General, well-known background about the technology itself is not an ungrounded claim. ungrounded_claims is [] when there are none.`

func composePrompt(page docspec.DocPage, sec docspec.DocSection, body string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Page intent: %s\n\n", page.Intent)
	fmt.Fprintf(&sb, "Section: %s\n", sec.Title)
	fmt.Fprintf(&sb, "Section intent (the contract): %s\n\n", sec.Intent)
	if len(sec.Sources) > 0 {
		sb.WriteString("Declared sources for this section:\n")
		for _, s := range sec.Sources {
			fmt.Fprintf(&sb, "- %s\n", s.Raw)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("Rendered section text:\n---\n")
	sb.WriteString(body)
	sb.WriteString("\n---\n")
	return sb.String()
}

type verdictJSON struct {
	Pass             bool     `json:"pass"`
	Reason           string   `json:"reason"`
	UngroundedClaims []string `json:"ungrounded_claims"`
}

// parseVerdict tolerantly extracts the JSON object from the LLM
// response. Unparseable → Inconclusive (never a silent pass).
func parseVerdict(section, raw string) Verdict {
	obj, ok := extractJSONObject(raw)
	if !ok {
		return Verdict{
			Section: section, Pass: false, Inconclusive: true,
			Reason: "judge response was not parseable JSON",
		}
	}
	var vj verdictJSON
	if err := json.Unmarshal([]byte(obj), &vj); err != nil {
		return Verdict{
			Section: section, Pass: false, Inconclusive: true,
			Reason: "judge response was not parseable JSON",
		}
	}
	return Verdict{
		Section:          section,
		Pass:             vj.Pass,
		Reason:           strings.TrimSpace(vj.Reason),
		UngroundedClaims: vj.UngroundedClaims,
	}
}

// extractJSONObject returns the first balanced {...} run, ignoring
// any prose or code fences the LLM wrapped it in.
func extractJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// inside string: ignore braces
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}
