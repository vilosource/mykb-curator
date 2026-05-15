// Package reporter assembles structured run reports — the audit
// trail of what the curator did in a given run.
//
// A run report is load-bearing for the soft-read-only contract:
// every wiki overwrite must be surfaced so humans can review.
package reporter

import (
	"fmt"
	"time"
)

// Report is the structured record of one curator run.
type Report struct {
	RunID      string
	Wiki       string
	StartedAt  time.Time
	EndedAt    time.Time
	KBCommit   string
	Specs      []SpecResult
	Errors     []string
	Warnings   []string
	Metrics    Metrics
}

// SpecResult records what happened to one spec during the run.
type SpecResult struct {
	ID                string
	Status            SpecStatus
	Reason            string // populated for Skipped, Errored
	NewRevisionID     string
	BlocksRegenerated int
	HumanEdits        []HumanEditEvent
}

// SpecStatus is the disposition of one spec in one run.
type SpecStatus string

const (
	StatusRendered SpecStatus = "rendered"
	StatusSkipped  SpecStatus = "skipped" // cache hit, no source changes
	StatusFailed   SpecStatus = "failed"
)

// HumanEditEvent records a detected human modification.
type HumanEditEvent struct {
	BlockID     string
	Action      EditAction
	Diff        string
	Explanation string
	Suggestion  string
}

// EditAction is what the reconciler decided to do with a human edit.
type EditAction string

const (
	ActionOverwritten EditAction = "overwritten"
	ActionPreserved   EditAction = "preserved"
	ActionFlagged     EditAction = "flagged"
)

// Metrics captures per-run counters.
type Metrics struct {
	LLMCalls         int
	LLMTokensIn      int
	LLMTokensOut     int
	WikiPagesChanged int
	WikiPagesSkipped int
}

// Builder accumulates events during a run and produces a Report.
type Builder struct {
	r Report
}

// NewBuilder starts a fresh report for the given wiki + run id.
func NewBuilder(wiki, runID string) *Builder {
	return &Builder{r: Report{
		RunID:     runID,
		Wiki:      wiki,
		StartedAt: time.Now().UTC(),
	}}
}

// SetKBCommit records the kb commit the run is operating against.
func (b *Builder) SetKBCommit(commit string) { b.r.KBCommit = commit }

// AddSpecResult appends one spec disposition to the report.
func (b *Builder) AddSpecResult(s SpecResult) {
	b.r.Specs = append(b.r.Specs, s)
}

// AddError records a run-level error (an error that didn't tie
// cleanly to one spec — e.g. kb pull failed).
func (b *Builder) AddError(err error) {
	if err != nil {
		b.r.Errors = append(b.r.Errors, err.Error())
	}
}

// AddWarning records a non-fatal observation.
func (b *Builder) AddWarning(msg string) {
	b.r.Warnings = append(b.r.Warnings, msg)
}

// Build finalises and returns the report.
func (b *Builder) Build() Report {
	b.r.EndedAt = time.Now().UTC()
	return b.r
}

// Summary returns a one-line human-readable summary of the report.
// Used as the default CLI output; full report is the YAML file.
func (r Report) Summary() string {
	return fmt.Sprintf(
		"wiki=%s kb=%s specs=%d changed=%d skipped=%d errors=%d warnings=%d",
		r.Wiki, r.KBCommit, len(r.Specs),
		r.Metrics.WikiPagesChanged, r.Metrics.WikiPagesSkipped,
		len(r.Errors), len(r.Warnings),
	)
}
