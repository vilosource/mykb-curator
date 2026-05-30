package nav

import "testing"

// A subpaged page with no declared placement derives its parent from
// the title path and its label from the last segment (de-underscored).
func TestResolve_SubpathParentAndLabel(t *testing.T) {
	got := Resolve("OptiscanGroup/Azure_Infrastructure/Vault", Placement{})
	if got.Parent != "OptiscanGroup/Azure_Infrastructure" {
		t.Errorf("Parent = %q, want %q", got.Parent, "OptiscanGroup/Azure_Infrastructure")
	}
	if got.Label != "Vault" {
		t.Errorf("Label = %q, want %q", got.Label, "Vault")
	}
}

func TestResolve_DeUnderscoreLabel(t *testing.T) {
	got := Resolve("OptiscanGroup/Azure_Infrastructure/Azure_Tenant_and_Subscriptions", Placement{})
	if got.Label != "Azure Tenant and Subscriptions" {
		t.Errorf("Label = %q, want de-underscored last segment", got.Label)
	}
}

// Declared fields always win over subpath derivation.
func TestResolve_DeclaredOverridesSubpath(t *testing.T) {
	got := Resolve("OptiscanGroup/Azure_Infrastructure/Vault", Placement{
		Parent: "OptiscanGroup/Custom", Section: "Core", Order: 5,
		Label: "Vault (custom)", Blurb: "the secrets backend",
	})
	if got.Parent != "OptiscanGroup/Custom" || got.Label != "Vault (custom)" {
		t.Errorf("declared parent/label not preserved: %+v", got)
	}
	if got.Section != "Core" || got.Order != 5 || got.Blurb != "the secrets backend" {
		t.Errorf("declared section/order/blurb not carried through: %+v", got)
	}
}

// A flat-titled page (e.g. a docspec cluster parent) has no subpath, so
// no derived parent (→ root) and the whole title is the label.
func TestResolve_FlatTitle(t *testing.T) {
	got := Resolve("Vault Architecture", Placement{})
	if got.Parent != "" {
		t.Errorf("Parent = %q, want \"\" (root) for a flat title", got.Parent)
	}
	if got.Label != "Vault Architecture" {
		t.Errorf("Label = %q, want the whole title", got.Label)
	}
}

// The site root itself (no slash) resolves to no parent.
func TestResolve_RootPage(t *testing.T) {
	if got := Resolve("OptiscanGroup", Placement{}); got.Parent != "" {
		t.Errorf("root Parent = %q, want \"\"", got.Parent)
	}
}
