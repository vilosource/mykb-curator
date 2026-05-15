package prbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

// freshKBRepo creates a tiny git repo simulating a kb working tree
// with a single seed commit on the configured base branch.
func freshKBRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("kb seed"), 0o600)
	_, _ = wt.Add("README.md")
	if _, err := wt.Commit("seed", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@e", When: time.Now()},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

func TestSubmit_CreatesBranchAndWritesSummary(t *testing.T) {
	repo := freshKBRepo(t)
	b := New(repo)

	props := []maintenance.MutationProposal{
		{Kind: maintenance.ProposalDeprecate, Area: "vault", ID: "g1", Source: "link-rot", Reason: "404"},
		{Kind: maintenance.ProposalVerify, Area: "harbor", ID: "f1", Source: "external-truth", Reason: "matched source"},
	}
	res, err := b.Submit(context.Background(), Input{
		RunID:      "abc1234",
		BaseBranch: "master", // default for go-git PlainInit
		Proposals:  props,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !strings.HasPrefix(res.Branch, "curator/maint-") {
		t.Errorf("Branch = %q, want curator/maint- prefix", res.Branch)
	}
	if !strings.Contains(res.Branch, "abc1234") {
		t.Errorf("Branch %q should include run id", res.Branch)
	}
	if res.CommitHash == "" {
		t.Errorf("CommitHash empty")
	}
	if res.Mutations != 2 {
		t.Errorf("Mutations = %d, want 2", res.Mutations)
	}

	// Summary file must exist on the branch with proposal details.
	summary, err := os.ReadFile(filepath.Join(repo, "curator-proposals", "abc1234.md"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	body := string(summary)
	for _, want := range []string{"vault", "g1", "link-rot", "harbor", "f1", "external-truth"} {
		if !strings.Contains(body, want) {
			t.Errorf("summary missing %q\n---\n%s\n---", want, body)
		}
	}
}

func TestSubmit_EmptyProposals_NoCommitMade(t *testing.T) {
	repo := freshKBRepo(t)
	b := New(repo)
	res, err := b.Submit(context.Background(), Input{
		RunID:      "empty",
		BaseBranch: "master",
		Proposals:  nil,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if res.Mutations != 0 {
		t.Errorf("Mutations = %d, want 0", res.Mutations)
	}
	if res.CommitHash != "" {
		t.Errorf("CommitHash = %q, want empty (no commit for zero proposals)", res.CommitHash)
	}
	// No branch should be left dangling.
	if _, err := os.Stat(filepath.Join(repo, "curator-proposals")); err == nil {
		t.Errorf("curator-proposals dir created for zero proposals")
	}
}

func TestSubmit_PRBodyContainsAllProposals(t *testing.T) {
	repo := freshKBRepo(t)
	res, err := New(repo).Submit(context.Background(), Input{
		RunID:      "x",
		BaseBranch: "master",
		Proposals: []maintenance.MutationProposal{
			{Kind: maintenance.ProposalDeprecate, Area: "a", ID: "1", Source: "s1", Reason: "r1"},
			{Kind: maintenance.ProposalAdd, Area: "b", ID: "2", Source: "s2", Reason: "r2", Text: "new fact"},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for _, want := range []string{"deprecate", "add", "r1", "r2", "new fact"} {
		if !strings.Contains(strings.ToLower(res.PRBody), want) {
			t.Errorf("PRBody missing %q\n%s", want, res.PRBody)
		}
	}
}

func TestSubmit_GroupsByArea(t *testing.T) {
	repo := freshKBRepo(t)
	res, _ := New(repo).Submit(context.Background(), Input{
		RunID:      "g",
		BaseBranch: "master",
		Proposals: []maintenance.MutationProposal{
			{Kind: maintenance.ProposalDeprecate, Area: "vault", ID: "v1", Reason: "r"},
			{Kind: maintenance.ProposalVerify, Area: "vault", ID: "v2", Reason: "r"},
			{Kind: maintenance.ProposalDeprecate, Area: "harbor", ID: "h1", Reason: "r"},
		},
	})
	body := res.PRBody
	// Areas appear alphabetically (deterministic ordering); within an
	// area, same-area proposals appear adjacent.
	harborIdx := strings.Index(body, "## Area: harbor")
	vaultIdx := strings.Index(body, "## Area: vault")
	h1Idx := strings.Index(body, "h1")
	v1Idx := strings.Index(body, "v1")
	v2Idx := strings.Index(body, "v2")
	if !(harborIdx < h1Idx && h1Idx < vaultIdx && vaultIdx < v1Idx && v1Idx < v2Idx) {
		t.Errorf("PRBody not grouped by area in alphabetical order:\n%s", body)
	}
}

func TestSubmit_BranchNameIncludesDate(t *testing.T) {
	repo := freshKBRepo(t)
	res, _ := New(repo).Submit(context.Background(), Input{
		RunID:      "d",
		BaseBranch: "master",
		Proposals:  []maintenance.MutationProposal{{Kind: maintenance.ProposalVerify, Area: "a", ID: "1"}},
	})
	// YYYY-MM-DD substring should appear.
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.Contains(res.Branch, today) {
		t.Errorf("Branch %q should include today's date %s", res.Branch, today)
	}
}
