// Package frontends defines the Frontend interface and the Registry
// that dispatches a spec to its appropriate frontend.
//
// Per the architecture (DESIGN.md §5.2), frontends are the
// intelligence locus of the rendering pipeline:
//   - ProjectionFrontend: deterministic; reads area + entries → IR.
//   - EditorialFrontend (v0.5): LLM-driven; reads sources, exercises
//     editorial judgement.
//   - HubFrontend / RunbookFrontend (later): specialised shapes.
//
// Every frontend ends with an ir.Document — the universal hand-off
// to the pass pipeline.
package frontends

import (
	"context"
	"fmt"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// Frontend turns a (spec, kb-snapshot) pair into an IR Document.
type Frontend interface {
	// Name returns a stable identifier for run reports.
	Name() string

	// Kind returns the spec.Kind value this frontend handles
	// ("projection" | "editorial" | "hub" | "runbook" | ...).
	Kind() string

	// Build produces the IR Document. May call an injected LLM
	// client for LLM-backed frontends; deterministic frontends use
	// no I/O.
	Build(ctx context.Context, spec specs.Spec, snap kb.Snapshot) (ir.Document, error)
}

// Registry maps spec kinds to frontends.
type Registry struct {
	byKind map[string]Frontend
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byKind: make(map[string]Frontend)}
}

// Register adds a frontend keyed by its declared Kind. Panics on a
// duplicate kind — duplicate registration is a programming error,
// not a runtime condition.
func (r *Registry) Register(f Frontend) {
	if _, dup := r.byKind[f.Kind()]; dup {
		panic(fmt.Sprintf("frontends: duplicate registration for kind %q", f.Kind()))
	}
	r.byKind[f.Kind()] = f
}

// For returns the frontend for the given spec kind, or an error if
// no frontend is registered.
func (r *Registry) For(kind string) (Frontend, error) {
	f, ok := r.byKind[kind]
	if !ok {
		return nil, fmt.Errorf("frontends: no frontend registered for kind %q", kind)
	}
	return f, nil
}

// Kinds returns the registered kinds in unspecified order. For
// diagnostics / run reports.
func (r *Registry) Kinds() []string {
	out := make([]string, 0, len(r.byKind))
	for k := range r.byKind {
		out = append(out, k)
	}
	return out
}
