//go:build scenario

package scenario_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	mwbackend "github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/mediawiki"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/editorial"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// startPiHarness builds + runs deployments/pi-harness/Dockerfile and
// returns its base URL. The provider key is passed through from the
// host env so the containerised pi can reach a model.
func startPiHarness(t *testing.T, providerEnv map[string]string) string {
	t.Helper()
	ctx := context.Background()

	_, self, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(self), "..", "..")

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    repoRoot,
				Dockerfile: "deployments/pi-harness/Dockerfile",
				KeepImage:  true,
			},
			ExposedPorts: []string{"8080/tcp"},
			Env:          providerEnv,
			WaitingFor:   wait.ForHTTP("/healthz").WithPort("8080/tcp").WithStartupTimeout(4 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("build/start pi-harness: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("pi-harness host: %v", err)
	}
	port, err := c.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("pi-harness port: %v", err)
	}
	return "http://" + host + ":" + port.Port()
}

// TestScenario_AgenticRender_LiveAgentAgainstRealMediaWiki is the
// experiment-harness capstone: a real Pi agent (in the pi-harness
// container) authors the editorial page, which lands on a real
// MediaWiki. Both halves are real; nothing is replayed.
//
// Opt-in / nightly-gated: skipped unless MYKB_CURATOR_AGENTIC=1 and a
// provider key is present. It exercises a live model (non-
// deterministic, costs tokens, minutes-slow) so it must never run in
// the default test-scenario / CI path — the skip is a faithful,
// reasoned deferral, not a hidden gap.
func TestScenario_AgenticRender_LiveAgentAgainstRealMediaWiki(t *testing.T) {
	if os.Getenv("MYKB_CURATOR_AGENTIC") != "1" {
		t.Skip("agentic harness scenario is opt-in: set MYKB_CURATOR_AGENTIC=1 (+ a provider key) to run; live model, costs tokens, nightly-only")
	}

	// Pick the provider key + model. MYKB_CURATOR_AGENTIC_MODEL pins
	// the pi "provider/id"; default tracks whichever key is present.
	model := os.Getenv("MYKB_CURATOR_AGENTIC_MODEL")
	providerEnv := map[string]string{}
	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		providerEnv["ANTHROPIC_API_KEY"] = os.Getenv("ANTHROPIC_API_KEY")
		if model == "" {
			model = "anthropic/claude-sonnet-4-5"
		}
	case os.Getenv("GEMINI_API_KEY") != "":
		providerEnv["GEMINI_API_KEY"] = os.Getenv("GEMINI_API_KEY")
		if model == "" {
			// Verified working against the live pi-harness
			// 2026-05-16; an invalid id (e.g. gemini-2.0-flash) makes
			// pi emit no text — the model string is load-bearing.
			model = "google/gemini-2.5-flash"
		}
	case os.Getenv("OPENAI_API_KEY") != "":
		providerEnv["OPENAI_API_KEY"] = os.Getenv("OPENAI_API_KEY")
		providerEnv["OPENAI_BASE_URL"] = os.Getenv("OPENAI_BASE_URL")
		if model == "" {
			model = "openai/gpt-4o-mini"
		}
	default:
		t.Skip("no provider key (ANTHROPIC_API_KEY / GEMINI_API_KEY / OPENAI_API_KEY) — cannot drive a live agent")
	}

	piURL := startPiHarness(t, providerEnv)
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

	piClient := llm.NewPiClient(piURL)
	reg := frontends.NewRegistry()
	reg.Register(projection.New())
	reg.Register(editorial.New(piClient, model))

	orch := orchestrator.New(orchestrator.Deps{
		Wiki:       "acme",
		KB:         kblocal.New("../fixtures/kb/acme"),
		Specs:      localfs.New("../fixtures/specs/acme"),
		WikiTarget: tgt,
		LLM:        piClient,
		Frontends:  reg,
		BuildPasses: func(_ kb.Snapshot) *passes.Pipeline {
			return passes.NewPipeline(zonemarkers.New())
		},
		Backend: mwbackend.New(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rep, err := orch.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v\nReport: %s", err, rep.Summary())
	}

	var ed *reporter.SpecResult
	for i := range rep.Specs {
		if rep.Specs[i].ID == "azure-infrastructure.spec.md" {
			ed = &rep.Specs[i]
		}
	}
	if ed == nil {
		t.Fatalf("no azure-infrastructure.spec.md in report: %+v", rep.Specs)
	}
	if ed.Status != reporter.StatusRendered {
		t.Fatalf("editorial status = %q, want rendered; reason=%q", ed.Status, ed.Reason)
	}

	// Live output is non-deterministic — assert the agent authored a
	// non-trivial page that actually landed on the real wiki, not
	// exact prose.
	got, err := fetchPageContent(context.Background(), mw.URL, "Azure_Infrastructure")
	if err != nil {
		t.Fatalf("fetch landed page: %v", err)
	}
	if len(strings.TrimSpace(got)) < 80 {
		t.Errorf("agent-authored page is implausibly short (%d chars): %q", len(got), got)
	}
}
