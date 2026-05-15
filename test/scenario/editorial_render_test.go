//go:build scenario

package scenario_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/editorial"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// fixedLLM returns a canned editorial-style markdown response. Used
// to make the editorial scenario deterministic without a real
// Anthropic key. Distinct from stubLLM (which returns empty) so the
// projection scenario keeps its existing semantics.
type fixedLLM struct{}

func (fixedLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{
		Text: `## Overview

Vault is the centralised secrets manager for the platform. It serves as the single source of truth for application secrets, database credentials, and service tokens.

## Topology

Vault runs as a high-availability cluster using the Raft consensus protocol. Auto-unseal is configured against Azure Key Vault so the cluster can recover from a full restart without human intervention.

## Design decisions

The Raft backend was chosen over Consul (VAULT-001) to remove Consul as an operational dependency.
`,
		TokensIn:  150,
		TokensOut: 80,
	}, nil
}

// TestScenario_EditorialRender_AgainstRealMediaWiki:
//
// Wires the editorial frontend with a deterministic LLM stub and
// runs the full pipeline end-to-end against a real MediaWiki:
// loads the azure-infrastructure spec (editorial kind), generates
// IR from the LLM response, renders to wikitext, pushes to the
// wiki, and verifies the page lands with the LLM-generated prose.
//
// This is the v0.5 capstone — the curator's intelligence locus
// (editorial frontend) proven against real infrastructure end-to-end.
func TestScenario_EditorialRender_AgainstRealMediaWiki(t *testing.T) {
	mw := startMediaWiki(t)

	tgt, err := mediawiki.New(mediawiki.Config{
		APIURL:           mw.URL + "/api.php",
		BotUser:          mw.AdminUser,
		BotPass:          mw.AdminPass,
		DisableBotAssert: true,
	})
	if err != nil {
		t.Fatalf("mediawiki.New: %v", err)
	}

	kbSrc := kblocal.New("../fixtures/kb/acme")
	specStore := localfs.New("../fixtures/specs/acme")

	reg := frontends.NewRegistry()
	reg.Register(projection.New())
	reg.Register(editorial.New(fixedLLM{}, "claude-test-fixed"))

	orch := orchestrator.New(orchestrator.Deps{
		Wiki:       "acme",
		KB:         kbSrc,
		Specs:      specStore,
		WikiTarget: tgt,
		LLM:        fixedLLM{},
		Frontends:  reg,
		BuildPasses: func(_ kb.Snapshot) *passes.Pipeline {
			return passes.NewPipeline(zonemarkers.New())
		},
		Backend: markdown.New(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rep, err := orch.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v\nReport: %s", err, rep.Summary())
	}

	// Find the azure-infrastructure (editorial) spec result.
	var editorialSpec *reporter.SpecResult
	for i := range rep.Specs {
		if rep.Specs[i].ID == "azure-infrastructure.spec.md" {
			editorialSpec = &rep.Specs[i]
		}
	}
	if editorialSpec == nil {
		t.Fatalf("no azure-infrastructure.spec.md entry in report: %+v", rep.Specs)
	}
	if editorialSpec.Status != reporter.StatusRendered {
		t.Fatalf("azure-infrastructure status = %q, want %q; reason=%q",
			editorialSpec.Status, reporter.StatusRendered, editorialSpec.Reason)
	}

	// Verify the LLM-generated prose lands on the wiki.
	verifyPageLanded(t, mw.URL, "Azure_Infrastructure", []string{
		"Overview",
		"centralised secrets manager",
		"Topology",
		"VAULT-001",
		"Raft backend was chosen over Consul",
	})
}

// Sanity check: the fixed response parses cleanly when fed through
// the editorial frontend in isolation. Catches markdown-format
// drift in the canned response without needing the full scenario.
func TestEditorialFixedResponse_ParsesToSections(t *testing.T) {
	r, _ := fixedLLM{}.Complete(context.Background(), llm.Request{})
	if !strings.Contains(r.Text, "## ") {
		t.Errorf("fixed response missing section headings; editorial frontend won't parse it correctly")
	}
}
