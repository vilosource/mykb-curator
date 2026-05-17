//go:build scenario

// Pyramid level 4 — the capstone proof for Track A: the spec-chat
// loop closes a real GRS-class brain-grounding gap.
//
// Reproduces the accepted residual gap (vault.doc.yaml's
// "Deployment & Operations" asserts "daily Raft snapshot -> GRS" but
// its sources don't carry that fact, so the hardened Judge flags it),
// then drives the widen-sources remedy through the REAL pipeline
// (docedit -> docspec.Parse -> cluster.Render -> architecture
// SectionGrounding -> judge.Review, via specchat.Previewer) and
// asserts the Judge verdict FLIPS fail -> pass once the grounding
// actually contains the fact. The LLM is a deterministic,
// grounding-aware scripted client ($0, offline): it proves the
// PIPELINE feeds the right grounding, not the real model's judgement.
package scenario_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/curatorapi"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/judge"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/cluster"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/architecture"
	"github.com/vilosource/mykb-curator/internal/specchat"
	"github.com/vilosource/mykb-curator/internal/specs/docspec/docedit"
)

// grsToken is the distinctive string that exists ONLY in the
// disaster-recovery area's fact — never in the vault/docker-swarm
// areas and never in the rendered body. So "prompt contains grsToken"
// is true iff area=disaster-recovery is in the section's grounding.
const grsToken = "storbackupscentralgrs01"

// scriptLLM: render calls return a body that makes the org-specific
// GRS claim (the section intent demands it) WITHOUT the token; judge
// calls pass iff the shown grounding carries the token.
type scriptLLM struct{}

func (scriptLLM) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	if strings.Contains(req.System, "documentation reviewer") {
		if strings.Contains(req.Prompt, grsToken) {
			return llm.Response{Text: `{"pass": true, "reason": "GRS destination is grounded", "ungrounded_claims": []}`}, nil
		}
		return llm.Response{Text: `{"pass": false, "reason": "the daily Raft snapshot to GRS destination is not in the shown grounding", "ungrounded_claims": ["daily Raft snapshot backed up to GRS"]}`}, nil
	}
	// architecture frontend render call — emit a body that contains
	// the org-specific GRS claim but NOT the distinctive token.
	return llm.Response{Text: "Vault takes a daily Raft snapshot that is backed up to geo-redundant storage for disaster recovery."}, nil
}

func writeArea(t *testing.T, root, id, name string, facts []map[string]any) {
	t.Helper()
	dir := filepath.Join(root, "areas", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]string{"id": id, "name": name, "summary": name})
	if err := os.WriteFile(filepath.Join(dir, "area.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range facts {
		f["area"], f["type"], f["zone"] = id, "fact", "established"
		line, _ := json.Marshal(f)
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "facts.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSpecChat_ClosesGRSGap(t *testing.T) {
	brain := t.TempDir()
	// vault area: real vault facts but NOTHING about the GRS backup
	// destination — this is the gap.
	writeArea(t, brain, "vault", "Vault", []map[string]any{
		{"id": "v1", "text": "Vault runs 3-node HA Raft on Docker Swarm with Azure Key Vault auto-unseal."},
		{"id": "v2", "text": "Vault ingress is via Traefik; SSO is Entra OIDC."},
	})
	writeArea(t, brain, "docker-swarm", "Docker Swarm", []map[string]any{
		{"id": "d1", "text": "Swarm services bind-mount NFS subdirectories under /var/docker-swarm-files."},
	})
	// disaster-recovery area: HAS the GRS destination fact (the token).
	writeArea(t, brain, "disaster-recovery", "Disaster Recovery", []map[string]any{
		{"id": "dr1", "text": "Vault daily Raft snapshots are archived to GRS account " + grsToken + " (RA-GRS, swedencentral->swedensouth) for DR."},
	})

	specYAML := []byte(`topic: Vault
parent:
  page: Vault Architecture
  kind: architecture
  audience: human-operator
  intent: >
    Explain how Optiscan deploys and operates Vault, including backups.
  sections:
    - title: Deployment & Operations
      intent: >
        How the stack is operated: rolling updates and the daily Raft
        snapshot backups to GRS.
      sources: ["kb:area=vault", "kb:area=docker-swarm"]
`)

	kbReader := kblocal.New(brain)
	mkPreviewer := func() *specchat.Previewer {
		fe := architecture.New(scriptLLM{}, "scenario-model")
		return specchat.NewPreviewer(kbReader, cluster.New(fe), judge.New(scriptLLM{}, "scenario-model"))
	}

	// 1. As authored: the GRS claim is ungrounded -> Judge FAILS.
	before, err := mkPreviewer().Preview(context.Background(), specYAML)
	if err != nil {
		t.Fatalf("preview (before): %v", err)
	}
	vBefore := verdictFor(before, "Deployment & Operations")
	if vBefore == nil {
		t.Fatalf("no verdict for the section; verdicts=%+v", before.Verdicts)
	}
	if vBefore.Pass {
		t.Fatalf("expected the GRS gap to FAIL pre-widen, but it passed: %+v", vBefore)
	}
	if before.AllPass {
		t.Fatal("AllPass must be false while the GRS claim is ungrounded")
	}

	// 2. The widen-sources remedy through the REAL editor (the slice-1
	//    docedit path the agent's put_doc_spec uses).
	doc, err := docedit.Parse(specYAML)
	if err != nil {
		t.Fatalf("docedit.Parse: %v", err)
	}
	if err := doc.AddSectionSource(docedit.ParentPage(), "Deployment & Operations", "kb:area=disaster-recovery"); err != nil {
		t.Fatalf("AddSectionSource: %v", err)
	}
	widened, err := doc.Bytes()
	if err != nil {
		t.Fatalf("docedit.Bytes: %v", err)
	}

	// 3. Same pipeline, widened spec: grounding now carries the fact
	//    -> Judge PASSES. The loop closed the gap.
	after, err := mkPreviewer().Preview(context.Background(), widened)
	if err != nil {
		t.Fatalf("preview (after): %v", err)
	}
	vAfter := verdictFor(after, "Deployment & Operations")
	if vAfter == nil || !vAfter.Pass {
		t.Fatalf("expected the verdict to FLIP to pass after widen, got %+v\nspec:\n%s", vAfter, widened)
	}
	if !after.AllPass {
		t.Fatalf("AllPass should be true once grounded; verdicts=%+v", after.Verdicts)
	}
}

func verdictFor(r curatorapi.PreviewResult, section string) *curatorapi.Verdict {
	for i := range r.Verdicts {
		if r.Verdicts[i].Section == section {
			return &r.Verdicts[i]
		}
	}
	return nil
}
