// Package sources defines the contract for resolving a doc-spec
// Source whose scheme is not kb (git/cmd/ssh/file — the
// "reality-probe" family).
//
// kb is resolved inside the architecture frontend (it is the
// always-available, in-process knowledge source). Everything else is
// a pluggable Resolver so the dangerous, capability-bearing schemes
// stay opt-in and individually policy-gated. Only the read-only
// git: resolver ships today; cmd/ssh/az are deferred behind an
// execution-policy model (adoption issue #12 / slice 4b).
package sources

import (
	"context"

	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Resolved is what a Resolver returns for one Source.
type Resolved struct {
	// Digest is prose-grounding text injected into the LLM prompt,
	// shaped like the kb digest (headings + bullet facts).
	Digest string

	// Rows are render:table rows, aligned to the architecture
	// frontend's ["Type","Ref","Summary"] columns.
	Rows [][]string

	// Refs are provenance identifiers (e.g. "git:repo@<commit>:path")
	// recorded on the produced block so drift is detectable.
	Refs []string
}

// Resolver turns a single declared Source into grounded content.
// Implementations MUST be read-only with respect to the resolved
// system — a resolver never mutates infrastructure or repos.
type Resolver interface {
	// Scheme is the docspec.Source.Scheme this resolver handles
	// ("git", "cmd", "ssh", "file").
	Scheme() string

	// Resolve returns the grounded content. ok=false (with nil error)
	// means "declared but not resolvable here" — the caller keeps the
	// honest pending placeholder rather than fabricating. A non-nil
	// error is a hard failure (misconfig, command failure).
	Resolve(ctx context.Context, s docspec.Source) (res Resolved, ok bool, err error)
}
