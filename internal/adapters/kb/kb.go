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

// Snapshot is a point-in-time view of a kb.
type Snapshot struct {
	// Commit identifies the snapshot (git commit hash for git sources,
	// generation counter for daemon sources).
	Commit string

	// ChangedAreas lists areas that have changed since the previous
	// snapshot. Empty for first-run / cold-start.
	ChangedAreas []string

	// Areas is the loaded area tree, including entries. May be nil
	// if a Source intentionally returns a metadata-only snapshot
	// (e.g. for cheap diff-only operations).
	Areas []Area
}

// Area returns the area with the given ID, or nil if not present.
// Linear scan — fine for the typical 10–50 areas; if scale demands
// O(1), the Source impl can intern a map.
func (s *Snapshot) Area(id string) *Area {
	for i := range s.Areas {
		if s.Areas[i].ID == id {
			return &s.Areas[i]
		}
	}
	return nil
}

// Area is one knowledge area: identity + summary + entries.
//
// Mirrors the mykb on-disk schema: ID + name + summary from area.json,
// entries (facts, decisions, gotchas, patterns, links) flattened into
// one slice tagged by Type.
type Area struct {
	ID      string
	Name    string
	Summary string
	Tags    []string
	Entries []Entry
}

// EntriesByType returns the subset of entries with the given type tag.
// Common types: "fact", "decision", "gotcha", "pattern", "link".
func (a *Area) EntriesByType(t string) []Entry {
	var out []Entry
	for _, e := range a.Entries {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// Entry is one record in an area's JSONL files. The fields below cover
// the common schema plus the decision-specific fields. New entry
// types extend the type tag and add fields here as needed.
type Entry struct {
	ID         string
	Area       string
	Type       string // "fact" | "decision" | "gotcha" | "pattern" | "link"
	Text       string
	Tags       []string
	Zone       string // "incoming" | "active" | "established" | "archived"
	Created    string // RFC3339; kept as string to avoid timezone surprises
	Updated    string
	Provenance EntryProvenance

	// Decision-only fields. Empty for other entry types.
	Why      string
	Rejected string
	Context  string

	// Link-only fields. Empty for other entry types.
	URL string
}

// EntryProvenance carries the verification status + source for an
// entry. Used by the StampVerification pass to annotate output with
// verified vs unverified content.
type EntryProvenance struct {
	Status string // "verified" | "unverified"
	Source string
}
