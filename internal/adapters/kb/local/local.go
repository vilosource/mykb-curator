// Package local implements kb.Source by reading the mykb on-disk
// layout from a filesystem path.
//
// On-disk layout (mirrors the mykb canonical brain — see mykb's
// CLAUDE.md):
//
//	<root>/
//	├── areas/
//	│   └── <id>/
//	│       ├── area.json        # metadata: id, name, summary, tags
//	│       ├── facts.jsonl
//	│       ├── decisions.jsonl
//	│       ├── gotchas.jsonl
//	│       ├── patterns.jsonl
//	│       └── links.jsonl
//	├── manifest.json             # ignored — derived from areas/
//	├── kb.db                     # ignored — SQLite index, not used
//	└── workspaces/               # ignored — separate concern
//
// All JSONL files are optional individually. Missing area.json on an
// existing area directory is an error (corrupt area).
//
// Read-only. Writes go through the kb-maintenance pipeline's PR
// backend, never through this adapter.
package local

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
)

// Source is a kb.Source backed by a filesystem path.
type Source struct {
	root string
}

// New constructs a Source rooted at dir. Existence is checked on Pull.
func New(dir string) *Source { return &Source{root: dir} }

// Whoami returns a human-readable identity for run reports.
func (s *Source) Whoami() string { return "local:" + s.root }

// DiffSince always returns ErrDiffNotSupported — a filesystem path
// has no commit history, so we can't tell what changed. Orchestrator
// falls back to rendering all specs.
func (s *Source) DiffSince(_ context.Context, _ string) ([]string, error) {
	return nil, kb.ErrDiffNotSupported
}

// Pull loads the kb snapshot from disk.
func (s *Source) Pull(_ context.Context) (kb.Snapshot, error) {
	areasDir := filepath.Join(s.root, "areas")
	info, err := os.Stat(areasDir)
	if err != nil {
		return kb.Snapshot{}, fmt.Errorf("local kb: stat %q: %w", areasDir, err)
	}
	if !info.IsDir() {
		return kb.Snapshot{}, fmt.Errorf("local kb: %q is not a directory", areasDir)
	}

	entries, err := os.ReadDir(areasDir)
	if err != nil {
		return kb.Snapshot{}, fmt.Errorf("local kb: read %q: %w", areasDir, err)
	}

	var areas []kb.Area
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		a, err := loadArea(filepath.Join(areasDir, e.Name()))
		if err != nil {
			return kb.Snapshot{}, err
		}
		areas = append(areas, a)
	}

	sort.Slice(areas, func(i, j int) bool { return areas[i].ID < areas[j].ID })
	return kb.Snapshot{Areas: areas}, nil
}

// loadArea reads one area directory.
func loadArea(dir string) (kb.Area, error) {
	areaJSON := filepath.Join(dir, "area.json")
	meta, err := os.ReadFile(areaJSON)
	if err != nil {
		return kb.Area{}, fmt.Errorf("local kb: read %s: %w", areaJSON, err)
	}
	var m struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Summary string   `json:"summary"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		return kb.Area{}, fmt.Errorf("local kb: parse %s: %w", areaJSON, err)
	}

	a := kb.Area{
		ID:      m.ID,
		Name:    m.Name,
		Summary: m.Summary,
		Tags:    m.Tags,
	}

	// Optional entry files: any missing file is treated as zero entries.
	files := []string{"facts.jsonl", "decisions.jsonl", "gotchas.jsonl", "patterns.jsonl", "links.jsonl"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		entries, err := loadJSONL(path)
		if err != nil {
			if errIsNotExist(err) {
				continue
			}
			return kb.Area{}, fmt.Errorf("local kb: %s: %w", path, err)
		}
		a.Entries = append(a.Entries, entries...)
	}

	return a, nil
}

// loadJSONL reads one JSONL file (one JSON object per non-blank line).
func loadJSONL(path string) ([]kb.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []kb.Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var raw struct {
			ID         string   `json:"id"`
			Area       string   `json:"area"`
			Type       string   `json:"type"`
			Text       string   `json:"text"`
			Tags       []string `json:"tags"`
			Zone       string   `json:"zone"`
			Created    string   `json:"created"`
			Updated    string   `json:"updated"`
			Provenance struct {
				Status string `json:"status"`
				Source string `json:"source"`
			} `json:"provenance"`
			Why      string `json:"why"`
			Rejected string `json:"rejected"`
			Context  string `json:"context"`
			URL      string `json:"url"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse entry: %w (line: %s)", err, line)
		}
		out = append(out, kb.Entry{
			ID:         raw.ID,
			Area:       raw.Area,
			Type:       raw.Type,
			Text:       raw.Text,
			Tags:       raw.Tags,
			Zone:       raw.Zone,
			Created:    raw.Created,
			Updated:    raw.Updated,
			Provenance: kb.EntryProvenance{Status: raw.Provenance.Status, Source: raw.Provenance.Source},
			Why:        raw.Why,
			Rejected:   raw.Rejected,
			Context:    raw.Context,
			URL:        raw.URL,
		})
	}
	return out, sc.Err()
}

func errIsNotExist(err error) bool {
	return err != nil && (os.IsNotExist(err) || isPathErrorNotExist(err))
}

func isPathErrorNotExist(err error) bool {
	var pe *fs.PathError
	if errAs(err, &pe) {
		return os.IsNotExist(pe.Err)
	}
	return false
}

// errAs is a tiny wrapper around errors.As for readability above.
func errAs(err error, target any) bool {
	type aser interface{ As(any) bool }
	if a, ok := err.(aser); ok {
		return a.As(target)
	}
	return false
}
