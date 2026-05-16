// Package git is the read-only git: source resolver.
//
// Grammar (the docspec.Source.Spec after "git:"):
//
//	<repo>[ ref=<rev>][ file=<subpath>]
//
//	<repo>  required; resolved to a local clone dir via, in order:
//	        an exact key in the configured repos map; else
//	        <root>/<repo>; else <repo> if it is an absolute path.
//	ref     optional git revision (default HEAD).
//	file    optional path within the repo — single-file mode.
//
// Every operation is read-only and offline: rev-parse / ls-tree /
// show only. No fetch, pull, checkout, or working-tree write. This
// is why git: is exempt from the execution-policy gate that blocks
// cmd:/ssh:/az: (adoption issue #12).
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/sources"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

const (
	maxFileBytes = 4000 // per-file excerpt cap fed to the LLM
	maxFiles     = 6    // curated files read in repo-digest mode
	maxTreePaths = 80   // tracked-path listing cap
)

// curatedExact are exact base filenames worth surfacing for an
// infra/IaC repo digest (lower-cased). README*/docker-compose* are
// matched by prefix, *.tf/*.hcl by extension — see isCurated.
var curatedExact = map[string]bool{
	"dockerfile": true, "makefile": true,
}

// Resolver resolves git: sources against local clones.
type Resolver struct {
	root  string            // base dir holding clones (optional)
	repos map[string]string // explicit name -> abs path (optional)
}

// New builds a Resolver. root is a directory containing clones
// (e.g. ~/GitLab); repos is an explicit name→path override map.
// Either or both may be empty.
func New(root string, repos map[string]string) *Resolver {
	return &Resolver{root: root, repos: repos}
}

// Scheme implements sources.Resolver.
func (*Resolver) Scheme() string { return "git" }

// Resolve implements sources.Resolver. Read-only; never fabricates.
func (r *Resolver) Resolve(ctx context.Context, s docspec.Source) (sources.Resolved, bool, error) {
	if s.Scheme != "git" {
		return sources.Resolved{}, false, nil
	}
	repoSpec, ref, file := parseSpec(s.Spec)
	if repoSpec == "" {
		return sources.Resolved{}, false, fmt.Errorf("git: source %q has no repo", s.Raw)
	}
	dir, ok := r.repoDir(repoSpec)
	if !ok {
		// Declared but this resolver cannot locate it — keep the
		// honest pending placeholder, do not error the whole page.
		return sources.Resolved{}, false, nil
	}
	if _, err := r.out(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return sources.Resolved{}, false, fmt.Errorf("git: %s is not a git work tree: %w", dir, err)
	}
	commit, err := r.out(ctx, dir, "rev-parse", "--short", ref)
	if err != nil {
		return sources.Resolved{}, false, fmt.Errorf("git: rev-parse %s: %w", ref, err)
	}
	commit = strings.TrimSpace(commit)

	if file != "" {
		return r.resolveFile(ctx, dir, repoSpec, ref, commit, file)
	}
	return r.resolveRepo(ctx, dir, repoSpec, ref, commit)
}

func (r *Resolver) resolveFile(ctx context.Context, dir, repo, ref, commit, file string) (sources.Resolved, bool, error) {
	clean := path.Clean(file)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return sources.Resolved{}, false, fmt.Errorf("git: file %q escapes the repo", file)
	}
	content, err := r.out(ctx, dir, "show", ref+":"+clean)
	if err != nil {
		return sources.Resolved{}, false, fmt.Errorf("git: show %s:%s: %w", ref, clean, err)
	}
	excerpt, truncated := capBytes(content)
	ref0 := fmt.Sprintf("git:%s@%s:%s", repo, commit, clean)
	var d strings.Builder
	fmt.Fprintf(&d, "### git: %s @ %s — %s\n", repo, commit, clean)
	d.WriteString("```\n")
	d.WriteString(excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		d.WriteByte('\n')
	}
	if truncated {
		fmt.Fprintf(&d, "... [truncated at %d bytes]\n", maxFileBytes)
	}
	d.WriteString("```\n")
	return sources.Resolved{
		Digest: d.String(),
		Rows:   [][]string{{"file", ref0, summarise(content)}},
		Refs:   []string{ref0},
	}, true, nil
}

