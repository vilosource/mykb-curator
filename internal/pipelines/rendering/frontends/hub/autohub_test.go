package hub_test

import (
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/nav"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/hub"
)

func autoSpec(page string, sections ...specs.HubSection) specs.Spec {
	return specs.Spec{Page: page, Kind: "hub", Hub: &specs.HubSpec{Auto: true, Sections: sections}}
}

// A non-auto hub (explicit links) is returned unchanged.
func TestExpandAuto_PassthroughWhenNotAuto(t *testing.T) {
	s := specs.Spec{Page: "H", Kind: "hub", Hub: &specs.HubSpec{
		Sections: []specs.HubSection{{Title: "S", Links: []specs.HubLink{{Page: "X"}}}},
	}}
	got := hub.ExpandAuto(s, nav.Map{"H": {{Page: "Y", Label: "Y"}}})
	if len(got.Hub.Sections) != 1 || len(got.Hub.Sections[0].Links) != 1 || got.Hub.Sections[0].Links[0].Page != "X" {
		t.Fatalf("non-auto hub must be untouched, got %+v", got.Hub.Sections)
	}
}

// An auto hub fills its declared sections' Links from the nav map,
// matching member.Section to the section title, preserving section
// order + intro, and using member Label/Blurb.
func TestExpandAuto_FillsDeclaredSections(t *testing.T) {
	s := autoSpec("OptiscanGroup/Azure_Infrastructure",
		specs.HubSection{Title: "Core Infrastructure", Desc: "Focus: the base layer."},
		specs.HubSection{Title: "Platform Service Automation"},
	)
	m := nav.Map{"OptiscanGroup/Azure_Infrastructure": {
		{Page: "OptiscanGroup/Azure_Infrastructure/Networking", Section: "Core Infrastructure", Label: "Networking", Blurb: "the wires"},
		{Page: "OptiscanGroup/Azure_Infrastructure/SSL", Section: "Platform Service Automation", Label: "Wildcard SSL"},
	}}
	got := hub.ExpandAuto(s, m)

	if len(got.Hub.Sections) != 2 {
		t.Fatalf("want 2 sections, got %d", len(got.Hub.Sections))
	}
	core := got.Hub.Sections[0]
	if core.Title != "Core Infrastructure" || core.Desc != "Focus: the base layer." {
		t.Errorf("section order/intro not preserved: %+v", core)
	}
	if len(core.Links) != 1 || core.Links[0].Page != "OptiscanGroup/Azure_Infrastructure/Networking" ||
		core.Links[0].Label != "Networking" || core.Links[0].Desc != "the wires" {
		t.Errorf("Core links wrong: %+v", core.Links)
	}
	if got.Hub.Sections[1].Links[0].Label != "Wildcard SSL" {
		t.Errorf("Platform section link wrong: %+v", got.Hub.Sections[1].Links)
	}
}

// Members whose section is undeclared (or empty) are not dropped — they
// land in a trailing "Other" section (completeness).
func TestExpandAuto_UndeclaredSectionGoesToOther(t *testing.T) {
	s := autoSpec("H", specs.HubSection{Title: "Known"})
	m := nav.Map{"H": {
		{Page: "H/a", Section: "Known", Label: "a"},
		{Page: "H/b", Section: "Mystery", Label: "b"},
		{Page: "H/c", Label: "c"}, // empty section
	}}
	got := hub.ExpandAuto(s, m)
	// Known section + a trailing Other section holding b and c.
	last := got.Hub.Sections[len(got.Hub.Sections)-1]
	if len(got.Hub.Sections) != 2 || len(last.Links) != 2 {
		t.Fatalf("undeclared/empty-section members must fall to a trailing bucket; got %+v", got.Hub.Sections)
	}
}

// A member's nav.area is carried onto the HubLink so the hub
// frontend's area→summary fallback fills the blurb (fresh from kb).
func TestExpandAuto_CarriesAreaForSummaryFallback(t *testing.T) {
	s := autoSpec("H", specs.HubSection{Title: "Topics"})
	m := nav.Map{"H": {{Page: "H/Vault", Section: "Topics", Label: "Vault", Area: "vault"}}}
	got := hub.ExpandAuto(s, m)
	link := got.Hub.Sections[0].Links[0]
	if link.Area != "vault" || link.Desc != "" {
		t.Errorf("want Area=vault + empty Desc (so frontend fills from summary), got %+v", link)
	}
}
