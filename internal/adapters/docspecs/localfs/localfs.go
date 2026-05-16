// Package localfs implements docspecs.Store backed by a directory.
//
// Walks the root recursively, parses every *.doc.yaml file via
// docspec.Parse, returns them sorted by relative path. One bad file
// aborts the whole pull (same rationale as the specs localfs store).
// The directory not existing is NOT an error — it yields zero
// cluster files, so a deployment with only legacy *.spec.md specs
// keeps working unchanged.
package localfs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/docspecs"
	"github.com/vilosource/mykb-curator/internal/specs/docspec"
)

// Store is a docspecs.Store backed by a directory.
type Store struct {
	root string
}

// New constructs a Store rooted at dir.
func New(dir string) *Store { return &Store{root: dir} }

// Whoami returns a human-readable identity for run reports.
func (s *Store) Whoami() string { return "localfs-docspec:" + s.root }

// Pull walks the root, parses every *.doc.yaml file, returns the
// slice sorted by ID. A missing root yields zero files (not an
// error); any parse error aborts the pull.
func (s *Store) Pull(_ context.Context) ([]docspecs.File, error) {
	info, err := os.Stat(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("localfs docspecs: stat %q: %w", s.root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("localfs docspecs: %q is not a directory", s.root)
	}

	var out []docspecs.File
	walkErr := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".doc.yaml") {
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
		spec, err := docspec.Parse(content)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		out = append(out, docspecs.File{ID: rel, Spec: spec})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("localfs docspecs (root=%s): %w", s.root, walkErr)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
