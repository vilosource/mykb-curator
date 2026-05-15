package localfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeSpec(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

const validSpec = `---
wiki: acme
page: P
kind: projection
include:
  areas: [vault]
---
body
`

func TestStore_Pull_ReadsAllSpecFiles(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "page-a.spec.md", validSpec)
	writeSpec(t, dir, "page-b.spec.md", validSpec)
	writeSpec(t, dir, "pages/nested.spec.md", validSpec)
	// non-spec files must be ignored
	writeSpec(t, dir, "README.md", "not a spec")
	writeSpec(t, dir, "index.yaml", "x: 1")

	s := New(dir)
	got, err := s.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (.spec.md files); got: %+v", len(got), got)
	}
}

func TestStore_Pull_StableOrderingForDeterministicReports(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "z.spec.md", validSpec)
	writeSpec(t, dir, "a.spec.md", validSpec)
	writeSpec(t, dir, "m.spec.md", validSpec)

	s := New(dir)
	got, _ := s.Pull(context.Background())
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []string{"a.spec.md", "m.spec.md", "z.spec.md"}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("position %d: ID = %q, want %q", i, got[i].ID, w)
		}
	}
}

func TestStore_Pull_FailsLoudlyOnInvalidSpec(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "good.spec.md", validSpec)
	writeSpec(t, dir, "bad.spec.md", "no frontmatter here, just text")

	s := New(dir)
	_, err := s.Pull(context.Background())
	if err == nil {
		t.Errorf("expected error for invalid spec, got nil (one bad spec must fail the run)")
	}
}

func TestStore_Pull_EmptyDirReturnsEmptySlice(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	got, err := s.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestStore_Whoami_DescribesPath(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if got := s.Whoami(); got == "" {
		t.Errorf("Whoami returned empty string")
	}
}

func TestStore_Pull_RecordsIDRelativeToRoot(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "pages/nested/deep.spec.md", validSpec)

	s := New(dir)
	got, _ := s.Pull(context.Background())
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := filepath.Join("pages", "nested", "deep.spec.md")
	if got[0].ID != want {
		t.Errorf("ID = %q, want %q", got[0].ID, want)
	}
}
