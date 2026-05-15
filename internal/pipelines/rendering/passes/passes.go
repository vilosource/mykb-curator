// Package passes defines the Pass interface — the compiler's
// IR→IR transformation stage — and the Pipeline composer that runs
// passes in order.
//
// Per the architecture (DESIGN.md §5.4), passes:
//   - Are deterministic by default (LLM-backed passes opt in
//     explicitly per spec).
//   - Operate on IR (no I/O, no external state at this layer; an LLM
//     pass would access an injected LLM client, not the network
//     directly).
//   - Run in declared order. Each pass takes the output of the last.
//
// Concrete passes live in subpackages (zonemarkers, validatelinks,
// dedupefacts, renderdiagrams, ...).
package passes

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Pass is one IR→IR transformation.
type Pass interface {
	// Name returns a stable identifier used by config + run reports.
	Name() string

	// Apply transforms doc. Pure for deterministic passes; may call
	// an injected LLM client (passed via constructor) for LLM passes.
	Apply(ctx context.Context, doc ir.Document) (ir.Document, error)
}

// Pipeline runs a sequence of Passes. Construct once, Apply many
// times; Pipelines are safe for concurrent Apply calls as long as
// the underlying Pass impls are.
type Pipeline struct {
	passes []Pass
}

// NewPipeline returns a Pipeline that applies the given passes in
// order. Variadic for ergonomic composition at the call site.
func NewPipeline(passes ...Pass) *Pipeline {
	return &Pipeline{passes: passes}
}

// Apply runs each pass in sequence, threading the IR through. Stops
// at the first error and wraps it with the failing pass's name so
// run reports show which pass broke.
func (p *Pipeline) Apply(ctx context.Context, doc ir.Document) (ir.Document, error) {
	for _, pass := range p.passes {
		out, err := pass.Apply(ctx, doc)
		if err != nil {
			return doc, fmt.Errorf("pass %q: %w", pass.Name(), err)
		}
		doc = out
	}
	return doc, nil
}

// Names returns the pass names in execution order. Used by run
// reports to show which passes ran for each spec.
func (p *Pipeline) Names() []string {
	out := make([]string, len(p.passes))
	for i, pass := range p.passes {
		out[i] = pass.Name()
	}
	return out
}
