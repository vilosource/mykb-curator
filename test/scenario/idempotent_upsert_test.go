//go:build scenario

package scenario_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
)

// TestScenario_IdempotentUpsert_NotReportedFailed is the regression
// for kb gotcha eEeHXXRM: re-writing a page with byte-identical
// content makes MediaWiki return "edit successful, but did not
// change page" (go-mwclient ErrEditNoChange). That is an idempotent
// no-op SUCCESS — the desired state is present — and must NOT
// surface as an error (which previously made the run report falsely
// say status=failed though the page was correct).
func TestScenario_IdempotentUpsert_NotReportedFailed(t *testing.T) {
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

	ctx := context.Background()
	const (
		title = "Idempotency_Probe"
		body  = "Byte-identical content written twice."
	)

	if _, err := tgt.UpsertPage(ctx, title, body, "first write"); err != nil {
		t.Fatalf("first UpsertPage: %v", err)
	}
	// Second write: identical content ⇒ MediaWiki "nochange". This
	// must be a successful no-op, not an error.
	rev, err := tgt.UpsertPage(ctx, title, body, "identical re-write")
	if err != nil {
		t.Fatalf("idempotent re-upsert must succeed, got error: %v", err)
	}
	if !rev.IsBot {
		t.Errorf("no-op revision should still report the bot identity, got %+v", rev)
	}

	// Sanity: the page is present with the expected content.
	got, err := fetchPageContent(ctx, mw.URL, title)
	if err != nil {
		t.Fatalf("fetch page: %v", err)
	}
	if got == "" {
		t.Errorf("page %q missing after idempotent upsert", title)
	}
}
