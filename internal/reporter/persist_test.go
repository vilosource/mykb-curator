package reporter

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func sampleReport() Report {
	t := time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC)
	return Report{
		RunID:     "abc1234",
		Wiki:      "acme",
		StartedAt: t,
		EndedAt:   t.Add(2 * time.Minute),
		KBCommit:  "0f72946",
		Specs: []SpecResult{
			{
				ID:                "area-vault.spec.md",
				Status:            StatusRendered,
				NewRevisionID:     "49231",
				BlocksRegenerated: 4,
			},
			{
				ID:     "_invalid-wrong-wiki.spec.md",
				Status: StatusFailed,
				Reason: "frontmatter guardrail rejected mis-routed spec",
			},
		},
		Errors:   nil,
		Warnings: []string{"spec X declared include area Y which no longer exists"},
		Metrics: Metrics{
			LLMCalls:         0,
			LLMTokensIn:      0,
			LLMTokensOut:     0,
			WikiPagesChanged: 1,
			WikiPagesSkipped: 0,
		},
	}
}

func TestMarshal_ProducesParseableYAML(t *testing.T) {
	r := sampleReport()
	b, err := r.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(b) == 0 {
		t.Errorf("Marshal returned empty bytes")
	}
	// Sanity check fields present in output.
	s := string(b)
	for _, want := range []string{"run_id:", "wiki: acme", "kb_commit: 0f72946", "area-vault.spec.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q\n---\n%s\n---", want, s)
		}
	}
	// Verify it parses back as YAML — does not need full round-trip
	// equality, just that it's syntactically valid.
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("re-parse YAML: %v", err)
	}
}

func TestWriteToDir_CreatesFileAndLatestSymlink(t *testing.T) {
	dir := t.TempDir()
	r := sampleReport()
	path, err := r.WriteToDir(dir)
	if err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}

	// File written with run-id in the name for traceability.
	if !strings.Contains(filepath.Base(path), r.RunID) {
		t.Errorf("written file %q doesn't include run-id %q", path, r.RunID)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if !bytes.Contains(body, []byte("wiki: acme")) {
		t.Errorf("written file content unexpected:\n%s", body)
	}

	// "latest" symlink points to the new file.
	latest := filepath.Join(dir, "latest.yaml")
	info, err := os.Lstat(latest)
	if err != nil {
		t.Fatalf("lstat latest: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("latest.yaml is not a symlink")
	}
	target, err := os.Readlink(latest)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != filepath.Base(path) {
		t.Errorf("latest -> %q, want -> %q", target, filepath.Base(path))
	}
}

func TestWriteToDir_OverwritesLatestSymlink(t *testing.T) {
	dir := t.TempDir()
	r1 := sampleReport()
	r1.RunID = "run-1"
	p1, _ := r1.WriteToDir(dir)

	r2 := sampleReport()
	r2.RunID = "run-2"
	p2, _ := r2.WriteToDir(dir)

	target, _ := os.Readlink(filepath.Join(dir, "latest.yaml"))
	if target != filepath.Base(p2) {
		t.Errorf("latest -> %q, want %q (latest must point at most-recent run)", target, filepath.Base(p2))
	}
	// First file still exists for history.
	if _, err := os.Stat(p1); err != nil {
		t.Errorf("first report file deleted: %v", err)
	}
}
