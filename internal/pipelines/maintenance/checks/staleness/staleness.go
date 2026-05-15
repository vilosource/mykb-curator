// Package staleness implements the StalenessCheck: flags entries
// whose Updated timestamp is older than a configured threshold so
// the operator can decide to re-verify, refresh, or archive them.
//
// Skips:
//   - entries already verified (different concern: re-verification)
//   - entries in zone=archived (already out of circulation)
//   - entries with no Updated timestamp (no decision possible)
//
// Threshold is per-check; per-area thresholds are a future extension
// (spec.fact_check.staleness: 30d).
package staleness

import (
	"context"
	"fmt"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

// Check is the staleness maintenance.Check impl.
type Check struct {
	threshold time.Duration
	now       func() time.Time // injected for tests
}

// New constructs a Check with the given freshness threshold.
func New(threshold time.Duration) *Check {
	return &Check{threshold: threshold, now: time.Now}
}

// Name returns "staleness".
func (*Check) Name() string { return "staleness" }

// Run scans the snapshot and proposes deprecation for each stale
// non-verified non-archived entry.
func (c *Check) Run(_ context.Context, snap kb.Snapshot) ([]maintenance.MutationProposal, error) {
	cutoff := c.now().Add(-c.threshold)
	var out []maintenance.MutationProposal
	for _, area := range snap.Areas {
		for _, e := range area.Entries {
			if !c.shouldFlag(e) {
				continue
			}
			updated, err := time.Parse(time.RFC3339, e.Updated)
			if err != nil {
				continue // unparseable → skip silently (logging is out of scope here)
			}
			if updated.After(cutoff) {
				continue
			}
			ageDays := int(c.now().Sub(updated).Hours() / 24)
			out = append(out, maintenance.MutationProposal{
				Kind:   maintenance.ProposalDeprecate,
				Area:   area.ID,
				ID:     e.ID,
				Source: c.Name(),
				Reason: fmt.Sprintf("entry not updated in %d days (threshold %s)", ageDays, c.threshold),
				Evidence: map[string]string{
					"updated_at": e.Updated,
					"threshold":  c.threshold.String(),
				},
			})
		}
	}
	return out, nil
}

func (c *Check) shouldFlag(e kb.Entry) bool {
	if e.Updated == "" {
		return false
	}
	if e.Zone == "archived" {
		return false
	}
	if e.Provenance.Status == "verified" {
		return false
	}
	return true
}
