// Package kb defines the KB source adapter interface — the read-side
// boundary between mykb-curator and a mykb brain.
//
// Implementations live in subpackages (git, local, daemon). The
// orchestrator depends on Source, not on any concrete implementation.
package kb

import "context"

// Source fetches kb snapshots. Implementations are expected to be
// read-only against the canonical brain; writes go through a separate
// channel (the maintenance pipeline's PR backend).
type Source interface {
	// Pull fetches the current state of the kb. Returns a snapshot and
	// the commit identifier the snapshot was taken from.
	Pull(ctx context.Context) (Snapshot, error)

	// Whoami reports the identity the adapter is operating as (e.g. a
	// git remote URL, a local path). Used for run reports.
	Whoami() string
}

// Snapshot is a point-in-time view of a kb. v1 carries minimal
// information; future versions add the area/entry tree.
type Snapshot struct {
	// Commit identifies the snapshot (git commit hash for git sources,
	// generation counter for daemon sources).
	Commit string

	// ChangedAreas lists areas that have changed since the previous
	// snapshot. Empty for first-run / cold-start.
	ChangedAreas []string
}
