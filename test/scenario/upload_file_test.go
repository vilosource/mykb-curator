//go:build scenario

package scenario_test

import (
	"context"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
)

// 1x1 transparent PNG — a minimal valid image. mmdc-independent: the
// mermaid→PNG step is unit-tested (skipped when mmdc absent); this
// scenario proves the risky half — the hand-rolled multipart
// action=upload against a real MediaWiki — end to end.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// TestScenario_UploadFile_AgainstRealMediaWiki proves
// wiki.Target.UploadFile (used by the RenderDiagrams pass) works
// against a real MediaWiki: a PNG uploads, returns a File: ref, and
// re-uploading identical content under the same name is idempotent
// (ignorewarnings path) rather than erroring.
func TestScenario_UploadFile_AgainstRealMediaWiki(t *testing.T) {
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
	const name = "Curator_Scenario_Diagram.png"

	ref, err := tgt.UploadFile(ctx, name, onePixelPNG, "image/png", "scenario: first upload")
	if err != nil {
		t.Fatalf("UploadFile #1: %v", err)
	}
	if ref == "" {
		t.Fatalf("UploadFile returned empty ref")
	}
	if want := "File:" + name; ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}

	ref2, err := tgt.UploadFile(ctx, name, onePixelPNG, "image/png", "scenario: idempotent re-upload")
	if err != nil {
		t.Fatalf("UploadFile #2 (identical content must be idempotent, not error): %v", err)
	}
	if ref2 != ref {
		t.Errorf("idempotent re-upload changed ref: %q -> %q", ref, ref2)
	}
}
