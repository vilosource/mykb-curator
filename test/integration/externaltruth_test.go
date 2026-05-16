//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance/checks/externaltruth"
)

type etWeb struct{ res []externaltruth.Result }

func (w etWeb) Search(context.Context, string) ([]externaltruth.Result, error) {
	return w.res, nil
}

type etLLM struct{ verdict string }

func (l etLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{Text: l.verdict}, nil
}

// TestExternalTruth_PipelineIntegration runs the external-truth check
// as part of a real maintenance.Pipeline (alongside the kind of
// no-op-on-this-snapshot neighbours it ships with) and asserts both
// the funding gate and that a CONTRADICTED opted-in fact aggregates
// into a Deprecate proposal out of the composed pipeline.
func TestExternalTruth_PipelineIntegration(t *testing.T) {
	web := etWeb{res: []externaltruth.Result{{URL: "https://release-notes"}}}
	model := etLLM{verdict: "CONTRADICTED — the deployed version differs"}

	// Only "vault" is funded; the snapshot also has "networking",
	// which must be ignored by the check entirely.
	check := externaltruth.New([]string{"vault"}, web, model, "test-model")
	pipe := maintenance.NewPipeline(check)

	snap := kb.Snapshot{Areas: []kb.Area{
		{ID: "vault", Entries: []kb.Entry{
			{ID: "f1", Type: "fact", Text: "Vault 1.17 deployed", Zone: "active"},
		}},
		{ID: "networking", Entries: []kb.Entry{
			{ID: "f2", Type: "fact", Text: "BGP enabled", Zone: "active"},
		}},
	}}

	props, err := pipe.Run(context.Background(), snap)
	if err != nil {
		t.Fatalf("pipeline Run: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("want exactly 1 proposal (funding gate excludes networking), got %d: %+v", len(props), props)
	}
	p := props[0]
	if p.Kind != maintenance.ProposalDeprecate || p.Area != "vault" || p.ID != "f1" {
		t.Errorf("unexpected proposal: %+v", p)
	}
	if p.Source != "external-truth" {
		t.Errorf("Source = %q, want external-truth", p.Source)
	}
}
