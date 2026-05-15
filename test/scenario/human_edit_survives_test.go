//go:build scenario

package scenario_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/editorial"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
)

// TestScenario_HumanEditSurvivesReRender proves the soft-read-only
// contract end-to-end:
//
//  1. Curator renders an editorial page to MediaWiki.
//  2. We fetch the page and patch an editorial block's body — a
//     human polishing a sentence on the wiki.
//  3. Curator re-renders the same spec against the same kb.
//  4. The human's edited body survives in the editorial block;
//     machine blocks are still regenerated wholesale.
//
// This is the moment "soft read-only" stops being a design claim
// and starts being an empirically-verified property.
func TestScenario_HumanEditSurvivesReRender(t *testing.T) {
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

	buildOrch := func() *orchestrator.Orchestrator {
		return orchestrator.New(orchestrator.Deps{
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
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// First render — page lands on wiki with both editorial + machine blocks.
	if _, err := buildOrch().Run(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Fetch the page we just rendered. Using direct HTTP rather than
	// the curator's MediaWikiTarget.GetPage because the latter has a
	// known fragility on fresh-install MW with slots responses (see
	// first_render_test.go for the same workaround).
	pageTitle := "Azure_Infrastructure"
	original, err := fetchPageContent(ctx, mw.URL, pageTitle)
	if err != nil {
		t.Fatalf("fetch original content: %v", err)
	}
	if !strings.Contains(original, "zone=editorial") {
		t.Fatalf("first render didn't emit editorial markers — block-level merge can't apply\n%s", original)
	}

	// Simulate a human polishing the prose: take the rendered page,
	// inject a marker into the first editorial block's body, push it
	// back as a non-bot edit.
	humanPolished := injectHumanPolish(original, "HUMAN-POLISH-MARKER")
	if humanPolished == original {
		t.Fatalf("test bug: human polish injection didn't change content")
	}
	if err := simulateHumanEdit(ctx, mw.URL, mw.AdminUser, mw.AdminPass, pageTitle, humanPolished); err != nil {
		t.Fatalf("simulate human edit: %v", err)
	}

	// Verify the polish made it onto the wiki.
	afterHuman, err := fetchPageContent(ctx, mw.URL, pageTitle)
	if err != nil {
		t.Fatalf("fetch after human edit: %v", err)
	}
	if !strings.Contains(afterHuman, "HUMAN-POLISH-MARKER") {
		t.Fatalf("human polish didn't land on wiki:\n%s", afterHuman)
	}
	// Second curator run — same spec, same kb. Should preserve the
	// human polish on the editorial block because its provenance
	// hash hasn't changed.
	if _, err := buildOrch().Run(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}

	final, err := fetchPageContent(ctx, mw.URL, pageTitle)
	if err != nil {
		t.Fatalf("fetch final: %v", err)
	}
	if !strings.Contains(final, "HUMAN-POLISH-MARKER") {
		t.Errorf("editorial preservation broken — human polish lost on re-render:\n%s", final)
	}
}

// injectHumanPolish finds the first editorial block's body and
// prepends a marker token. Used to simulate a human editing a wiki
// page. Returns content unchanged if no editorial block found.
func injectHumanPolish(content, marker string) string {
	// Find the first BEGIN block with zone=editorial.
	idx := strings.Index(content, "zone=editorial")
	if idx < 0 {
		return content
	}
	// Find the end of the BEGIN line.
	beginEnd := strings.Index(content[idx:], "-->")
	if beginEnd < 0 {
		return content
	}
	insertAt := idx + beginEnd + len("-->")
	// Insert the marker on a new line right after the BEGIN marker.
	return content[:insertAt] + "\n\n" + marker + "\n" + content[insertAt:]
}

// simulateHumanEdit pushes a page revision under the Admin account
// WITHOUT the bot flag — i.e., as a regular human edit. Uses the
// MediaWiki action=edit API directly with bot=0.
//
// Login + token + edit sequence done with raw http so we don't go
// through the curator's MediaWikiTarget (which always sets bot=1).
func simulateHumanEdit(ctx context.Context, wikiURL, username, password, title, content string) error {
	jar, _ := newCookieJar()
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	// Step 1: get login token.
	tokURL := wikiURL + "/api.php?action=query&meta=tokens&type=login&format=json"
	loginTok, err := postForJSONField(ctx, c, tokURL, nil, "query.tokens.logintoken")
	if err != nil {
		return err
	}

	// Step 2: login.
	loginForm := url.Values{
		"action":     {"login"},
		"lgname":     {username},
		"lgpassword": {password},
		"lgtoken":    {loginTok},
		"format":     {"json"},
	}
	if _, err := postForJSONField(ctx, c, wikiURL+"/api.php", loginForm, "login.result"); err != nil {
		return err
	}

	// Step 3: csrftoken.
	csrfURL := wikiURL + "/api.php?action=query&meta=tokens&type=csrf&format=json"
	csrf, err := postForJSONField(ctx, c, csrfURL, nil, "query.tokens.csrftoken")
	if err != nil {
		return err
	}

	// Step 4: edit (without bot=1 — this is a "human" edit).
	editForm := url.Values{
		"action":  {"edit"},
		"title":   {title},
		"text":    {content},
		"summary": {"human polish (test)"},
		"token":   {csrf},
		"format":  {"json"},
	}
	if _, err := postForJSONField(ctx, c, wikiURL+"/api.php", editForm, "edit.result"); err != nil {
		return err
	}
	return nil
}
