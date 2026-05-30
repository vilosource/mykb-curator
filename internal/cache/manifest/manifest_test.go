package manifest

import (
	"path/filepath"
	"testing"
)

func TestSaveLoad_RoundTrips(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "m.json"))
	want := map[string]bool{"PageA": true, "OptiscanGroup/X": true}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 || !got["PageA"] || !got["OptiscanGroup/X"] {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestLoad_MissingIsEmpty(t *testing.T) {
	got, err := Open(filepath.Join(t.TempDir(), "absent.json")).Load()
	if err != nil {
		t.Fatalf("Load missing must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing manifest should be empty, got %+v", got)
	}
}
