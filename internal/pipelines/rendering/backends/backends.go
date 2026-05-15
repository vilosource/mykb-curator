// Package backends defines the Backend interface — the compiler's
// codegen stage. A Backend renders an ir.Document into a
// target-format byte sequence.
//
// Per the architecture (DESIGN.md §5), backends are:
//   - Pure functions: no I/O, no side effects.
//   - Deterministic: same IR → same bytes, byte-for-byte.
//   - Mechanical: every backend implements the full IR block taxonomy.
//
// Concrete backends live in subpackages (markdown, mediawiki, ...).
// The Backend Registry (TBD) selects by name from config.
package backends

import "github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"

// Backend is the codegen stage. Render is a pure function.
type Backend interface {
	// Name returns a stable identifier used by config + run reports
	// to select this backend. Must be unique across impls.
	Name() string

	// Render produces the target-format bytes for the given document.
	// Pure: no I/O, deterministic, idempotent.
	Render(doc ir.Document) ([]byte, error)
}
