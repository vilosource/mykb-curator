package cluster

import (
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

func TestPageAreas_FromPageAndSectionSources(t *testing.T) {
	p := docspec.DocPage{
		Sources: []docspec.Source{{Scheme: "kb", Spec: "area=vault"}},
		Sections: []docspec.DocSection{
			{Sources: []docspec.Source{{Scheme: "kb", Spec: "area=disaster-recovery zone=active"}}},
			{Sources: []docspec.Source{{Scheme: "git", Spec: "infra/x"}}},   // non-kb ignored
			{Sources: []docspec.Source{{Scheme: "kb", Spec: "area=vault"}}}, // dup collapses
		},
	}
	got := pageAreas(p)
	if len(got) != 2 {
		t.Fatalf("areas = %v, want 2 unique (vault, disaster-recovery)", got)
	}
	// sorted + deduped for a stable key
	if got[0] != "disaster-recovery" || got[1] != "vault" {
		t.Errorf("areas not sorted/deduped: %v", got)
	}
}

// The page hash must change when any content-determining field changes,
// and be stable otherwise (so unchanged pages reuse cached IR+verdict).
func TestHashDocPage_StableAndSensitive(t *testing.T) {
	base := docspec.DocPage{Page: "P", Kind: "architecture", Intent: "x",
		Sections: []docspec.DocSection{{Title: "S", Intent: "do X", Render: ""}}}
	h := hashDocPage(base)
	if h != hashDocPage(base) {
		t.Errorf("hash not stable for identical page")
	}
	changed := base
	changed.Sections = []docspec.DocSection{{Title: "S", Intent: "do Y", Render: ""}} // intent changed
	if hashDocPage(changed) == h {
		t.Errorf("hash must change when a section intent changes")
	}
}

// The cluster key is stable for an unchanged spec, and changes when any
// page (parent or child) changes — so the whole cluster invalidates
// together.
func TestClusterKey_StableAndWholeClusterInvalidation(t *testing.T) {
	spec := docspec.DocSpec{Topic: "Vault",
		Parent:   docspec.DocPage{Page: "Vault Architecture", Intent: "overview"},
		Children: []docspec.DocPage{{Page: "Vault Operations", Intent: "ops"}},
	}
	snap := kb.Snapshot{Commit: "c1"}
	k := ClusterKey(spec, snap, "v1")
	if k != ClusterKey(spec, snap, "v1") {
		t.Errorf("cluster key not stable for unchanged spec")
	}
	// a CHILD change must invalidate the cluster key
	ch := spec
	ch.Children = []docspec.DocPage{{Page: "Vault Operations", Intent: "ops CHANGED"}}
	if ClusterKey(ch, snap, "v1") == k {
		t.Errorf("cluster key must change when a child page changes")
	}
	// pipeline version is part of the key
	if ClusterKey(spec, snap, "v2") == k {
		t.Errorf("cluster key must include pipeline version")
	}
}
