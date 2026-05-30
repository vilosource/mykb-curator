package nav

import "testing"

func TestBuild_GroupsByResolvedParent(t *testing.T) {
	m := Build([]PageNav{
		// declared parent (a flat-titled cluster parent)
		{Page: "Vault Architecture", Nav: Placement{Parent: "OptiscanGroup/Azure_Infrastructure", Order: 2, Label: "Vault (deep dive)"}},
		// subpath-derived parent
		{Page: "OptiscanGroup/Azure_Infrastructure/Networking", Nav: Placement{Order: 1}},
		// the top hub self-registers into the root ("")
		{Page: "OptiscanGroup/Azure_Infrastructure", Nav: Placement{Parent: "OptiscanGroup"}},
	})

	azi := m["OptiscanGroup/Azure_Infrastructure"]
	if len(azi) != 2 {
		t.Fatalf("Azure_Infrastructure members = %d, want 2; got %+v", len(azi), azi)
	}
	if got := m["OptiscanGroup"]; len(got) != 1 || got[0].Page != "OptiscanGroup/Azure_Infrastructure" {
		t.Errorf("root-hub self-registration missing: %+v", got)
	}
}

// Within a parent, members are deterministically ordered by
// (section, order, label) — independent of input order.
func TestBuild_DeterministicOrder(t *testing.T) {
	m := Build([]PageNav{
		{Page: "P/c", Nav: Placement{Parent: "P", Section: "B", Order: 1, Label: "c"}},
		{Page: "P/a", Nav: Placement{Parent: "P", Section: "A", Order: 9, Label: "a"}},
		{Page: "P/b", Nav: Placement{Parent: "P", Section: "A", Order: 1, Label: "b"}},
	})
	got := []string{m["P"][0].Label, m["P"][1].Label, m["P"][2].Label}
	want := []string{"b", "a", "c"} // section A (order1,order9) then section B
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A member's resolved Label/Blurb come through (subpath label when not
// declared).
func TestBuild_ResolvesMemberFields(t *testing.T) {
	m := Build([]PageNav{
		{Page: "OptiscanGroup/Azure_Infrastructure/Docker_Swarm_Platform", Nav: Placement{Parent: "OptiscanGroup/Azure_Infrastructure"}},
	})
	mem := m["OptiscanGroup/Azure_Infrastructure"][0]
	if mem.Page != "OptiscanGroup/Azure_Infrastructure/Docker_Swarm_Platform" {
		t.Errorf("Page = %q", mem.Page)
	}
	if mem.Label != "Docker Swarm Platform" {
		t.Errorf("Label = %q, want subpath-derived de-underscored", mem.Label)
	}
}
