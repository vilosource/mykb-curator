package hub

import (
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/nav"
)

// otherSection is the trailing bucket for auto-hub members whose
// declared section isn't one of the hub's declared sections (or is
// empty). It guarantees completeness — no member is silently dropped.
const otherSection = "Other"

// ExpandAuto fills a `members: auto` hub's section Links from the nav
// map before the deterministic hub frontend renders it. The hub spec
// declares section ORDER + intro; the membership is auto-derived from
// the pages that named this hub as their nav.parent.
//
// Non-auto hubs (and non-hub specs) are returned unchanged, so this is
// a safe no-op pass over every spec. Pure: same (spec, map) → same
// output. Section order and intros are preserved; members matching a
// declared section title fill that section, in nav-map order; members
// with an undeclared or empty section land in a trailing "Other"
// section.
func ExpandAuto(spec specs.Spec, m nav.Map) specs.Spec {
	if spec.Hub == nil || !spec.Hub.Auto {
		return spec
	}

	members := m[spec.Page]

	// Index members by section for declared-section lookup; track
	// which ones land in a declared section so the rest go to "Other".
	placed := make([]bool, len(members))

	out := make([]specs.HubSection, 0, len(spec.Hub.Sections)+1)
	for _, sec := range spec.Hub.Sections {
		links := make([]specs.HubLink, 0)
		for i, mem := range members {
			if mem.Section == sec.Title {
				links = append(links, memberLink(mem))
				placed[i] = true
			}
		}
		out = append(out, specs.HubSection{Title: sec.Title, Desc: sec.Desc, Links: links})
	}

	var other []specs.HubLink
	for i, mem := range members {
		if !placed[i] {
			other = append(other, memberLink(mem))
		}
	}
	if len(other) > 0 {
		out = append(out, specs.HubSection{Title: otherSection, Links: other})
	}

	expanded := spec
	hub := *spec.Hub
	hub.Sections = out
	expanded.Hub = &hub
	return expanded
}

func memberLink(m nav.Member) specs.HubLink {
	// Area is carried so the hub frontend's existing area→summary
	// fallback fills Desc when Blurb is empty (fresh-from-kb blurbs).
	return specs.HubLink{Page: m.Page, Label: m.Label, Desc: m.Blurb, Area: m.Area}
}
