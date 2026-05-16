package judge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

type seqLLM struct {
	resp []string
	i    int
	seen []llm.Request
	err  error
}

func (s *seqLLM) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	s.seen = append(s.seen, req)
	if s.err != nil {
		return llm.Response{}, s.err
	}
	r := ""
	if s.i < len(s.resp) {
		r = s.resp[s.i]
	}
	s.i++
	return llm.Response{Text: r}, nil
}

func proseDoc(sectionsText map[string]string) ir.Document {
	var d ir.Document
	for h, t := range sectionsText {
		d.Sections = append(d.Sections, ir.Section{
			Heading: h, Blocks: []ir.Block{ir.ProseBlock{Text: t}},
		})
	}
	return d
}

func page() docspec.DocPage {
	return docspec.DocPage{
		Page: "OptiscanGroup/Azure_Infrastructure/Vault_Architecture",
		Kind: "architecture", Intent: "Reader understands Vault.",
		Sections: []docspec.DocSection{
			{Title: "Overview", Intent: "Explain what Vault is and how it is deployed."},
			{Title: "Source Code", Render: "table"},    // structural → skipped
			{Title: "Diagram Only", Intent: ""},        // no contract → skipped
			{Title: "Runbooks", Render: "child-index"}, // structural → skipped
		},
	}
}

func TestReview_JudgesOnlyIntentBearingProseSections(t *testing.T) {
	llmC := &seqLLM{resp: []string{`{"pass": true, "reason": "meets intent", "ungrounded_claims": []}`}}
	doc := proseDoc(map[string]string{
		"Overview":     "Vault is a secrets manager. Deployed as a 5-node Raft cluster.",
		"Source Code":  "irrelevant table prose",
		"Diagram Only": "no intent declared",
	})

	rep, err := New(llmC, "m").Review(context.Background(), page(), doc)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	// Only "Overview" is judged (others are structural or intent-less).
	if len(llmC.seen) != 1 || len(rep.Verdicts) != 1 || rep.Verdicts[0].Section != "Overview" {
		t.Fatalf("only the intent-bearing prose section must be judged: %+v / calls=%d", rep.Verdicts, len(llmC.seen))
	}
	if !rep.Verdicts[0].Pass || !rep.AllPass() {
		t.Errorf("expected pass: %+v", rep.Verdicts[0])
	}
	// the section intent (the contract) must reach the judge.
	if !strings.Contains(llmC.seen[0].Prompt, "Explain what Vault is") {
		t.Errorf("section intent missing from judge prompt:\n%s", llmC.seen[0].Prompt)
	}
}

func TestReview_FlagsUngroundedClaims(t *testing.T) {
	llmC := &seqLLM{resp: []string{
		`Here is my review:
		{"pass": false, "reason": "version not in sources", "ungrounded_claims": ["Vault 1.15.2"]}
		hope that helps`,
	}}
	doc := proseDoc(map[string]string{"Overview": "We run Vault 1.15.2."})

	rep, err := New(llmC, "m").Review(context.Background(), page(), doc)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	v := rep.Verdicts[0]
	if v.Pass || len(v.UngroundedClaims) != 1 || v.UngroundedClaims[0] != "Vault 1.15.2" {
		t.Errorf("ungrounded claim not surfaced (despite prose wrapper): %+v", v)
	}
	if rep.AllPass() {
		t.Error("AllPass must be false when a section fails")
	}
}

func TestReview_EmptySectionFailsWithoutLLMCall(t *testing.T) {
	llmC := &seqLLM{}
	doc := proseDoc(map[string]string{}) // Overview absent
	rep, err := New(llmC, "m").Review(context.Background(), page(), doc)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(llmC.seen) != 0 {
		t.Error("a missing section must not cost an LLM call")
	}
	if len(rep.Verdicts) != 1 || rep.Verdicts[0].Pass {
		t.Errorf("missing section must fail: %+v", rep.Verdicts)
	}
}

func TestReview_UnparseableResponseIsInconclusiveNotPass(t *testing.T) {
	llmC := &seqLLM{resp: []string{"the section looks fine to me"}}
	doc := proseDoc(map[string]string{"Overview": "Vault prose."})
	rep, _ := New(llmC, "m").Review(context.Background(), page(), doc)

	v := rep.Verdicts[0]
	if v.Pass || !v.Inconclusive {
		t.Errorf("garbage response must be Inconclusive, never a silent pass: %+v", v)
	}
	if rep.AllPass() {
		t.Error("an inconclusive verdict must not count as all-pass")
	}
}

func TestReview_LLMErrorIsReturned(t *testing.T) {
	llmC := &seqLLM{err: errors.New("boom")}
	doc := proseDoc(map[string]string{"Overview": "x"})
	if _, err := New(llmC, "m").Review(context.Background(), page(), doc); err == nil {
		t.Fatal("an LLM transport error must be returned (distinct from a fail verdict)")
	}
}

func TestExtractJSONObject_IgnoresBracesInStrings(t *testing.T) {
	in := `prefix {"reason": "uses { and } literally", "pass": true} suffix`
	got, ok := extractJSONObject(in)
	if !ok || got != `{"reason": "uses { and } literally", "pass": true}` {
		t.Errorf("balanced extraction wrong: %q ok=%v", got, ok)
	}
	if _, ok := extractJSONObject("no json here"); ok {
		t.Error("no object must report ok=false")
	}
}
