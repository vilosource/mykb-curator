package localfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const vaultDoc = `
topic: Vault
parent:
  page: Vault Architecture
  kind: architecture
  audience: human-operator
  intent: A human understands Vault.
  sections:
    - title: System Architecture
      intent: Topology + unseal.
      sources: ["kb:area=vault"]
children:
  - page: Vault Operations
    kind: runbook
    intent: Day-2 ops.
`

func TestPull_ParsesDocYamlSortedByID(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "b", "vault.doc.yaml"), vaultDoc)
	mustWrite(t, filepath.Join(dir, "a.doc.yaml"), vaultDoc)
	mustWrite(t, filepath.Join(dir, "ignored.spec.md"), "not a docspec")

	got, err := New(dir).Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 doc files (spec.md ignored), got %d", len(got))
	}
	if got[0].ID != "a.doc.yaml" || got[1].ID != filepath.Join("b", "vault.doc.yaml") {
		t.Errorf("not sorted by rel-path ID: %q %q", got[0].ID, got[1].ID)
	}
	if got[0].Spec.Topic != "Vault" || len(got[0].Spec.Children) != 1 {
		t.Errorf("parsed cluster wrong: %+v", got[0].Spec)
	}
}

func TestPull_MissingRootIsZeroNotError(t *testing.T) {
	got, err := New(filepath.Join(t.TempDir(), "nope")).Pull(context.Background())
	if err != nil || got != nil {
		t.Fatalf("missing root must be zero files, not an error: got=%v err=%v", got, err)
	}
}

func TestPull_BadDocSpecAbortsPull(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "bad.doc.yaml"), "topic: T\nparent:\n  page: P\n  kind: bogus\n")
	if _, err := New(dir).Pull(context.Background()); err == nil {
		t.Fatal("a malformed cluster file must abort the pull, not skip-and-continue")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
