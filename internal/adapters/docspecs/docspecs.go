// Package docspecs is the store contract for doc-spec cluster files
// (the SDD-for-docs source language: one topic = parent + children).
//
// It is deliberately separate from the legacy specs.Store: a
// docspec produces a whole cross-linked cluster via the cluster
// orchestrator, not a single page via a Frontend.Build. Same
// fail-whole-pull discipline as specs: a half-loaded spec set
// produces a silently-broken wiki.
package docspecs

import (
	"context"

	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// File is one parsed *.doc.yaml: a stable ID (relative path) plus
// the cluster spec it contains.
type File struct {
	ID   string
	Spec docspec.DocSpec
}

// Store discovers and parses doc-spec cluster files.
type Store interface {
	// Pull returns every cluster file, sorted by ID. Any parse error
	// aborts the whole pull.
	Pull(ctx context.Context) ([]File, error)
	// Whoami is a human-readable identity for run reports.
	Whoami() string
}
