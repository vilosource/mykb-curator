// Package nav models where a curated page sits in the site hub
// hierarchy. Placement is declared per-page (a `nav` block in the
// spec) with subpath-derived defaults, so navigation is auto-derived
// rather than hand-maintained (see docs/navigation-DESIGN.md).
//
// This package is pure: no spec parsing, no kb, no wiki. Parsers
// populate the declared Placement; the orchestrator builds a nav map
// from the resolved placements; the hub frontend renders it.
package nav

import "strings"

// Placement is a page's position in the hub hierarchy.
//
//   - Parent:  the hub page this page is listed under ("" = the root).
//   - Section: the group within that hub ("" = the hub's default group).
//   - Order:   sort weight within the section (lower first; ties break
//     by Label/title — applied at render time).
//   - Label:   display text in the hub list.
//   - Blurb:   one-line description (a kb area-summary fallback is
//     applied later, where the kb snapshot is available).
type Placement struct {
	Parent  string
	Section string
	Order   int
	Label   string
	Blurb   string

	// Area is an optional kb area id. When set and Blurb is empty, the
	// hub frontend fills the description from that area's summary — so
	// an auto-derived link stays fresh from the brain (same mechanism
	// as an explicit hub link's `area:`).
	Area string
}

// Resolve fills empty fields of a declared placement from the page
// title: Parent from the subpage path, Label from the last path
// segment (de-underscored). Declared fields always win. Section,
// Order and Blurb are carried through unchanged (their defaults are a
// render-time concern).
func Resolve(page string, declared Placement) Placement {
	out := declared
	if out.Parent == "" {
		out.Parent = subpathParent(page)
	}
	if out.Label == "" {
		out.Label = deUnderscore(lastSegment(page))
	}
	return out
}

// SubpathParent returns the parent path of a MediaWiki subpage title
// ("A/B/C" → "A/B"); a title with no "/" has no parent (""). Exposed
// for orphan-pruning, which redirects an orphan to its parent hub.
func SubpathParent(title string) string { return subpathParent(title) }

// subpathParent returns the parent path of a MediaWiki subpage title
// ("A/B/C" → "A/B"); a title with no "/" has no parent ("").
func subpathParent(title string) string {
	if i := strings.LastIndex(title, "/"); i >= 0 {
		return title[:i]
	}
	return ""
}

// lastSegment returns the final path segment ("A/B/C" → "C"); a title
// with no "/" is its own last segment.
func lastSegment(title string) string {
	if i := strings.LastIndex(title, "/"); i >= 0 {
		return title[i+1:]
	}
	return title
}

// deUnderscore turns a MediaWiki page segment into display text
// ("Azure_Tenant" → "Azure Tenant"). Underscores and spaces are
// equivalent in MediaWiki titles.
func deUnderscore(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}
