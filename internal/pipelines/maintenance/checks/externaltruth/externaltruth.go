// Package externaltruth implements the ExternalTruthCheck
// (DESIGN.md §6.1, the "Expensive (LLM + web)" row): for kb facts in
// opted-in areas, web-search the claim and ask the LLM whether the
// results still support it, emitting Verify / Deprecate proposals.
//
// Funding gate (DESIGN.md §6.4 — "Pull, not push"): the check only
// ever touches areas explicitly opted in by a spec's
// `fact_check: external_truth`. Areas nobody opted into are never
// searched and never cost a token. An empty opt-in set makes Run a
// total no-op. Cost discipline also means: no search results → no
// LLM call (nothing to compare against).
//
// Deterministic given its injected collaborators: the WebSearch and
// llm.Client are interfaces, exercised with fakes / replay in tests
// (same boundary pattern as the editorial frontend's LLM client).
package externaltruth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

// Result is one web-search hit.
type Result struct {
	Title   string
	URL     string
	Snippet string
}

// WebSearch is the web-search boundary. Deliberately narrow; the
// concrete provider adapter is wired by the composition root.
type WebSearch interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

// Check is the external-truth maintenance.Check impl.
type Check struct {
	optedIn map[string]bool
	web     WebSearch
	llm     llm.Client
	model   string
	now     func() time.Time // injected for deterministic evidence timestamps
}

// New constructs the check. optedInAreas is the set of area IDs that
// a spec opted into external-truth checking (the funding gate). web
// + llm are the injected collaborators.
func New(optedInAreas []string, web WebSearch, client llm.Client, model string) *Check {
	set := make(map[string]bool, len(optedInAreas))
	for _, a := range optedInAreas {
		set[a] = true
	}
	return &Check{optedIn: set, web: web, llm: client, model: model, now: time.Now}
}

// Name returns "external-truth".
func (*Check) Name() string { return "external-truth" }

// Run scans opted-in areas' fact entries and proposes Verify /
// Deprecate based on the web+LLM verdict.
func (c *Check) Run(ctx context.Context, snap kb.Snapshot) ([]maintenance.MutationProposal, error) {
	if len(c.optedIn) == 0 {
		return nil, nil // funding gate: nobody opted in → never spend
	}
	var out []maintenance.MutationProposal
	for ai := range snap.Areas {
		area := snap.Areas[ai]
		if !c.optedIn[area.ID] {
			continue // not funded for this area
		}
		for _, e := range area.Entries {
			if e.Type != "fact" || e.Zone == "archived" {
				continue
			}
			p, ok, err := c.checkFact(ctx, area.ID, e)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// checkFact runs one fact through web search + LLM verdict. The
// bool reports whether a proposal was produced (false = skip:
// no results, or an UNCERTAIN verdict).
func (c *Check) checkFact(ctx context.Context, areaID string, e kb.Entry) (maintenance.MutationProposal, bool, error) {
	results, err := c.web.Search(ctx, e.Text)
	if err != nil {
		return maintenance.MutationProposal{}, false, fmt.Errorf("external-truth: web search %q: %w", e.ID, err)
	}
	if len(results) == 0 {
		return maintenance.MutationProposal{}, false, nil // nothing to compare → don't spend an LLM call
	}

	resp, err := c.llm.Complete(ctx, llm.Request{
		Model:  c.model,
		System: verdictSystemPrompt,
		Prompt: buildPrompt(e.Text, results),
	})
	if err != nil {
		return maintenance.MutationProposal{}, false, fmt.Errorf("external-truth: llm verdict %q: %w", e.ID, err)
	}

	verdict, rationale := parseVerdict(resp.Text)
	primary := results[0]
	evidence := map[string]string{
		"method":      "external-truth",
		"source":      primary.URL,
		"verified_at": c.now().UTC().Format(time.RFC3339),
	}
	switch verdict {
	case "CONFIRMED":
		return maintenance.MutationProposal{
			Kind:     maintenance.ProposalVerify,
			Area:     areaID,
			ID:       e.ID,
			Text:     e.Text,
			Source:   c.Name(),
			Reason:   "web+LLM external-truth check confirmed the claim: " + rationale,
			Evidence: evidence,
		}, true, nil
	case "CONTRADICTED":
		return maintenance.MutationProposal{
			Kind:     maintenance.ProposalDeprecate,
			Area:     areaID,
			ID:       e.ID,
			Text:     e.Text,
			Source:   c.Name(),
			Reason:   "web+LLM external-truth check contradicted the claim: " + rationale,
			Evidence: evidence,
		}, true, nil
	default:
		return maintenance.MutationProposal{}, false, nil // UNCERTAIN / unparseable → no flood
	}
}

const verdictSystemPrompt = "You are a fact-checking assistant. Given a knowledge-base claim and web search results, decide whether the results SUPPORT, CONTRADICT, or are INCONCLUSIVE about the claim. Reply with exactly one of CONFIRMED, CONTRADICTED, or UNCERTAIN as the first word, followed by a one-sentence rationale."

func buildPrompt(claim string, results []Result) string {
	var b strings.Builder
	b.WriteString("CLAIM:\n")
	b.WriteString(claim)
	b.WriteString("\n\nWEB SEARCH RESULTS:\n")
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s — %s\n   %s\n", i+1, r.Title, r.URL, r.Snippet)
	}
	b.WriteString("\nVerdict:")
	return b.String()
}

// parseVerdict extracts the leading verdict token and the rest as
// rationale. Tolerant of "CONFIRMED:", "CONTRADICTED —", etc.
func parseVerdict(s string) (verdict, rationale string) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", ""
	}
	fields := strings.FieldsFunc(t, func(r rune) bool {
		return r == ' ' || r == ':' || r == '\n' || r == '\t' || r == '-' || r == '—'
	})
	if len(fields) == 0 {
		return "", ""
	}
	verdict = strings.ToUpper(fields[0])
	rationale = strings.TrimSpace(strings.TrimLeft(t[len(fields[0]):], " :—-\n\t"))
	return verdict, rationale
}