func (r *Resolver) resolveRepo(ctx context.Context, dir, repo, ref, commit string) (sources.Resolved, bool, error) {
	treeOut, err := r.out(ctx, dir, "ls-tree", "-r", "--name-only", ref)
	if err != nil {
		return sources.Resolved{}, false, fmt.Errorf("git: ls-tree %s: %w", ref, err)
	}
	all := splitNonEmpty(treeOut)
	sort.Strings(all)
	tree := all
	treeTrunc := false
	if len(tree) > maxTreePaths {
		tree = tree[:maxTreePaths]
		treeTrunc = true
	}

	curated := pickCurated(all)
	ref0 := fmt.Sprintf("git:%s@%s", repo, commit)

	var d strings.Builder
	fmt.Fprintf(&d, "### git: %s @ %s (%d tracked files)\n", repo, commit, len(all))
	d.WriteString("Tracked paths:\n")
	for _, p := range tree {
		fmt.Fprintf(&d, "- %s\n", p)
	}
	if treeTrunc {
		fmt.Fprintf(&d, "- ... (%d more)\n", len(all)-maxTreePaths)
	}

	rows := [][]string{{"repo", ref0, fmt.Sprintf("%d tracked files at %s", len(all), commit)}}
	for _, f := range curated {
		content, e := r.out(ctx, dir, "show", ref+":"+f)
		if e != nil {
			continue
		}
		excerpt, truncated := capBytes(content)
		fmt.Fprintf(&d, "\n#### %s\n```\n%s", f, excerpt)
		if !strings.HasSuffix(excerpt, "\n") {
			d.WriteByte('\n')
		}
		if truncated {
			fmt.Fprintf(&d, "... [truncated at %d bytes]\n", maxFileBytes)
		}
		d.WriteString("```\n")
		rows = append(rows, []string{"file", "git:" + repo + "@" + commit + ":" + f, summarise(content)})
	}
	return sources.Resolved{Digest: d.String(), Rows: rows, Refs: []string{ref0}}, true, nil
}

// repoDir resolves a repo spec to a local directory.
func (r *Resolver) repoDir(repo string) (string, bool) {
	if p, ok := r.repos[repo]; ok && p != "" {
		return p, true
	}
	if strings.HasPrefix(repo, "/") {
		return repo, true
	}
	if r.root != "" {
		return path.Join(r.root, repo), true
	}
	return "", false
}

// parseSpec splits "<repo> ref=x file=y" into its parts. The first
// bare (non k=v) token is the repo; order of k=v tokens is free.
func parseSpec(spec string) (repo, ref, file string) {
	ref = "HEAD"
	for _, tok := range strings.Fields(spec) {
		switch {
		case strings.HasPrefix(tok, "ref="):
			ref = strings.TrimPrefix(tok, "ref=")
		case strings.HasPrefix(tok, "file="):
			file = strings.TrimPrefix(tok, "file=")
		case repo == "":
			repo = tok
		}
	}
	return repo, ref, file
}

// pickCurated selects up to maxFiles "interesting" tracked paths,
// preferring shallow ones (a root README beats a vendored one).
func pickCurated(paths []string) []string {
	type cand struct {
		p     string
		depth int
	}
	var cs []cand
	for _, p := range paths {
		if isCurated(path.Base(p)) {
			cs = append(cs, cand{p, strings.Count(p, "/")})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].depth != cs[j].depth {
			return cs[i].depth < cs[j].depth
		}
		return cs[i].p < cs[j].p
	})
	out := make([]string, 0, maxFiles)
	for _, c := range cs {
		if len(out) == maxFiles {
			break
		}
		out = append(out, c.p)
	}
	return out
}

func isCurated(base string) bool {
	b := strings.ToLower(base)
	if curatedExact[b] {
		return true
	}
	if strings.HasPrefix(b, "readme") || strings.HasPrefix(b, "docker-compose") {
		return true
	}
	switch path.Ext(b) {
	case ".tf", ".hcl":
		return true
	}
	return false
}

func capBytes(s string) (string, bool) {
	if len(s) <= maxFileBytes {
		return s, false
	}
	return s[:maxFileBytes], true
}

func summarise(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			if len(t) > 120 {
				return t[:120]
			}
			return t
		}
	}
	return ""
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// out executes a read-only git subcommand and returns stdout. Only
// non-mutating subcommands are ever passed here.
func (r *Resolver) out(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
