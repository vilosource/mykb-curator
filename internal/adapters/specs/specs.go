// Package specs defines the spec-store adapter interface and the
// in-memory Spec type. Implementations live in subpackages (git,
// s3, local).
package specs

import "context"

// Store fetches specs for a wiki tenant.
type Store interface {
	// Pull returns the current set of active specs from the store.
	Pull(ctx context.Context) ([]Spec, error)

	// Whoami reports the identity the adapter is operating as. Used
	// for run reports.
	Whoami() string
}

// Spec is the parsed in-memory representation of one page spec.
type Spec struct {
	// ID is the spec's stable identifier (file path inside the store,
	// or assigned slug).
	ID string

	// Wiki is the routing-guardrail field from frontmatter; the
	// orchestrator errors loudly if this mismatches the run's wiki.
	Wiki string

	// Page is the target page title in the wiki.
	Page string

	// Kind is the frontend selector: projection | editorial | hub | runbook.
	Kind string

	// Include declares the kb subset this spec is allowed to read.
	Include IncludeFilter

	// FactCheck is the spec's opt-in fact-checking declaration
	// (DESIGN §6.4: expensive checks are funded by specs that opt
	// in). Map of check-name -> cadence, e.g.
	// {"external_truth": "quarterly"}. Empty = no opt-in.
	FactCheck map[string]string

	// Body is the (markdown) intent description; passed to the
	// frontend as part of the prompt-or-template.
	Body string

	// Hash is the content hash of the spec; used in cache keys.
	Hash string
}

// IncludeFilter declares which subset of the kb a spec is allowed
// to read. Defense in depth against cross-tenant data leakage.
type IncludeFilter struct {
	Areas        []string
	Workspaces   []string // workspace ids, or the literal "linked-to-areas"
	ExcludeZones []string
}
