// Package manifest persists the set of wiki page titles the curator
// owned as of the last run. Diffing it against the pages produced this
// run is how orphan-pruning detects pages whose spec was deleted or
// renamed (docs/navigation-DESIGN.md §11).
package manifest

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
)

// Store is a JSON-file-backed page-ownership manifest.
type Store struct{ path string }

// Open returns a manifest store backed by the given file path.
func Open(path string) *Store { return &Store{path: path} }

// Load returns the owned page set. A missing file is not an error —
// it yields an empty set (first run, nothing to prune against).
func (s *Store) Load() (map[string]bool, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var pages []string
	if err := json.Unmarshal(b, &pages); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(pages))
	for _, p := range pages {
		out[p] = true
	}
	return out, nil
}

// Save writes the owned page set (sorted, for stable diffs) atomically.
func (s *Store) Save(pages map[string]bool) error {
	out := make([]string, 0, len(pages))
	for p := range pages {
		out = append(out, p)
	}
	sort.Strings(out)
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
