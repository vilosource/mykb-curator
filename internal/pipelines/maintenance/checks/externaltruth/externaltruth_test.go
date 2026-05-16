package externaltruth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance/checks/externaltruth"
)

// fakeWeb records queries and returns canned results.
type fakeWeb struct {
	results []externaltruth.Result
	queries []string
	err     error
}

func (f *fakeWeb) Search(_ context.Context, q string) ([]externaltruth.Result, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

// fakeLLM returns a verdict keyed by whether the prompt contains a
// marker string, so different facts get different verdicts
// deterministically.
type fakeLLM struct {
	verdict string
	calls   int
}

func (f *fakeLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	f.calls++
	return llm.Response{Text: f.verdict}, nil
}

func snap(areaID string, entries ...kb.Entry) kb.Snapshot {
	return kb.Snapshot{Areas: []kb.Area{{ID: areaID, Entries: entries}}}
}

func fact(id, text string) kb.Entry {
	return kb.Entry{ID: id, Type: "fact", Text: text, Zone: "active"}
}

func TestName(t *testing.T) {
	c := externaltruth.New([]string{"vault"}, &fakeWeb{}, &fakeLLM{}, "m")
	if c.Name() != "external-truth" {
		t.Errorf("Name() = %q, want external-truth", c.Name())
	}
}

func TestFundingGate_NonOptedInArea_NoSpend(t *testing.T) {
	web := &fakeWeb{results: []externaltruth.Result{{URL: "https://x"}}}
	model := &fakeLLM{verdict: "CONFIRMED"}
	// opted-in set is {vault}; snapshot only has area "networking".
	c := externaltruth.New([]string{"vault"}, web, model, "m")

	props, err := c.Run(context.Background(), snap("networking", fact("f1", "claim")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 0 {
		t.Errorf("expected no proposals for non-opted-in area, got %d", len(props))
	}
	if len(web.queries) != 0 || model.calls != 0 {
		t.Errorf("funding gate breached: web=%d llm=%d (must be 0 for non-opted-in)", len(web.queries), model.calls)
	}
}

func TestEmptyOptInSet_NeverSpends(t *testing.T) {
	web := &fakeWeb{}
	model := &fakeLLM{verdict: "CONFIRMED"}
	c := externaltruth.New(nil, web, model, "m")
	props, err := c.Run(context.Background(), snap("vault", fact("f1", "claim")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 0 || len(web.queries) != 0 || model.calls != 0 {
		t.Errorf("empty opt-in must be a total no-op; props=%d web=%d llm=%d", len(props), len(web.queries), model.calls)
	}
}

func TestConfirmed_EmitsVerify(t *testing.T) {
	web := &fakeWeb{results: []externaltruth.Result{{Title: "T", URL: "https://src", Snippet: "S"}}}
	model := &fakeLLM{verdict: "CONFIRMED: matches the cited release notes"}
	c := externaltruth.New([]string{"vault"}, web, model, "m")

	props, err := c.Run(context.Background(), snap("vault", fact("f1", "Vault 1.17 is deployed")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("want 1 proposal, got %d", len(props))
	}
	p := props[0]
	if p.Kind != maintenance.ProposalVerify {
		t.Errorf("Kind = %v, want Verify", p.Kind)
	}
	if p.Area != "vault" || p.ID != "f1" || p.Source != "external-truth" {
		t.Errorf("proposal misrouted: %+v", p)
	}
	if p.Evidence["source"] != "https://src" || p.Evidence["method"] != "external-truth" {
		t.Errorf("evidence missing source/method: %+v", p.Evidence)
	}
}

func TestContradicted_EmitsDeprecate(t *testing.T) {
	web := &fakeWeb{results: []externaltruth.Result{{URL: "https://src"}}}
	model := &fakeLLM{verdict: "CONTRADICTED — current version is 1.20, not 1.17"}
	c := externaltruth.New([]string{"vault"}, web, model, "m")

	props, err := c.Run(context.Background(), snap("vault", fact("f1", "Vault 1.17 is deployed")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 1 || props[0].Kind != maintenance.ProposalDeprecate {
		t.Fatalf("want 1 Deprecate, got %+v", props)
	}
	if !strings.Contains(props[0].Reason, "1.20") {
		t.Errorf("reason should carry the LLM rationale, got %q", props[0].Reason)
	}
}

func TestUncertain_NoProposal(t *testing.T) {
	web := &fakeWeb{results: []externaltruth.Result{{URL: "https://src"}}}
	model := &fakeLLM{verdict: "UNCERTAIN: search results inconclusive"}
	c := externaltruth.New([]string{"vault"}, web, model, "m")
	props, err := c.Run(context.Background(), snap("vault", fact("f1", "claim")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 0 {
		t.Errorf("UNCERTAIN must not flood proposals, got %d", len(props))
	}
}

func TestOnlyFactsChecked_ArchivedSkipped(t *testing.T) {
	web := &fakeWeb{results: []externaltruth.Result{{URL: "https://s"}}}
	model := &fakeLLM{verdict: "CONFIRMED"}
	c := externaltruth.New([]string{"vault"}, web, model, "m")

	s := kb.Snapshot{Areas: []kb.Area{{ID: "vault", Entries: []kb.Entry{
		{ID: "d1", Type: "decision", Text: "a decision", Zone: "active"},
		{ID: "f-arch", Type: "fact", Text: "old", Zone: "archived"},
		fact("f-ok", "live claim"),
	}}}}
	props, err := c.Run(context.Background(), s)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 1 || props[0].ID != "f-ok" {
		t.Errorf("only the live fact should be checked; got %+v", props)
	}
}

func TestNoSearchResults_NoSpendOnLLM(t *testing.T) {
	web := &fakeWeb{results: nil} // nothing found
	model := &fakeLLM{verdict: "CONFIRMED"}
	c := externaltruth.New([]string{"vault"}, web, model, "m")
	props, err := c.Run(context.Background(), snap("vault", fact("f1", "claim")))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(props) != 0 {
		t.Errorf("no results → no verdict → no proposal, got %d", len(props))
	}
	if model.calls != 0 {
		t.Errorf("must not call the LLM when web search returned nothing (cost discipline); calls=%d", model.calls)
	}
}
