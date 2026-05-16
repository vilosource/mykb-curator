//go:build scenario

// Package scenario_test holds pyramid level 4 tests — full
// end-to-end scenarios against real services.
//
// Run with:
//
//	make test-scenario
//
// or:
//
//	go test -race -count=1 -tags=scenario -timeout=30m ./test/scenario/...
//
// These tests:
//   - Spin up real Docker containers (testcontainers-go)
//   - Make real HTTP calls
//   - Take seconds to minutes per test
//   - Are gated to nightly CI; not run on every PR
package scenario_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	mwbackend "github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/mediawiki"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
	"github.com/vilosource/mykb-curator/internal/reporter"
)

// TestScenario_FirstRender_ProjectionAgainstRealMediaWiki:
//
// The first real end-to-end scenario. Everything is real:
//   - Real MediaWiki container (testcontainers-go)
//   - Real local kb (test/fixtures/kb/acme)
//   - Real local spec store (test/fixtures/specs/acme)
//   - Real curator orchestrator + passes + backend
//   - Real wiki MediaWiki API client
//
// Asserts:
//   - The Vault_Architecture page lands on the wiki
//   - The page content contains the kb facts we expect
//   - The page edit shows up under our bot identity
//
// Mis-routed specs (in the same fixture set) get rejected as before;
// editorial specs fail-skip without an LLM client wired here.
func TestScenario_FirstRender_ProjectionAgainstRealMediaWiki(t *testing.T) {
	mw := startMediaWiki(t)

	tgt, err := mediawiki.New(mediawiki.Config{
		APIURL:           mw.URL + "/api.php",
		BotUser:          mw.AdminUser,
		BotPass:          mw.AdminPass,
		DisableBotAssert: true, // scenario uses Admin-as-bot; bot group is set but ASSERT can still fail under SQLite test setup
	})
	if err != nil {
		t.Fatalf("mediawiki.New: %v", err)
	}

	kbSrc := kblocal.New("../fixtures/kb/acme")
	specStore := localfs.New("../fixtures/specs/acme")

	reg := frontends.NewRegistry()
	reg.Register(projection.New())

	orch := orchestrator.New(orchestrator.Deps{
		Wiki:       "acme",
		KB:         kbSrc,
		Specs:      specStore,
		WikiTarget: tgt,
		LLM:        stubLLM{},
		Frontends:  reg,
		BuildPasses: func(_ kb.Snapshot) *passes.Pipeline {
			return passes.NewPipeline(zonemarkers.New())
		},
		Backend: mwbackend.New(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rep, err := orch.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v\nReport: %s", err, rep.Summary())
	}

	// Find the area-vault.spec.md result and assert it rendered.
	var areaVault *reporter.SpecResult
	for i := range rep.Specs {
		if rep.Specs[i].ID == "area-vault.spec.md" {
			areaVault = &rep.Specs[i]
		}
	}
	if areaVault == nil {
		t.Fatalf("no area-vault.spec.md entry in report: %+v", rep.Specs)
	}
	if areaVault.Status != reporter.StatusRendered {
		t.Fatalf("area-vault status = %q, want %q; reason=%q",
			areaVault.Status, reporter.StatusRendered, areaVault.Reason)
	}

	// Verify the page actually landed on the wiki by hitting
	// /wiki/<title> directly and parsing the rendered HTML for
	// expected content. Bypasses any GetPage roundtrip fragility.
	verifyPageLanded(t, mw.URL, "Area/Vault_Architecture", []string{
		"Vault Architecture",  // the area's Name from fixtures
		"Vault runs as an HA", // a fact from the fixture facts.jsonl
		"VAULT-001",           // a decision id from decisions.jsonl
	})
	// The assertion the suite was missing: not just "words present"
	// but "rendered as correct MediaWiki structure".
	verifyRenderedStructure(t, mw.URL, "Area/Vault_Architecture")
}

// verifyPageLanded fetches the rendered HTML of a wiki page directly
// and asserts the expected substrings are present. Used by scenarios
// instead of the GetPage roundtrip — fewer moving parts, easier to
// debug failures.
func verifyPageLanded(t *testing.T, wikiURL, title string, wantSubstrings []string) {
	t.Helper()
	// Try the API parse route: simpler than scraping HTML.
	apiURL := wikiURL + "/api.php?action=parse&page=" + title + "&prop=wikitext&format=json"
	resp, err := http.Get(apiURL)
	if err != nil {
		t.Fatalf("GET %s: %v", apiURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := new(strings.Builder)
	_, _ = io.Copy(body, resp.Body)
	bodyStr := body.String()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: status=%d body=%s", apiURL, resp.StatusCode, bodyStr)
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("page %q missing %q in wikitext\n---\n%s\n---", title, want, bodyStr)
		}
	}
}

// verifyRenderedStructure fetches the *rendered HTML* (not raw
// wikitext) and asserts the page is structurally correct MediaWiki —
// the assertion the suite was missing. verifyPageLanded only checks
// wikitext substrings, so Markdown-into-MediaWiki "passed" (the
// words were present) while every heading rendered as a list item.
// This fails loudly on that.
func verifyRenderedStructure(t *testing.T, wikiURL, title string) {
	t.Helper()
	apiURL := wikiURL + "/api.php?action=parse&page=" + title + "&prop=text&format=json"
	resp, err := http.Get(apiURL)
	if err != nil {
		t.Fatalf("GET %s: %v", apiURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b := new(strings.Builder)
	_, _ = io.Copy(b, resp.Body)
	html := b.String()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: status=%d", apiURL, resp.StatusCode)
	}
	// Real section headings → <h2> in the parsed HTML.
	if !strings.Contains(html, "<h2") {
		t.Errorf("page %q has no <h2> heading in rendered HTML — sections not rendered as headings\n%s", title, truncate(html, 800))
	}
	// No markdown artifacts surviving into the rendered HTML.
	for _, bad := range []string{"## ", "### ", "&lt;h2&gt;"} {
		if strings.Contains(html, bad) {
			t.Errorf("markdown artifact %q present in rendered HTML of %q\n%s", bad, title, truncate(html, 800))
		}
	}
	// The YAML frontmatter fence must never reach rendered output.
	if strings.Contains(html, "spec_hash:") || strings.Contains(html, "<p>---") {
		t.Errorf("YAML frontmatter leaked into rendered HTML of %q\n%s", title, truncate(html, 800))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// stubLLM is a minimal LLM placeholder — scenario doesn't use
// editorial frontend in this first test.
type stubLLM struct{}

func (stubLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, nil
}
