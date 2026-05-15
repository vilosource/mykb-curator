//go:build contract

package contract_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	kbgit "github.com/vilosource/mykb-curator/internal/adapters/kb/git"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
)

// kbBuilder constructs a fresh kb.Source pointed at a freshly-seeded
// kb tree. Each impl needs its own builder because Local takes a path
// and Git takes a remote+workdir; the suite below works against the
// constructed Source.
type kbBuilder func(t *testing.T) kb.Source

var allKBSources = map[string]kbBuilder{
	"local": func(t *testing.T) kb.Source {
		root := t.TempDir()
		seedKBTree(t, root)
		return kblocal.New(root)
	},
	"git": func(t *testing.T) kb.Source {
		repoDir := t.TempDir()
		seedKBTree(t, repoDir)
		mustGitCommit(t, repoDir)
		return kbgit.New(repoDir, t.TempDir())
	},
}

func TestKBSource_Contract(t *testing.T) {
	for name, build := range allKBSources {
		t.Run(name, func(t *testing.T) {
			KBSourceContractSuite(t, build(t))
		})
	}
}

// KBSourceContractSuite asserts behavioural properties every kb.Source
// must satisfy. The fixture seed below is the implicit input contract.
func KBSourceContractSuite(t *testing.T, src kb.Source) {
	t.Helper()

	t.Run("Whoami non-empty", func(t *testing.T) {
		if src.Whoami() == "" {
			t.Errorf("Whoami returned empty string")
		}
	})

	t.Run("Pull returns seeded areas", func(t *testing.T) {
		snap, err := src.Pull(context.Background())
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if len(snap.Areas) < 2 {
			t.Fatalf("len(Areas) = %d, want ≥ 2 (seed has vault + harbor)", len(snap.Areas))
		}
		if a := snap.Area("vault"); a == nil {
			t.Errorf("Area(vault) returned nil")
		}
		if a := snap.Area("harbor"); a == nil {
			t.Errorf("Area(harbor) returned nil")
		}
	})

	t.Run("Pull is idempotent (second call succeeds)", func(t *testing.T) {
		_, err1 := src.Pull(context.Background())
		_, err2 := src.Pull(context.Background())
		if err1 != nil || err2 != nil {
			t.Errorf("Pull errors: %v / %v", err1, err2)
		}
	})

	t.Run("Pull reads entries from JSONL", func(t *testing.T) {
		snap, _ := src.Pull(context.Background())
		a := snap.Area("vault")
		if a == nil {
			t.Fatalf("Area(vault) nil")
		}
		facts := a.EntriesByType("fact")
		if len(facts) == 0 {
			t.Errorf("vault has no facts; seed expects at least one")
		}
	})
}

// seedKBTree writes a small canonical fixture to root. Two areas
// (vault, harbor), each with at least one fact.
func seedKBTree(t *testing.T, root string) {
	t.Helper()
	writeArea(t, root, "vault", "Vault Architecture", "secrets manager",
		`{"id":"vault-f1","area":"vault","type":"fact","text":"Vault HA on Raft","zone":"active"}`,
	)
	writeArea(t, root, "harbor", "Harbor Registry", "container registry",
		`{"id":"harbor-f1","area":"harbor","type":"fact","text":"Harbor v2","zone":"active"}`,
	)
}

func writeArea(t *testing.T, root, id, name, summary string, factsJSONL ...string) {
	t.Helper()
	dir := filepath.Join(root, "areas", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	meta, _ := json.Marshal(map[string]string{"id": id, "name": name, "summary": summary})
	if err := os.WriteFile(filepath.Join(dir, "area.json"), meta, 0o600); err != nil {
		t.Fatalf("area.json: %v", err)
	}
	var body string
	for _, l := range factsJSONL {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "facts.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatalf("facts.jsonl: %v", err)
	}
}

// mustGitCommit init+commit the entire dir so the git backend can read it.
func mustGitCommit(t *testing.T, dir string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := wt.Commit("seed", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@e", When: time.Now()},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
