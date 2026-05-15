package wikiparse

import (
	"strings"
	"testing"
)

func TestParse_EmptyInput(t *testing.T) {
	got := Parse(nil)
	if len(got.Blocks) != 0 {
		t.Errorf("len(Blocks) = %d, want 0", len(got.Blocks))
	}
}

func TestParse_NoMarkers_NoBlocks(t *testing.T) {
	in := []byte(`# Title

Just some plain markdown.

## Section

Another paragraph.`)
	got := Parse(in)
	if len(got.Blocks) != 0 {
		t.Errorf("plain markdown should yield 0 blocks; got %d", len(got.Blocks))
	}
	if got.Prologue != string(in) {
		t.Errorf("everything outside markers should be Prologue")
	}
}

func TestParse_SingleBlock(t *testing.T) {
	in := []byte(`prefix text

<!-- CURATOR:BEGIN block=b1 provenance=hash-abc -->

block body line 1
block body line 2

<!-- CURATOR:END block=b1 -->

suffix text
`)
	got := Parse(in)
	if len(got.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(got.Blocks))
	}
	b := got.Blocks[0]
	if b.ID != "b1" {
		t.Errorf("ID = %q, want %q", b.ID, "b1")
	}
	if b.Provenance != "hash-abc" {
		t.Errorf("Provenance = %q, want %q", b.Provenance, "hash-abc")
	}
	if !strings.Contains(b.Body, "block body line 1") {
		t.Errorf("Body missing line 1: %q", b.Body)
	}
	if !strings.Contains(b.Body, "block body line 2") {
		t.Errorf("Body missing line 2: %q", b.Body)
	}
	if !strings.Contains(got.Prologue, "prefix text") {
		t.Errorf("Prologue missing prefix")
	}
}

func TestParse_MultipleBlocks_PreservedInOrder(t *testing.T) {
	in := []byte(`<!-- CURATOR:BEGIN block=a provenance=h-a -->
A content
<!-- CURATOR:END block=a -->

<!-- CURATOR:BEGIN block=b provenance=h-b -->
B content
<!-- CURATOR:END block=b -->

<!-- CURATOR:BEGIN block=c provenance=h-c -->
C content
<!-- CURATOR:END block=c -->
`)
	got := Parse(in)
	if len(got.Blocks) != 3 {
		t.Fatalf("len(Blocks) = %d, want 3", len(got.Blocks))
	}
	wantIDs := []string{"a", "b", "c"}
	for i, w := range wantIDs {
		if got.Blocks[i].ID != w {
			t.Errorf("Blocks[%d].ID = %q, want %q", i, got.Blocks[i].ID, w)
		}
	}
}

func TestParse_BlockWithoutProvenance(t *testing.T) {
	// Older renders or hand-edited markers may omit the provenance
	// attribute. Parser should still find the block and just return
	// empty provenance.
	in := []byte(`<!-- CURATOR:BEGIN block=foo -->
body
<!-- CURATOR:END block=foo -->
`)
	got := Parse(in)
	if len(got.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(got.Blocks))
	}
	if got.Blocks[0].Provenance != "" {
		t.Errorf("Provenance = %q, want empty", got.Blocks[0].Provenance)
	}
	if got.Blocks[0].ID != "foo" {
		t.Errorf("ID = %q, want foo", got.Blocks[0].ID)
	}
}

func TestParse_UnclosedBegin_TreatedAsRawText(t *testing.T) {
	// Malformed input shouldn't panic; an unclosed BEGIN marker
	// becomes part of the prologue/interstitial text rather than
	// hijacking a block.
	in := []byte(`<!-- CURATOR:BEGIN block=oops -->
incomplete
no end marker
`)
	got := Parse(in)
	if len(got.Blocks) != 0 {
		t.Errorf("unclosed BEGIN should produce 0 blocks; got %d", len(got.Blocks))
	}
}

func TestParse_InterstitialContentBetweenBlocks_KeptInInterstitials(t *testing.T) {
	in := []byte(`prologue

<!-- CURATOR:BEGIN block=a provenance=ha -->
A
<!-- CURATOR:END block=a -->

between A and B

<!-- CURATOR:BEGIN block=b provenance=hb -->
B
<!-- CURATOR:END block=b -->

epilogue`)
	got := Parse(in)
	if len(got.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(got.Blocks))
	}
	if !strings.Contains(got.Prologue, "prologue") {
		t.Errorf("Prologue should contain prologue")
	}
	if !strings.Contains(got.Epilogue, "epilogue") {
		t.Errorf("Epilogue should contain epilogue")
	}
	if !strings.Contains(got.Blocks[0].FollowingText, "between A and B") {
		t.Errorf("FollowingText for block[0] should hold interstitial text; got %q", got.Blocks[0].FollowingText)
	}
}

func TestFindBlockByID(t *testing.T) {
	in := []byte(`<!-- CURATOR:BEGIN block=alpha provenance=ha -->
A
<!-- CURATOR:END block=alpha -->
<!-- CURATOR:BEGIN block=beta provenance=hb -->
B
<!-- CURATOR:END block=beta -->`)
	got := Parse(in)
	b, ok := got.BlockByID("beta")
	if !ok {
		t.Fatalf("BlockByID(beta) not found")
	}
	if b.Provenance != "hb" {
		t.Errorf("Provenance = %q, want hb", b.Provenance)
	}
	if _, ok := got.BlockByID("missing"); ok {
		t.Errorf("BlockByID(missing) should not find anything")
	}
}
