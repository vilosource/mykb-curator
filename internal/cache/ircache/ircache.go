// Package ircache memoises rendered IR by content hash so the
// expensive frontends (editorial / LLM-driven) don't re-run when
// nothing material has changed.
//
// Key shape:
//
//		sha256( specHash || kbSubsetHash || pipelineVersion )
//
//	  - specHash       — content hash of the spec file (set by parser).
//	  - kbSubsetHash   — hash of just the kb areas the spec declares
//	                     in include:; areas outside the include list
//	                     don't invalidate the cache (otherwise two
//	                     specs sharing the same kb but with different
//	                     includes would constantly evict each other).
//	  - pipelineVersion — curator-side bump-stamp; lets us invalidate
//	                     every entry at once when frontend/pass
//	                     semantics change.
//
// Encoding: gob. Pros: handles the IR Block interface via registered
// types out of the box; compact; fast. Cons: not human-readable.
// Acceptable because this is a runtime cache (not a test fixture).
package ircache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
)

func init() {
	// Register every IR block kind so gob's interface decoding finds
	// the concrete type at decode time. Adding a new block kind in
	// ir/ requires adding it here too — failing fast is preferred to
	// silently dropping content.
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

// Cache stores rendered IR documents keyed by content hash.
type Cache struct {
	dir string
}

// Open opens (or creates) the cache directory.
func Open(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ircache: mkdir %q: %w", dir, err)
	}
	return &Cache{dir: dir}, nil
}

// Key computes the cache key for (specHash, kbSubsetHash, pipelineVersion).
func Key(specHash, kbSubsetHash, pipelineVersion string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s", specHash, kbSubsetHash, pipelineVersion)
	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the cached IR for the given key, or (nil, false, nil)
// on miss. Decode errors return (nil, false, err) — caller decides
// whether to fall through to a regenerate (typically yes).
func (c *Cache) Get(key string) (*ir.Document, bool, error) {
	path := filepath.Join(c.dir, key+".gob")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("ircache: read %s: %w", path, err)
	}
	var doc ir.Document
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		return nil, false, fmt.Errorf("ircache: decode %s: %w", path, err)
	}
	return &doc, true, nil
}

// Set persists doc under the given key. Atomic via tmp + rename.
func (c *Cache) Set(key string, doc ir.Document) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(doc); err != nil {
		return fmt.Errorf("ircache: encode: %w", err)
	}
	path := filepath.Join(c.dir, key+".gob")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("ircache: write tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

// HashKBSubset computes the kb_subset_hash for a (snapshot, include)
// pair. Only areas declared in include affect the hash — areas
// outside the include list are filtered out before hashing.
//
// Stable across area-list order: areas are sorted by ID before
// hashing, so a snapshot with the same content but a different
// area iteration order produces the same hash.
func HashKBSubset(snap kb.Snapshot, include specs.IncludeFilter) string {
	want := make(map[string]bool, len(include.Areas))
	for _, a := range include.Areas {
		want[a] = true
	}

	var inScope []kb.Area
	for _, a := range snap.Areas {
		if !want[a.ID] {
			continue
		}
		inScope = append(inScope, a)
	}
	sort.Slice(inScope, func(i, j int) bool { return inScope[i].ID < inScope[j].ID })

	h := sha256.New()
	for _, a := range inScope {
		fmt.Fprintf(h, "area:%s|name:%s|summary:%s\n", a.ID, a.Name, a.Summary)
		// Sort entries within an area too — JSONL files might come
		// back in any order; hash must be stable.
		entries := make([]kb.Entry, len(a.Entries))
		copy(entries, a.Entries)
		sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
		for _, e := range entries {
			fmt.Fprintf(h, "  entry:%s|type:%s|text:%s|why:%s|rejected:%s|context:%s|url:%s|zone:%s|prov:%s/%s\n",
				e.ID, e.Type, e.Text, e.Why, e.Rejected, e.Context, e.URL, e.Zone,
				e.Provenance.Status, e.Provenance.Source)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
