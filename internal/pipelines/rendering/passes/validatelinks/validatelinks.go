// Package validatelinks implements the ValidateLinks pass.
//
// Scans every block for wiki-internal links of the form [[Page]] or
// [[Target|alias]]. Each link's target is checked against a
// known-pages map. Unknown targets fail the pass loudly — pipeline
// errors are wrapped with the failing pass name in run reports, so
// broken links surface as actionable spec/kb gaps.
//
// External URLs (http://, https://) are NOT this pass's concern;
// they're handled by the maintenance pipeline's link-rot check.
//
// Known-pages map is built by the composition root from the
// configured spec store (every spec.Page → true). Nil map is
// conservative — every link is treated as broken so the operator
// must explicitly declare what's reachable.
package validatelinks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

// ErrBrokenLinks is the sentinel returned (wrapped) when one or more
// wiki-internal links resolve to pages not in the known set.
var ErrBrokenLinks = errors.New("validate-links: broken internal links")

// linkRE matches [[Target]] or [[Target|display]] but is intentionally
// greedy-by-default; the captured group is the target before any pipe.
var linkRE = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// ValidateLinks is the Pass impl.
type ValidateLinks struct {
	known map[string]bool
}

// New constructs a ValidateLinks pass with the given known-pages map.
// Pass nil to make every link a failure (defense-in-depth).
func New(known map[string]bool) *ValidateLinks {
	return &ValidateLinks{known: known}
}

// Name returns "validate-links".
func (*ValidateLinks) Name() string { return "validate-links" }

// Apply walks blocks, extracts wiki links, validates each. The
// document is returned unchanged on success; on failure, it's
// returned alongside an error listing every broken link.
func (v *ValidateLinks) Apply(_ context.Context, doc ir.Document) (ir.Document, error) {
	broken := map[string]bool{}
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			// IndexBlock carries structured internal links (hub /
			// index pages). Navigation correctness is the whole
			// point of a hub, so every entry's target must resolve.
			if ib, ok := b.(ir.IndexBlock); ok {
				for _, e := range ib.Entries {
					if t := strings.TrimSpace(e.Page); t != "" && !v.known[t] {
						broken[t] = true
					}
				}
				continue
			}
			text := textOf(b)
			if text == "" {
				continue
			}
			for _, m := range linkRE.FindAllStringSubmatch(text, -1) {
				target := strings.TrimSpace(m[1])
				if target == "" {
					continue
				}
				if v.known[target] {
					continue
				}
				broken[target] = true
			}
		}
	}
	if len(broken) == 0 {
		return doc, nil
	}
	names := make([]string, 0, len(broken))
	for n := range broken {
		names = append(names, n)
	}
	sort.Strings(names)
	return doc, fmt.Errorf("%w: %s", ErrBrokenLinks, strings.Join(names, ", "))
}

// textOf extracts the searchable text from a block. Each block kind
// carries different fields; this helper centralises which fields
// can hold links.
func textOf(b ir.Block) string {
	switch x := b.(type) {
	case ir.ProseBlock:
		return x.Text
	case ir.Callout:
		return x.Body
	case ir.MachineBlock:
		return x.Body
	case ir.EscapeHatch:
		return x.Raw
	default:
		return ""
	}
}
