// Package git implements kb.Source by cloning (or fetching into) a
// git working copy and delegating the actual mykb-layout read to the
// local adapter.
//
// Strategy: clone-once-then-fetch.
//   - First Pull: clone src → workDir.
//   - Subsequent Pull: fetch + hard-reset workDir to origin/<branch>.
//
// Authentication is out of scope for v0.1 — accepts whatever
// remote URL go-git can reach (local paths, file://, https with
// system creds, ssh with the user's agent). Token-based auth lands
// when the first real remote needs it.
package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/kb/local"
)

// Source is a kb.Source that pulls from a git repo.
type Source struct {
	remote  string
	workDir string

	// branch is the ref to track. Empty means HEAD of the default
	// branch as the server reports it.
	branch string
}

// New constructs a Source. remote is any go-git-acceptable URL or
// local path. workDir is where the clone lives on disk; it will be
// created if it doesn't exist.
func New(remote, workDir string) *Source {
	return &Source{remote: remote, workDir: workDir}
}

// WithBranch returns a Source pinned to a specific branch.
func (s *Source) WithBranch(branch string) *Source {
	s.branch = branch
	return s
}

// Whoami returns a human-readable identity for run reports.
func (s *Source) Whoami() string {
	return fmt.Sprintf("git:%s", s.remote)
}

// Pull clones or refreshes the working copy, then delegates to the
// local adapter for the read.
func (s *Source) Pull(ctx context.Context) (kb.Snapshot, error) {
	commit, err := s.cloneOrFetch(ctx)
	if err != nil {
		return kb.Snapshot{}, err
	}

	// Delegate the actual file walk to the local adapter — same
	// layout, same parsing, no duplication.
	snap, err := local.New(s.workDir).Pull(ctx)
	if err != nil {
		return kb.Snapshot{}, err
	}
	snap.Commit = commit
	return snap, nil
}

// cloneOrFetch ensures s.workDir is an up-to-date clone of s.remote.
// Returns the HEAD commit hash.
func (s *Source) cloneOrFetch(ctx context.Context) (string, error) {
	if _, err := os.Stat(filepath.Join(s.workDir, ".git")); err == nil {
		return s.fetchAndReset(ctx)
	}
	return s.clone(ctx)
}

func (s *Source) clone(ctx context.Context) (string, error) {
	if err := os.MkdirAll(s.workDir, 0o755); err != nil {
		return "", fmt.Errorf("git kb: mkdir workdir: %w", err)
	}
	opts := &gogit.CloneOptions{URL: s.remote}
	if s.branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(s.branch)
		opts.SingleBranch = true
	}
	repo, err := gogit.PlainCloneContext(ctx, s.workDir, false, opts)
	if err != nil {
		return "", fmt.Errorf("git kb: clone %s: %w", s.remote, err)
	}
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("git kb: HEAD: %w", err)
	}
	return head.Hash().String(), nil
}

func (s *Source) fetchAndReset(ctx context.Context) (string, error) {
	repo, err := gogit.PlainOpen(s.workDir)
	if err != nil {
		return "", fmt.Errorf("git kb: open workdir: %w", err)
	}
	err = repo.FetchContext(ctx, &gogit.FetchOptions{Force: true})
	if err != nil && err != gogit.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("git kb: fetch: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("git kb: worktree: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("git kb: HEAD: %w", err)
	}

	if err := wt.Reset(&gogit.ResetOptions{Mode: gogit.HardReset, Commit: head.Hash()}); err != nil {
		return "", fmt.Errorf("git kb: reset: %w", err)
	}

	return head.Hash().String(), nil
}
