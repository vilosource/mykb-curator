// Package clustercache persists a whole docspec cluster's rendered
// output — each page's final (post-refine, post-pass) IR plus its
// Judge verdict — keyed by the cluster's deterministic ClusterKey. On
// a cache hit (unchanged spec + kb) the orchestrator skips both LLM
// synthesis and the Judge, reusing the cached IR + verdict, which
// removes run-to-run nondeterminism (task #3 / docs/navigation-DESIGN
// §11). gob is used (same as ircache) so the IR Block interface
// round-trips via registered types.
package clustercache

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

func init() {
	gob.Register(ir.ProseBlock{})
	gob.Register(ir.MachineBlock{})
	gob.Register(ir.KBRefBlock{})
	gob.Register(ir.TableBlock{})
	gob.Register(ir.IndexBlock{})
	gob.Register(ir.DiagramBlock{})
	gob.Register(ir.Callout{})
	gob.Register(ir.EscapeHatch{})
	gob.Register(ir.MarkerBlock{})
}

// PageResult is one cluster page's cached output: the published IR plus
// the Judge verdict + iteration count, so a hit reproduces both the
// content and the verdict.
type PageResult struct {
	Page    string
	Kind    string
	Doc     ir.Document
	Verdict string
	Iters   int
}

// Cache stores []PageResult keyed by a cluster key.
type Cache struct{ dir string }

// Open opens (or creates) the cache directory.
func Open(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("clustercache: mkdir %q: %w", dir, err)
	}
	return &Cache{dir: dir}, nil
}

// Get returns the cached pages for key, or (nil, false, nil) on miss.
func (c *Cache) Get(key string) ([]PageResult, bool, error) {
	path := filepath.Join(c.dir, key+".gob")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("clustercache: read %s: %w", path, err)
	}
	var pages []PageResult
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&pages); err != nil {
		return nil, false, fmt.Errorf("clustercache: decode %s: %w", path, err)
	}
	return pages, true, nil
}

// Set persists pages under key (atomic via tmp + rename).
func (c *Cache) Set(key string, pages []PageResult) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(pages); err != nil {
		return fmt.Errorf("clustercache: encode: %w", err)
	}
	path := filepath.Join(c.dir, key+".gob")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("clustercache: write tmp: %w", err)
	}
	return os.Rename(tmp, path)
}
