//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/applystylerules"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
)

// TestApplyStyleRules_PipelineIntegration runs ApplyStyleRules inside
// a real passes.Pipeline next to ApplyZoneMarkers (its real
// downstream neighbour) and asserts terminology + heading-case land
// and survive marker wrapping.
func TestApplyStyleRules_PipelineIntegration(t *testing.T) {
	term := applystylerules.NewTerminologyRule(map[string]string{"k8s": "Kubernetes"})
	hcase, err := applystylerules.NewHeadingCaseRule("title")
	if err != nil {
		t.Fatalf("NewHeadingCaseRule: %v", err)
	}
	pipe := passes.NewPipeline(
		applystylerules.New(term, hcase),
		zonemarkers.New(),
	)

	in := ir.Document{Sections: []ir.Section{{
		Heading: "running k8s in prod",
		Blocks:  []ir.Block{ir.ProseBlock{Text: "We deploy k8s daily."}},
	}}}

	out, err := pipe.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("pipeline Apply: %v", err)
	}

	if got := out.Sections[0].Heading; got != "Running Kubernetes In Prod" {
		t.Errorf("heading = %q, want %q", got, "Running Kubernetes In Prod")
	}
	// Find the (now possibly marker-wrapped) prose block.
	var found bool
	for _, b := range out.Sections[0].Blocks {
		if pb, ok := b.(ir.ProseBlock); ok {
			found = true
			if pb.Text != "We deploy Kubernetes daily." {
				t.Errorf("prose = %q, want terminology applied", pb.Text)
			}
		}
	}
	if !found {
		t.Fatalf("prose block lost after pipeline: %+v", out.Sections[0].Blocks)
	}
}
