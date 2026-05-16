// Package applystylerules implements the ApplyStyleRules pass
// (DESIGN.md §5.4): deterministic house-style enforcement.
//
// The pass is a composition of independent Rule strategies. Adding a
// new house-style rule is a new Rule implementation, not an edit to
// the pass (Open/Closed) — the same shape as the backend/frontend
// adapter Strategies elsewhere in the codebase.
//
// Rules only touch human-readable prose: ProseBlock text, Callout
// bodies, and Section headings. Structural machine content
// (MachineBlock bodies, KB refs, table cells, markers) is left
// untouched — rewriting it could corrupt IDs / generated structure,
// and house style is about the words humans read.
//
// Config-driven: the composition root builds the Rule list from the
// per-wiki config's `style:` block. This package stays
// config-agnostic (dependency inversion) — it knows Rules, not YAML.
package applystylerules

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Rule is one deterministic, pure house-style transformation over a
// Document. Implementations must be order-independent of map
// iteration and produce identical output for identical input.
type Rule interface {
	Name() string
	Apply(ir.Document) ir.Document
}

// ApplyStyleRules is the Pass implementation: it threads the
// Document through its Rules in the order given to New.
type ApplyStyleRules struct {
	rules []Rule
}

// New constructs the pass over the given rules (applied in order).
// Zero rules = identity pass.
func New(rules ...Rule) *ApplyStyleRules { return &ApplyStyleRules{rules: rules} }

// Name returns "apply-style-rules".
func (*ApplyStyleRules) Name() string { return "apply-style-rules" }

// Apply runs each rule in sequence. Deterministic and pure; the
// context is accepted for Pass-interface conformance only.
func (p *ApplyStyleRules) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	for _, r := range p.rules {
		doc = r.Apply(doc)
	}
	return doc, nil
}

// mapProse walks every prose-bearing field (ProseBlock text, Callout
// body, Section heading) and replaces it with fn's output. Shared by
// the text-oriented rules so they agree on exactly which content is
// "prose".
func mapProse(doc ir.Document, fn func(string) string) ir.Document {
	for si := range doc.Sections {
		doc.Sections[si].Heading = fn(doc.Sections[si].Heading)
		blocks := doc.Sections[si].Blocks
		for bi := range blocks {
			switch b := blocks[bi].(type) {
			case ir.ProseBlock:
				b.Text = fn(b.Text)
				blocks[bi] = b
			case ir.Callout:
				b.Body = fn(b.Body)
				blocks[bi] = b
			}
		}
	}
	return doc
}

// TerminologyRule canonicalises terms: whole-word, case-sensitive
// replacement of each key with its value.
type TerminologyRule struct {
	// pats is built once at construction, in sorted-key order, so
	// application is independent of Go's map iteration order.
	pats []termPat
}

type termPat struct {
	re   *regexp.Regexp
	repl string
}

// NewTerminologyRule builds a rule from a {wrong: canonical} map.
// Keys are matched on word boundaries so "go"->"Go" never mangles
// "goroutine". Replacements are applied in sorted-key order for
// determinism.
func NewTerminologyRule(repl map[string]string) *TerminologyRule {
	keys := make([]string, 0, len(repl))
	for k := range repl {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pats := make([]termPat, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		pats = append(pats, termPat{
			re:   regexp.MustCompile(`\b` + regexp.QuoteMeta(k) + `\b`),
			repl: repl[k],
		})
	}
	return &TerminologyRule{pats: pats}
}

// Name returns "terminology".
func (*TerminologyRule) Name() string { return "terminology" }

// Apply rewrites prose terms.
func (r *TerminologyRule) Apply(doc ir.Document) ir.Document {
	return mapProse(doc, func(s string) string {
		for _, p := range r.pats {
			s = p.re.ReplaceAllString(s, p.repl)
		}
		return s
	})
}

// HeadingCaseRule normalises Section heading casing.
type HeadingCaseRule struct {
	mode string // "sentence" | "title"
}

// NewHeadingCaseRule builds the rule. mode must be "sentence" or
// "title"; anything else is an error so a typo'd config fails loudly
// rather than silently doing nothing.
func NewHeadingCaseRule(mode string) (*HeadingCaseRule, error) {
	switch mode {
	case "sentence", "title":
		return &HeadingCaseRule{mode: mode}, nil
	default:
		return nil, fmt.Errorf("applystylerules: unknown heading-case mode %q (want \"sentence\" or \"title\")", mode)
	}
}

// Name returns "heading-case".
func (*HeadingCaseRule) Name() string { return "heading-case" }

// Apply recases only Section headings (not body prose).
func (r *HeadingCaseRule) Apply(doc ir.Document) ir.Document {
	for si := range doc.Sections {
		switch r.mode {
		case "sentence":
			doc.Sections[si].Heading = toSentenceCase(doc.Sections[si].Heading)
		case "title":
			doc.Sections[si].Heading = toTitleCase(doc.Sections[si].Heading)
		}
	}
	return doc
}

// toSentenceCase lowercases the whole string then capitalises the
// first letter.
func toSentenceCase(s string) string {
	if s == "" {
		return s
	}
	rs := []rune(strings.ToLower(s))
	rs[0] = []rune(strings.ToUpper(string(rs[0])))[0]
	return string(rs)
}

// toTitleCase capitalises the first letter of each space-separated
// word, leaving the remaining letters as-is (so acronyms a later
// rule produced, e.g. "GitHub", survive).
func toTitleCase(s string) string {
	words := strings.Split(s, " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		rs := []rune(w)
		rs[0] = []rune(strings.ToUpper(string(rs[0])))[0]
		words[i] = string(rs)
	}
	return strings.Join(words, " ")
}
