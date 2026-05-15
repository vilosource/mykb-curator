//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// TestEndToEnd_ProjectionPipeline_AgainstFixtures is the first
// genuinely-end-to-end pyramid-level-2 test. It wires the full
// rendering pipeline (Frontend → Passes → Backend) against committed
// kb + spec fixtures, captures the rendered markdown via OnRendered,
// and compares against a golden file.
//
// Any change to the projection frontend, ApplyZoneMarkers pass, or
// markdown backend that affects output will fail this test until
// the golden is regenerated and reviewed.
func TestEndToEnd_ProjectionPipeline_AgainstFixtures(t *testing.T) {
	kb := kblocal.New("../fixtures/kb/acme")
	specs := localfs.New("../fixtures/specs/acme")

	reg := frontends.NewRegistry()
	reg.Register(projection.New())

	rendered := map[string][]byte{}
	o := orchestrator.New(orchestrator.Deps{
		Wiki:       "acme",
		KB:         kb,
		Specs:      specs,
		WikiTarget: inMemWiki{},
		LLM:        inMemLLM{},
		Frontends:  reg,
		Passes:     passes.NewPipeline(zonemarkers.New()),
		Backend:    markdown.New(),
		OnRendered: func(id string, b []byte, _ ir.Document) error {
			rendered[id] = b
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rep, err := o.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Two acme specs are projection (area-vault + azure-infrastructure
	// but only the projection one is registered; the editorial one
	// fails for "no frontend registered for kind=editorial"). One
	// spec is deliberately mis-routed.
	var rendered_count, failed int
	for _, s := range rep.Specs {
		switch s.Status {
		case reporter.StatusRendered:
			rendered_count++
		case reporter.StatusFailed:
			failed++
		}
	}
	if rendered_count < 1 {
		t.Errorf("rendered = %d, want ≥ 1 (area-vault.spec.md is a projection)", rendered_count)
	}
	if failed < 1 {
		t.Errorf("failed = %d, want ≥ 1 (_invalid-wrong-wiki must fail guardrail)", failed)
	}

	// Golden file check: area-vault is the deterministic projection.
	vaultOut, ok := rendered["area-vault.spec.md"]
	if !ok {
		t.Fatalf("no rendered output for area-vault.spec.md; got keys: %v", keys(rendered))
	}
	// Strip the GeneratedAt timestamp + footer LastCurated since
	// the frontend doesn't set them in v0.1 (and we want the
	// golden to be stable across runs even before that wires up).
	got := normalizeForGolden(vaultOut)

	goldenPath := filepath.Join("testdata", "area-vault.golden.md")
	if *updateGolden {
		_ = os.MkdirAll(filepath.Dir(goldenPath), 0o755)
		if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("end-to-end output does not match golden %s\nrun with -update to regenerate then review\ngot:\n%s", goldenPath, got)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// normalizeForGolden strips fields that aren't yet deterministic in
// the pipeline (generated_at, last-curated). When those become
// stamped by a future pass, this helper goes away.
func normalizeForGolden(b []byte) []byte {
	out := []byte{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "generated_at:") ||
			strings.Contains(line, "last-curated:") {
			continue
		}
		out = append(out, []byte(line)...)
		out = append(out, '\n')
	}
	return bytes.TrimRight(out, "\n")
}
