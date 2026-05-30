package nav

import "sort"

// PageNav pairs a page title with its declared placement — the input
// to Build, collected from every spec (both spec systems).
type PageNav struct {
	Page string
	Nav  Placement
}

// Member is a resolved entry in a hub's listing.
type Member struct {
	Page    string
	Section string
	Order   int
	Label   string
	Blurb   string
	Area    string
}

// Map maps a hub page title to its members, deterministically ordered.
// The empty key ("") holds the root's members (pages with no parent).
type Map map[string][]Member

// Build resolves every page's placement (subpath defaults via Resolve)
// and groups the pages by their resolved parent hub. Members within a
// parent are sorted by (section, order, label) for deterministic,
// idempotent output. Hubs self-register into their own parent, so the
// hierarchy emerges from the per-page declarations alone.
//
// Section ORDER across groups is the hub spec's concern (applied at
// render); Build only guarantees a stable within-parent ordering.
func Build(pages []PageNav) Map {
	m := Map{}
	for _, p := range pages {
		r := Resolve(p.Page, p.Nav)
		m[r.Parent] = append(m[r.Parent], Member{
			Page:    p.Page,
			Section: r.Section,
			Order:   r.Order,
			Label:   r.Label,
			Blurb:   r.Blurb,
			Area:    r.Area,
		})
	}
	for parent := range m {
		members := m[parent]
		sort.SliceStable(members, func(i, j int) bool {
			a, b := members[i], members[j]
			if a.Section != b.Section {
				return a.Section < b.Section
			}
			if a.Order != b.Order {
				return a.Order < b.Order
			}
			return a.Label < b.Label
		})
	}
	return m
}
