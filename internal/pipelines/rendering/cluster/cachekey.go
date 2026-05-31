package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/cache/ircache"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// PageKey is the deterministic cache key for one docspec page: its
// content hash + the hash of the kb subset it reads + the pipeline
// version. Two runs with an unchanged page spec and unchanged kb
// produce the same key, so the page's rendered IR + Judge verdict can
// be reused instead of re-synthesised (the source of run-to-run
// nondeterminism — docs/navigation-DESIGN.md §11 / task #3).
func PageKey(page docspec.DocPage, snap kb.Snapshot, pipelineVersion string) string {
	kbHash := ircache.HashKBSubset(snap, specs.IncludeFilter{Areas: pageAreas(page)})
	return ircache.Key(hashDocPage(page), kbHash, pipelineVersion)
}

// pageAreas returns the sorted, de-duplicated set of kb area ids the
// page reads (from page-level and section-level kb: sources). Drives
// the kb-subset hash so the key invalidates only when relevant kb
// content moves.
func pageAreas(p docspec.DocPage) []string {
	seen := map[string]bool{}
	collect := func(srcs []docspec.Source) {
		for _, s := range srcs {
			if s.Scheme != "kb" {
				continue
			}
			for _, tok := range strings.Fields(s.Spec) {
				if strings.HasPrefix(tok, "area=") {
					if a := strings.TrimPrefix(tok, "area="); a != "" {
						seen[a] = true
					}
				}
			}
		}
	}
	collect(p.Sources)
	for _, sec := range p.Sections {
		collect(sec.Sources)
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// hashDocPage hashes every content-determining field of a page, so the
// key changes iff the synthesis inputs change.
func hashDocPage(p docspec.DocPage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "page=%s\x00kind=%s\x00audience=%s\x00intent=%s\x00", p.Page, p.Kind, p.Audience, p.Intent)
	for _, s := range p.Sections {
		fmt.Fprintf(&b, "sec=%s\x00int=%s\x00render=%s\x00src=%s\x00", s.Title, s.Intent, s.Render, rawSources(s.Sources))
	}
	fmt.Fprintf(&b, "psrc=%s\x00related=%s\x00cats=%s\x00", rawSources(p.Sources), strings.Join(p.Related, ","), strings.Join(p.Categories, ","))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func rawSources(srcs []docspec.Source) string {
	parts := make([]string, 0, len(srcs))
	for _, s := range srcs {
		parts = append(parts, s.Scheme+":"+s.Spec)
	}
	return strings.Join(parts, "|")
}
