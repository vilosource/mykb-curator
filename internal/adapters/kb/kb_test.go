package kb

import "testing"

func TestSnapshot_ZeroValueIsSafe(t *testing.T) {
	// A zero-value Snapshot must be usable — no nil maps, no panics
	// on basic introspection. The orchestrator constructs a zero
	// Snapshot in error paths before a Source returns one.
	var s Snapshot
	if s.Commit != "" {
		t.Errorf("zero Commit = %q, want empty", s.Commit)
	}
	if s.Areas == nil {
		// Areas is allowed to be nil — Snapshot iteration must handle that.
		// Verify range over nil is fine.
		for range s.Areas {
			t.Errorf("range over nil Areas should not yield items")
		}
	}
	if a := s.Area("nope"); a != nil {
		t.Errorf("Area on empty snapshot returned %+v, want nil", a)
	}
}

func TestSnapshot_AreaLookup(t *testing.T) {
	s := Snapshot{
		Commit: "abc",
		Areas: []Area{
			{ID: "vault", Name: "Vault", Summary: "secrets manager"},
			{ID: "harbor", Name: "Harbor", Summary: "registry"},
		},
	}
	got := s.Area("harbor")
	if got == nil {
		t.Fatalf("Area(harbor) returned nil")
	}
	if got.Name != "Harbor" {
		t.Errorf("Name = %q, want %q", got.Name, "Harbor")
	}
	if missing := s.Area("does-not-exist"); missing != nil {
		t.Errorf("Area(missing) returned %+v, want nil", missing)
	}
}

func TestArea_EntriesByType(t *testing.T) {
	a := Area{
		ID: "vault",
		Entries: []Entry{
			{ID: "f1", Type: "fact", Text: "fact one"},
			{ID: "f2", Type: "fact", Text: "fact two"},
			{ID: "d1", Type: "decision", Text: "decision one", Why: "because"},
			{ID: "g1", Type: "gotcha", Text: "gotcha one"},
		},
	}
	facts := a.EntriesByType("fact")
	if len(facts) != 2 {
		t.Errorf("len(facts) = %d, want 2", len(facts))
	}
	decisions := a.EntriesByType("decision")
	if len(decisions) != 1 {
		t.Errorf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].Why != "because" {
		t.Errorf("decision.Why = %q, want %q", decisions[0].Why, "because")
	}
	if none := a.EntriesByType("nonexistent"); len(none) != 0 {
		t.Errorf("EntriesByType(nonexistent) = %d, want 0", len(none))
	}
}

func TestEntry_ProvenanceFields(t *testing.T) {
	// Spot-check that the type tag carries the provenance fields the
	// curator needs (status, source) — used by the StampVerification
	// pass later.
	e := Entry{
		ID:   "x",
		Type: "fact",
		Provenance: EntryProvenance{
			Status: "verified",
			Source: "design session",
		},
	}
	if e.Provenance.Status != "verified" {
		t.Errorf("Status = %q", e.Provenance.Status)
	}
}
