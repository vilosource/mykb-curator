package git

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// makeRepo creates a git repo at dir with one commit containing the
// given files. Returns the commit hash.
func makeRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatalf("add %s: %v", path, err)
		}
	}
	hash, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return hash.String()
}

// makeAreaJSON marshals an area.json for the test fixture.
func makeAreaJSON(id, name string) string {
	b, _ := json.Marshal(map[string]string{"id": id, "name": name, "summary": "test"})
	return string(b)
}

func TestPull_ClonesRepoAndReadsKB(t *testing.T) {
	srcDir := t.TempDir()
	makeRepo(t, srcDir, map[string]string{
		"areas/vault/area.json":   makeAreaJSON("vault", "Vault"),
		"areas/vault/facts.jsonl": `{"id":"f1","type":"fact","text":"fact one"}` + "\n",
	})

	workDir := t.TempDir()
	src := New(srcDir, workDir)
	snap, err := src.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if snap.Commit == "" {
		t.Errorf("Commit empty (should be HEAD hash)")
	}
	if len(snap.Areas) != 1 {
		t.Fatalf("len(Areas) = %d, want 1", len(snap.Areas))
	}
	if snap.Areas[0].ID != "vault" {
		t.Errorf("Area.ID = %q, want %q", snap.Areas[0].ID, "vault")
	}
	if len(snap.Areas[0].EntriesByType("fact")) != 1 {
		t.Errorf("expected 1 fact in vault")
	}
}

func TestPull_RecordsCommitHash(t *testing.T) {
	srcDir := t.TempDir()
	commit := makeRepo(t, srcDir, map[string]string{
		"areas/vault/area.json": makeAreaJSON("vault", "Vault"),
	})

	src := New(srcDir, t.TempDir())
	snap, err := src.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if snap.Commit != commit {
		t.Errorf("Commit = %q, want %q", snap.Commit, commit)
	}
}

func TestPull_SecondPull_ReusesCloneOrFreshens(t *testing.T) {
	// Second pull on the same Source should not panic / leak; doesn't
	// have to be a no-op (it can fetch + reset), just must succeed.
	srcDir := t.TempDir()
	makeRepo(t, srcDir, map[string]string{
		"areas/vault/area.json": makeAreaJSON("vault", "Vault"),
	})

	src := New(srcDir, t.TempDir())
	if _, err := src.Pull(context.Background()); err != nil {
		t.Fatalf("Pull 1: %v", err)
	}
	if _, err := src.Pull(context.Background()); err != nil {
		t.Fatalf("Pull 2: %v", err)
	}
}

func TestWhoami_DescribesRepo(t *testing.T) {
	if got := New("/tmp/x.git", "/tmp/work").Whoami(); got == "" {
		t.Errorf("Whoami empty")
	}
}

func TestPull_NonexistentRepo_IsError(t *testing.T) {
	src := New("/this/path/does/not/exist", t.TempDir())
	if _, err := src.Pull(context.Background()); err == nil {
		t.Errorf("expected error for nonexistent repo, got nil")
	}
}
