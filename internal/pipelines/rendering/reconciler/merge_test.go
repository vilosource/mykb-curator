package reconciler

import (
	"strings"
	"testing"
)

// Sanity: zero existing + zero new = empty merged result.
func TestMergeBlocks_BothEmpty(t *testing.T) {
	got := MergeBlocks([]byte{}, []byte{})
	if got != "" {
		t.Errorf("merge of two empties = %q, want empty", got)
	}
}

// New page (no existing content) → merged result is the new content
// verbatim.
func TestMergeBlocks_FirstRender(t *testing.T) {
	newContent := []byte(`# Title

<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
fresh content
<!-- CURATOR:END block=a -->
`)
	got := MergeBlocks(nil, newContent)
	if !strings.Contains(got, "fresh content") {
		t.Errorf("first-render merge lost new content:\n%s", got)
	}
}

func TestMergeBlocks_EditorialPreservation_SameProvenance(t *testing.T) {
	// The wiki has block a with human-polished body. The new render
	// has block a with the same provenance hash. Result: keep the
	// wiki body.
	existing := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=h1 -->
human-polished body
<!-- CURATOR:END block=a -->
`)
	newContent := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=h1 -->
fresh body from new render
<!-- CURATOR:END block=a -->
`)
	got := MergeBlocks(existing, newContent)
	if !strings.Contains(got, "human-polished body") {
		t.Errorf("editorial-with-matching-prov should preserve wiki content; got:\n%s", got)
	}
	if strings.Contains(got, "fresh body from new render") {
		t.Errorf("editorial preservation failed; new content leaked in:\n%s", got)
	}
}

func TestMergeBlocks_EditorialOverwritten_DifferentProvenance(t *testing.T) {
	// Same editorial block but provenance differs → kb changed
	// upstream → use new render.
	existing := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=OLD -->
stale body
<!-- CURATOR:END block=a -->
`)
	newContent := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=NEW -->
refreshed body
<!-- CURATOR:END block=a -->
`)
	got := MergeBlocks(existing, newContent)
	if !strings.Contains(got, "refreshed body") {
		t.Errorf("provenance change should overwrite editorial block; got:\n%s", got)
	}
	if strings.Contains(got, "stale body") {
		t.Errorf("stale content not overwritten:\n%s", got)
	}
}

func TestMergeBlocks_MachineAlwaysOverwritten(t *testing.T) {
	// Machine blocks: provenance match irrelevant — always use new
	// render. Pressures humans to edit kb, not wiki.
	existing := []byte(`<!-- CURATOR:BEGIN block=t zone=machine provenance=h1 -->
| old | row |
<!-- CURATOR:END block=t -->
`)
	newContent := []byte(`<!-- CURATOR:BEGIN block=t zone=machine provenance=h1 -->
| new | row |
<!-- CURATOR:END block=t -->
`)
	got := MergeBlocks(existing, newContent)
	if !strings.Contains(got, "new") || strings.Contains(got, "| old |") {
		t.Errorf("machine block should always use new render:\n%s", got)
	}
}

func TestMergeBlocks_NewBlock_NotInExisting_TakesNewVerbatim(t *testing.T) {
	existing := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
A body
<!-- CURATOR:END block=a -->
`)
	newContent := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
A from new render (would be preserved as wiki has same)
<!-- CURATOR:END block=a -->
<!-- CURATOR:BEGIN block=b zone=editorial provenance=h-b -->
B is a brand-new block
<!-- CURATOR:END block=b -->
`)
	got := MergeBlocks(existing, newContent)
	if !strings.Contains(got, "A body") {
		t.Errorf("block A should be preserved (matching prov):\n%s", got)
	}
	if !strings.Contains(got, "B is a brand-new block") {
		t.Errorf("new block B should appear:\n%s", got)
	}
}

func TestMergeBlocks_OldBlock_NotInNew_DroppedFromResult(t *testing.T) {
	// A block that the new render doesn't emit should NOT appear in
	// the merged output — even if it was on the wiki. The wiki page
	// reflects the current spec's intent; orphaned blocks would
	// accumulate forever otherwise.
	existing := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
A
<!-- CURATOR:END block=a -->
<!-- CURATOR:BEGIN block=orphan zone=editorial provenance=h-o -->
removed from new spec
<!-- CURATOR:END block=orphan -->
`)
	newContent := []byte(`<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
A from new
<!-- CURATOR:END block=a -->
`)
	got := MergeBlocks(existing, newContent)
	if strings.Contains(got, "removed from new spec") {
		t.Errorf("orphan block should be dropped:\n%s", got)
	}
}

func TestMergeBlocks_PreservesPrologueAndEpilogueFromNew(t *testing.T) {
	// Prologue/epilogue come from the NEW render — these are
	// frontmatter, title, footer — they're machine-owned by the
	// backend's convention.
	existing := []byte(`OLD PROLOGUE

<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
A
<!-- CURATOR:END block=a -->

OLD EPILOGUE`)
	newContent := []byte(`NEW PROLOGUE

<!-- CURATOR:BEGIN block=a zone=editorial provenance=h-a -->
A new
<!-- CURATOR:END block=a -->

NEW EPILOGUE`)
	got := MergeBlocks(existing, newContent)
	if !strings.Contains(got, "NEW PROLOGUE") || !strings.Contains(got, "NEW EPILOGUE") {
		t.Errorf("prologue/epilogue should come from new render:\n%s", got)
	}
	if strings.Contains(got, "OLD PROLOGUE") {
		t.Errorf("old prologue should be replaced:\n%s", got)
	}
}
