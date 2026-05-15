// Package localfs implements specs.Store backed by a directory on
// the local filesystem.
//
// Walks the root directory recursively, parses every *.spec.md file,
// returns them sorted by relative path (so run reports are
// deterministic).
//
// One bad spec fails the whole Pull. The curator's run report names
// the offender; the operator fixes the spec; the run retries. We
// deliberately do not skip-and-continue, because half-loaded spec
// sets produce silently-broken wikis.
package localfs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/specs/parser"
)

// Store is a specs.Store backed by a directory.
type Store struct {
	root string
}

// New constructs a Store rooted at dir. The directory does not need
// to exist at construction time — that's checked on Pull, so test
// fixtures can construct stores before populating directories.
func New(dir string) *Store {
	return &Store{root: dir}
}

// Whoami returns a human-readable identity for run reports.
func (s *Store) Whoami() string {
	return "localfs:" + s.root
}

// Pull walks the root, parses every *.spec.md file, returns the slice
// sorted by ID (relative path). Any parse error aborts the pull.
func (s *Store) Pull(_ context.Context) ([]specs.Spec, error) {
	info, err := os.Stat(s.root)
	if err != nil {
		return nil, fmt.Errorf("localfs specs: stat %q: %w", s.root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("localfs specs: %q is not a directory", s.root)
	}

	var out []specs.Spec
	walkErr := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".spec.md") {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return fmt.Errorf("rel %q: %w", path, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		spec, err := parser.Parse(rel, content)
		if err != nil {
			return err
		}
		out = append(out, spec)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("localfs specs (root=%s): %w", s.root, walkErr)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
