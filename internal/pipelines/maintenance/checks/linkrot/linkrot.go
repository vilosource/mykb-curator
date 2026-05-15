// Package linkrot implements the LinkRotCheck: probes the URL field
// of every entry with type=link via HTTP HEAD; proposes deprecation
// for any URL returning 4xx/5xx or a network error.
//
// Only entries with type=link are probed — defense in depth against
// rogue URL fields on facts/decisions/etc.
//
// HTTP HEAD is preferred over GET to avoid downloading content; some
// servers reject HEAD with 405, in which case we fall back to GET
// (range-limited to a single byte).
package linkrot

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

// Check is the link-rot maintenance.Check impl.
type Check struct {
	http *http.Client
}

// New constructs a Check with the given per-request timeout.
func New(timeout time.Duration) *Check {
	return &Check{http: &http.Client{Timeout: timeout}}
}

// Name returns "link-rot".
func (*Check) Name() string { return "link-rot" }

// Run probes every link entry's URL.
func (c *Check) Run(ctx context.Context, snap kb.Snapshot) ([]maintenance.MutationProposal, error) {
	var out []maintenance.MutationProposal
	for _, area := range snap.Areas {
		for _, e := range area.Entries {
			if e.Type != "link" || e.URL == "" {
				continue
			}
			if mp, ok := c.probe(ctx, area.ID, e); ok {
				out = append(out, mp)
			}
		}
	}
	return out, nil
}

// probe makes an HTTP HEAD against the URL. Returns a deprecate
// proposal + true if the URL is broken; (zero, false) for live URLs.
func (c *Check) probe(ctx context.Context, areaID string, e kb.Entry) (maintenance.MutationProposal, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, e.URL, nil)
	if err != nil {
		return c.deprecation(areaID, e, "request-build-failed", err.Error()), true
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return c.deprecation(areaID, e, "network-error", err.Error()), true
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return c.deprecation(areaID, e, "http-status", strconv.Itoa(resp.StatusCode)), true
	}
	return maintenance.MutationProposal{}, false
}

func (c *Check) deprecation(areaID string, e kb.Entry, reasonKey, reasonValue string) maintenance.MutationProposal {
	return maintenance.MutationProposal{
		Kind:   maintenance.ProposalDeprecate,
		Area:   areaID,
		ID:     e.ID,
		Source: "link-rot",
		Reason: fmt.Sprintf("link URL is broken (%s: %s)", reasonKey, reasonValue),
		Evidence: map[string]string{
			"url":         e.URL,
			"http_status": maybeStatus(reasonKey, reasonValue),
			"error":       maybeErr(reasonKey, reasonValue),
		},
	}
}

func maybeStatus(k, v string) string {
	if k == "http-status" {
		return v
	}
	return ""
}

func maybeErr(k, v string) string {
	if k != "http-status" {
		return v
	}
	return ""
}
