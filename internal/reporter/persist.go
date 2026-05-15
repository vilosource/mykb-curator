// Persistence for Reports — YAML serialisation + on-disk file
// writes with a "latest" symlink for fast access.
//
// One file per run, named with run-id so history is queryable by
// listing the directory. "latest.yaml" is always a symlink to the
// newest run's file.
package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// reportYAML is the on-disk schema. Separate type from Report so the
// in-memory representation can evolve without breaking persisted
// reports — and to keep the YAML tags out of the public Report API.
type reportYAML struct {
	RunID     string           `yaml:"run_id"`
	Wiki      string           `yaml:"wiki"`
	StartedAt time.Time        `yaml:"started_at"`
	EndedAt   time.Time        `yaml:"ended_at"`
	KBCommit  string           `yaml:"kb_commit,omitempty"`
	Specs     []specResultYAML `yaml:"specs,omitempty"`
	Errors    []string         `yaml:"errors,omitempty"`
	Warnings  []string         `yaml:"warnings,omitempty"`
	Metrics   metricsYAML      `yaml:"metrics,omitempty"`
}

type specResultYAML struct {
	ID                string          `yaml:"id"`
	Status            SpecStatus      `yaml:"status"`
	Reason            string          `yaml:"reason,omitempty"`
	NewRevisionID     string          `yaml:"new_revision,omitempty"`
	BlocksRegenerated int             `yaml:"blocks_regenerated,omitempty"`
	HumanEdits        []humanEditYAML `yaml:"human_edits_detected,omitempty"`
}

type humanEditYAML struct {
	BlockID     string     `yaml:"block"`
	Action      EditAction `yaml:"action"`
	Diff        string     `yaml:"diff,omitempty"`
	Explanation string     `yaml:"explanation,omitempty"`
	Suggestion  string     `yaml:"suggestion,omitempty"`
}

type metricsYAML struct {
	LLMCalls         int `yaml:"llm_calls,omitempty"`
	LLMTokensIn      int `yaml:"llm_tokens_in,omitempty"`
	LLMTokensOut     int `yaml:"llm_tokens_out,omitempty"`
	WikiPagesChanged int `yaml:"wiki_pages_changed,omitempty"`
	WikiPagesSkipped int `yaml:"wiki_pages_skipped,omitempty"`
}

// Marshal returns the report serialised as YAML.
func (r Report) Marshal() ([]byte, error) {
	return yaml.Marshal(r.toYAML())
}

// WriteToDir writes the report to dir as <run-id>.yaml and updates
// the latest.yaml symlink to point at it. Returns the path of the
// written file.
func (r Report) WriteToDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("reporter: mkdir %q: %w", dir, err)
	}
	body, err := r.Marshal()
	if err != nil {
		return "", err
	}
	name := r.RunID + ".yaml"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("reporter: write %q: %w", path, err)
	}
	if err := updateLatestSymlink(dir, name); err != nil {
		// Symlink failure doesn't lose the report — the file is on
		// disk. Surface as a warning by returning a non-nil error
		// that still includes the path.
		return path, fmt.Errorf("reporter: update latest symlink: %w", err)
	}
	return path, nil
}

// updateLatestSymlink atomically replaces dir/latest.yaml so it
// points at target (a basename within dir).
func updateLatestSymlink(dir, target string) error {
	latest := filepath.Join(dir, "latest.yaml")
	tmp := latest + ".tmp"
	_ = os.Remove(tmp) // best-effort cleanup of any stale tmp
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, latest)
}

func (r Report) toYAML() reportYAML {
	out := reportYAML{
		RunID:     r.RunID,
		Wiki:      r.Wiki,
		StartedAt: r.StartedAt,
		EndedAt:   r.EndedAt,
		KBCommit:  r.KBCommit,
		Errors:    r.Errors,
		Warnings:  r.Warnings,
		Metrics: metricsYAML{
			LLMCalls:         r.Metrics.LLMCalls,
			LLMTokensIn:      r.Metrics.LLMTokensIn,
			LLMTokensOut:     r.Metrics.LLMTokensOut,
			WikiPagesChanged: r.Metrics.WikiPagesChanged,
			WikiPagesSkipped: r.Metrics.WikiPagesSkipped,
		},
	}
	for _, s := range r.Specs {
		sy := specResultYAML{
			ID:                s.ID,
			Status:            s.Status,
			Reason:            s.Reason,
			NewRevisionID:     s.NewRevisionID,
			BlocksRegenerated: s.BlocksRegenerated,
		}
		for _, e := range s.HumanEdits {
			sy.HumanEdits = append(sy.HumanEdits, humanEditYAML{
				BlockID:     e.BlockID,
				Action:      e.Action,
				Diff:        e.Diff,
				Explanation: e.Explanation,
				Suggestion:  e.Suggestion,
			})
		}
		out.Specs = append(out.Specs, sy)
	}
	return out
}
