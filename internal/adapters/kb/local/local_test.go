package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
)

// writeArea creates a minimal mykb area tree under root.
// area.json + the listed entries grouped by type into JSONL files.
func writeArea(t *testing.T, root, id, name string, entries []kb.Entry) {
	t.Helper()
	areaDir := filepath.Join(root, "areas", id)
	if err := os.MkdirAll(areaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	meta := map[string]any{
		"id": id, "name": name, "summary": "summary of " + id,
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(areaDir, "area.json"), metaJSON, 0o600); err != nil {
		t.Fatalf("write area.json: %v", err)
	}

	// Group entries by type → JSONL file.
	byType := map[string][]kb.Entry{}
	for _, e := range entries {
		byType[e.Type] = append(byType[e.Type], e)
	}
	files := map[string]string{
		"fact":     "facts.jsonl",
		"decision": "decisions.jsonl",
		"gotcha":   "gotchas.jsonl",
		"pattern":  "patterns.jsonl",
		"link":     "links.jsonl",
	}
	for typ, name := range files {
		path := filepath.Join(areaDir, name)
		var lines []byte
		for _, e := range byType[typ] {
			b, _ := json.Marshal(e)
			lines = append(lines, b...)
			lines = append(lines, '\n')
		}
		// Always create the file (even empty), matches mykb convention.
		if err := os.WriteFile(path, lines, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

// The real ~/.mykb brain has entries (and areas) whose `tags` is a
// comma/space-separated STRING, not a JSON array. The adapter must
// accept both shapes — a strict decoder aborted the whole run on
// one such entry when first pointed at the real brain.
func TestPull_ToleratesCSVStringTags(t *testing.T) {
	root := t.TempDir()
	areaDir := filepath.Join(root, "areas", "ai-integration")
	if err := os.MkdirAll(areaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// area.json with tags as a CSV string
	if err := os.WriteFile(filepath.Join(areaDir, "area.json"),
		[]byte(`{"id":"ai-integration","name":"AI","summary":"s","tags":"ai, entra sso"}`), 0o600); err != nil {
		t.Fatalf("write area.json: %v", err)
	}
	// one fact with CSV-string tags, one with a JSON array — both valid
	facts := `{"id":"f1","area":"ai-integration","type":"fact","text":"csv tags","tags":"copilot,entra,sso,plugin","zone":"active"}
{"id":"f2","area":"ai-integration","type":"fact","text":"array tags","tags":["x","y"],"zone":"active"}
`
	if err := os.WriteFile(filepath.Join(areaDir, "facts.jsonl"), []byte(facts), 0o600); err != nil {
		t.Fatalf("write facts: %v", err)
	}

	snap, err := New(root).Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull must tolerate CSV-string tags, got: %v", err)
	}
	a := snap.Area("ai-integration")
	if a == nil {
		t.Fatalf("area not loaded")
	}
	if !equalStrs(a.Tags, []string{"ai", "entra", "sso"}) {
		t.Errorf("area tags = %v, want [ai entra sso]", a.Tags)
	}
	var f1, f2 *kb.Entry
	for i := range a.Entries {
		switch a.Entries[i].ID {
		case "f1":
			f1 = &a.Entries[i]
		case "f2":
			f2 = &a.Entries[i]
		}
	}
	if f1 == nil || !equalStrs(f1.Tags, []string{"copilot", "entra", "sso", "plugin"}) {
		t.Errorf("f1 CSV tags parsed wrong: %+v", f1)
	}
	if f2 == nil || !equalStrs(f2.Tags, []string{"x", "y"}) {
		t.Errorf("f2 array tags parsed wrong: %+v", f2)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPull_ReadsAreaMetadata(t *testing.T) {
	root := t.TempDir()
	writeArea(t, root, "vault", "Vault", nil)

	snap, err := New(root).Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(snap.Areas) != 1 {
		t.Fatalf("len(Areas) = %d, want 1", len(snap.Areas))
	}
	if snap.Areas[0].ID != "vault" {
		t.Errorf("Area.ID = %q, want %q", snap.Areas[0].ID, "vault")
	}
	if snap.Areas[0].Name != "Vault" {
		t.Errorf("Area.Name = %q, want %q", snap.Areas[0].Name, "Vault")
	}
}

func TestPull_ReadsFactsAndDecisionsAndGotchasAndPatternsAndLinks(t *testing.T) {
	root := t.TempDir()
	entries := []kb.Entry{
		{ID: "f1", Type: "fact", Text: "vault facts one", Zone: "active"},
		{ID: "d1", Type: "decision", Text: "DEC-001", Why: "for reasons", Zone: "active"},
		{ID: "g1", Type: "gotcha", Text: "watch the unseal", Zone: "active"},
		{ID: "p1", Type: "pattern", Text: "auto-unseal pattern", Zone: "active"},
		{ID: "l1", Type: "link", Text: "docs", URL: "https://example.com", Zone: "active"},
	}
	writeArea(t, root, "vault", "Vault", entries)

	snap, err := New(root).Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	a := snap.Area("vault")
	if a == nil {
		t.Fatalf("Area(vault) is nil")
	}
	if len(a.Entries) != 5 {
		t.Fatalf("len(Entries) = %d, want 5; got %+v", len(a.Entries), a.Entries)
	}
	if got := a.EntriesByType("decision"); len(got) != 1 || got[0].Why != "for reasons" {
		t.Errorf("decision lookup failed: %+v", got)
	}
	if got := a.EntriesByType("link"); len(got) != 1 || got[0].URL != "https://example.com" {
		t.Errorf("link lookup failed: %+v", got)
	}
}

func TestPull_MultipleAreasInDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	writeArea(t, root, "vault", "Vault", nil)
	writeArea(t, root, "harbor", "Harbor", nil)
	writeArea(t, root, "gitlab", "GitLab", nil)

	snap, _ := New(root).Pull(context.Background())
	if len(snap.Areas) != 3 {
		t.Fatalf("len(Areas) = %d, want 3", len(snap.Areas))
	}
	wantOrder := []string{"gitlab", "harbor", "vault"}
	for i, w := range wantOrder {
		if snap.Areas[i].ID != w {
			t.Errorf("Areas[%d].ID = %q, want %q (must be sorted for determinism)", i, snap.Areas[i].ID, w)
		}
	}
}

func TestPull_IgnoresNonAreaDirsAndFiles(t *testing.T) {
	root := t.TempDir()
	writeArea(t, root, "vault", "Vault", nil)
	// Non-area sibling dirs that mykb has — manifest.json, workspaces/, kb.db
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspaces", "x"), 0o755); err != nil {
		t.Fatalf("workspaces: %v", err)
	}

	snap, _ := New(root).Pull(context.Background())
	if len(snap.Areas) != 1 {
		t.Errorf("len(Areas) = %d, want 1 (non-area entries must be ignored)", len(snap.Areas))
	}
}

func TestPull_AreaMissingMetadata_IsError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "areas", "broken"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No area.json — that's a corrupt area; should surface as an error.
	if _, err := New(root).Pull(context.Background()); err == nil {
		t.Errorf("expected error for area missing area.json, got nil")
	}
}

func TestPull_AreaWithoutAnyEntryFiles_IsValid(t *testing.T) {
	root := t.TempDir()
	areaDir := filepath.Join(root, "areas", "empty-area")
	_ = os.MkdirAll(areaDir, 0o755)
	meta := map[string]string{"id": "empty-area", "name": "Empty", "summary": ""}
	b, _ := json.Marshal(meta)
	_ = os.WriteFile(filepath.Join(areaDir, "area.json"), b, 0o600)
	// No JSONL files — a brand-new area is valid.

	snap, err := New(root).Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if a := snap.Area("empty-area"); a == nil || len(a.Entries) != 0 {
		t.Errorf("empty area should produce zero entries; got %+v", a)
	}
}

func TestWhoami_DescribesRoot(t *testing.T) {
	if got := New("/tmp/x").Whoami(); got == "" {
		t.Errorf("Whoami empty")
	}
}
