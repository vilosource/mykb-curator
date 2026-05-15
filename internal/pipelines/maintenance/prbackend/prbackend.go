// Package prbackend turns a slice of MutationProposals into a git
// branch on a kb working tree, ready for the operator to push and
// open a PR.
//
// What this does:
//   - Creates a new branch off baseBranch named
//     curator/maint-YYYY-MM-DD-<runID>
//   - Writes a markdown proposal summary at
//     curator-proposals/<runID>.md
//   - Commits with a structured message
//   - Returns the branch name + commit hash + PR body suggestion
//
// What it deliberately doesn't:
//   - push the branch (caller does, with their own auth/transport)
//   - open the PR (caller invokes `gh pr create` or equivalent)
//   - rewrite the kb's JSONL data files (v0.5: surface proposals
//     for human review; v0.6 will apply structured patches)
//
// Empty proposals = no commit, no branch — keeps history clean.
package prbackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
)

// Backend opens proposal branches against a kb worktree.
type Backend struct {
	repoPath string
}

// New constructs a Backend bound to a kb working tree.
func New(repoPath string) *Backend {
	return &Backend{repoPath: repoPath}
}

// Input is the per-Submit configuration.
type Input struct {
	// RunID is the orchestrator's run identifier; appears in the
	// branch name + summary filename so multiple runs don't collide.
	RunID string

	// BaseBranch is the branch to fork from. Typically "main".
	BaseBranch string

	// Proposals to write to the branch.
	Proposals []maintenance.MutationProposal

	// AuthorName / AuthorEmail are stamped on the commit. Defaults
	// to "mykb-curator" / "mykb-curator@invalid" when empty.
	AuthorName  string
	AuthorEmail string
}

// Result describes what Submit produced.
type Result struct {
	// Branch is the new branch name (curator/maint-<date>-<runID>).
	// Empty for zero-proposal calls.
	Branch string

	// CommitHash is the new commit on the branch. Empty for
	// zero-proposal calls.
	CommitHash string

	// PRBody is a markdown PR body grouping proposals by area, ready
	// to pass to `gh pr create --body`.
	PRBody string

	// Mutations is the number of proposals processed.
	Mutations int
}

// Submit creates the branch + commit. Zero proposals = no-op.
func (b *Backend) Submit(_ context.Context, in Input) (Result, error) {
	if len(in.Proposals) == 0 {
		return Result{}, nil
	}

	branch := fmt.Sprintf("curator/maint-%s-%s", time.Now().UTC().Format("2006-01-02"), in.RunID)
	summaryPath := filepath.Join("curator-proposals", in.RunID+".md")
	body := buildPRBody(in.Proposals, in.RunID)

	repo, err := gogit.PlainOpen(b.repoPath)
	if err != nil {
		return Result{}, fmt.Errorf("prbackend: open repo: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return Result{}, fmt.Errorf("prbackend: worktree: %w", err)
	}

	// Resolve base branch and create the new branch off it.
	baseHash, err := repo.ResolveRevision(plumbing.Revision(in.BaseBranch))
	if err != nil {
		return Result{}, fmt.Errorf("prbackend: resolve base %q: %w", in.BaseBranch, err)
	}
	branchRef := plumbing.NewBranchReferenceName(branch)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, *baseHash)); err != nil {
		return Result{}, fmt.Errorf("prbackend: create branch %q: %w", branch, err)
	}
	// Track the new branch in config so `git push --set-upstream`
	// works naturally when the caller pushes.
	if err := repo.CreateBranch(&config.Branch{Name: branch, Remote: "origin", Merge: branchRef}); err != nil && err != gogit.ErrBranchExists {
		// Best-effort; not having a tracked branch isn't fatal here.
		_ = err
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: branchRef, Force: true}); err != nil {
		return Result{}, fmt.Errorf("prbackend: checkout %q: %w", branch, err)
	}

	// Write summary file.
	full := filepath.Join(b.repoPath, summaryPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Result{}, fmt.Errorf("prbackend: mkdir: %w", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		return Result{}, fmt.Errorf("prbackend: write summary: %w", err)
	}
	if _, err := wt.Add(summaryPath); err != nil {
		return Result{}, fmt.Errorf("prbackend: git add: %w", err)
	}

	author := in.AuthorName
	if author == "" {
		author = "mykb-curator"
	}
	email := in.AuthorEmail
	if email == "" {
		email = "mykb-curator@invalid"
	}
	commitMsg := fmt.Sprintf("curator(maint): %d proposals from run %s", len(in.Proposals), in.RunID)
	hash, err := wt.Commit(commitMsg, &gogit.CommitOptions{
		Author: &object.Signature{Name: author, Email: email, When: time.Now().UTC()},
	})
	if err != nil {
		return Result{}, fmt.Errorf("prbackend: commit: %w", err)
	}

	return Result{
		Branch:     branch,
		CommitHash: hash.String(),
		PRBody:     body,
		Mutations:  len(in.Proposals),
	}, nil
}

// buildPRBody groups proposals by area for a readable PR body.
func buildPRBody(props []maintenance.MutationProposal, runID string) string {
	byArea := map[string][]maintenance.MutationProposal{}
	for _, p := range props {
		byArea[p.Area] = append(byArea[p.Area], p)
	}
	areas := make([]string, 0, len(byArea))
	for a := range byArea {
		areas = append(areas, a)
	}
	sort.Strings(areas)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Curator maintenance proposals — run %s\n\n", runID)
	fmt.Fprintf(&sb, "%d proposal(s) across %d area(s). Review each section and merge selectively.\n\n", len(props), len(areas))

	for _, area := range areas {
		fmt.Fprintf(&sb, "## Area: %s\n\n", area)
		for _, p := range byArea[area] {
			fmt.Fprintf(&sb, "### %s — `%s/%s`\n\n", kindLabel(p.Kind), area, p.ID)
			if p.Reason != "" {
				fmt.Fprintf(&sb, "**Reason:** %s\n\n", p.Reason)
			}
			if p.Source != "" {
				fmt.Fprintf(&sb, "**Source:** `%s`\n\n", p.Source)
			}
			if p.Text != "" {
				fmt.Fprintf(&sb, "**Text:**\n\n> %s\n\n", p.Text)
			}
			if len(p.Evidence) > 0 {
				sb.WriteString("**Evidence:**\n\n")
				keys := make([]string, 0, len(p.Evidence))
				for k := range p.Evidence {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if v := p.Evidence[k]; v != "" {
						fmt.Fprintf(&sb, "- `%s`: %s\n", k, v)
					}
				}
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}

func kindLabel(k maintenance.ProposalKind) string {
	switch k {
	case maintenance.ProposalVerify:
		return "verify"
	case maintenance.ProposalDeprecate:
		return "deprecate"
	case maintenance.ProposalAdd:
		return "add"
	default:
		return "unknown"
	}
}
