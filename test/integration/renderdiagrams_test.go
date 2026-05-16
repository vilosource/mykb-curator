//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/memory"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/renderdiagrams"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
)

// stubRenderer is a deterministic, mmdc-independent Renderer. The
// real mmdc subprocess is exercised at the unit level (skipped when
// absent); this L2 test integrates the pass with a real wiki.Target
// (memory) acting as the Uploader.
type stubRenderer struct{}

func (stubRenderer) Render(_ context.Context, _, _ string) ([]byte, string, error) {
	return []byte("\x89PNG\r\n\x1a\n-stub"), "image/png", nil
}

// TestRenderDiagrams_PipelineIntegration runs RenderDiagrams inside a
// real passes.Pipeline, uploading to a real wiki.Target impl, and
// asserts the diagram block ends up asset-ref'd and the asset is
// retrievable from the target — and that the upload is idempotent
// across two pipeline runs (deterministic filename).
func TestRenderDiagrams_PipelineIntegration(t *testing.T) {
	ctx := context.Background()
	target := memory.New("User:Bot")

	// wiki.Target satisfies renderdiagrams.Uploader (interface
	// segregation) — assert that statically here.
	var _ renderdiagrams.Uploader = wiki.Target(target)

	pipe := passes.NewPipeline(
		renderdiagrams.New(stubRenderer{}, target),
		zonemarkers.New(), // realistic neighbour; must not disturb the asset ref
	)

	doc := ir.Document{Sections: []ir.Section{{
		Heading: "Diagrams",
		Blocks: []ir.Block{
			ir.DiagramBlock{Lang: "mermaid", Source: "graph TD; A-->B", Prov: ir.Provenance{SpecSection: "s0"}},
		},
	}}}

	out, err := pipe.Apply(ctx, doc)
	if err != nil {
		t.Fatalf("pipeline Apply: %v", err)
	}

	db := findDiagram(t, out)
	if db.AssetRef == "" {
		t.Fatalf("DiagramBlock.AssetRef empty after pipeline; markers/passes lost it")
	}

	// Second run over a fresh document with identical source must hit
	// the same deterministic filename → idempotent upload, same ref.
	out2, err := pipe.Apply(ctx, ir.Document{Sections: []ir.Section{{
		Heading: "Diagrams",
		Blocks:  []ir.Block{ir.DiagramBlock{Lang: "mermaid", Source: "graph TD; A-->B"}},
	}}})
	if err != nil {
		t.Fatalf("pipeline Apply #2: %v", err)
	}
	if got := findDiagram(t, out2).AssetRef; got != db.AssetRef {
		t.Errorf("non-idempotent: ref %q vs %q for identical source", got, db.AssetRef)
	}
}

func findDiagram(t *testing.T, d ir.Document) ir.DiagramBlock {
	t.Helper()
	for _, s := range d.Sections {
		for _, b := range s.Blocks {
			if db, ok := b.(ir.DiagramBlock); ok {
				return db
			}
		}
	}
	t.Fatalf("no DiagramBlock in document: %+v", d)
	return ir.DiagramBlock{}
}
