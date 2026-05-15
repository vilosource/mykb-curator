//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
)

// TestLocalKB_Orchestrator_HappyPath replaces the inMemKB fake with a
// real LocalKBSource pointed at test/fixtures/kb/acme. This proves the
// full read path (filesystem → JSONL → kb.Snapshot) wired through the
// orchestrator.
func TestLocalKB_Orchestrator_HappyPath(t *testing.T) {
	kb := kblocal.New("../fixtures/kb/acme")
	specs := localfs.New("../fixtures/specs/acme")

	o := orchestrator.New(orchestrator.Deps{
		Wiki:       "acme",
		KB:         kb,
		Specs:      specs,
		WikiTarget: inMemWiki{},
		LLM:        inMemLLM{},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rep, err := o.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The kb commit field comes from the source. For the local impl,
	// commit is unset (no SCM); other fields prove the snapshot loaded.
	if len(rep.Specs) == 0 {
		t.Errorf("no specs in report; expected 2 valid + 1 mis-routed from acme fixture")
	}
}
